package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/geminicli"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

func (s *GeminiMessagesCompatService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	startTime := time.Now()

	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("missing model")
	}

	originalModel := req.Model
	mappedModel := req.Model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(req.Model)
	}

	geminiReq, err := convertClaudeMessagesToGeminiGenerateContent(body)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	geminiReq = ensureGeminiFunctionCallThoughtSignatures(geminiReq)
	originalClaudeBody := body

	proxyURL, err := s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
	if err != nil {
		return nil, err
	}

	var requestIDHeader string
	var buildReq func(ctx context.Context) (*http.Request, string, error)
	useUpstreamStream := req.Stream
	if account.Type == AccountTypeOAuth && !req.Stream && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
	}

	switch account.Type {
	case AccountTypeAPIKey:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			apiKey := account.GetCredential("api_key")
			if strings.TrimSpace(apiKey) == "" {
				return nil, "", errors.New("gemini api_key not configured")
			}

			baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
			normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if req.Stream {
				action = "streamGenerateContent"
			}
			fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, action)
			if req.Stream {
				fullURL += "?alt=sse"
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			setGeminiAPIKeyAuth(upstreamReq, account, apiKey)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	case AccountTypeOAuth:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			projectID := strings.TrimSpace(account.GetCredential("project_id"))

			action := "generateContent"
			if useUpstreamStream {
				action = "streamGenerateContent"
			}

			// Two modes for OAuth:
			// 1. With project_id -> Code Assist API (wrapped request)
			// 2. Without project_id -> AI Studio API (direct OAuth, like API key but with Bearer token)
			if projectID != "" {
				// Mode 1: Code Assist API
				baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
				if err != nil {
					return nil, "", err
				}
				fullURL := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), action)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				wrapped := map[string]any{
					"model":   mappedModel,
					"project": projectID,
				}
				var inner any
				if err := json.Unmarshal(geminiReq, &inner); err != nil {
					return nil, "", fmt.Errorf("failed to parse gemini request: %w", err)
				}
				wrapped["request"] = inner
				wrappedBytes, _ := json.Marshal(wrapped)

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(wrappedBytes))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				upstreamReq.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
				return upstreamReq, "x-request-id", nil
			} else {
				// Mode 2: AI Studio API with OAuth (like API key mode, but using Bearer token)
				baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
				normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
				if err != nil {
					return nil, "", err
				}

				fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, action)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				return upstreamReq, "x-request-id", nil
			}
		}
		requestIDHeader = "x-request-id"

	case AccountTypeServiceAccount:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if req.Stream {
				action = "streamGenerateContent"
			}
			fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, action, req.Stream)
			if err != nil {
				return nil, "", err
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	default:
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}

	var resp *http.Response
	signatureRetryStage := 0
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Local build error: don't retry.
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			}
			return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}
		requestIDHeader = idHeader

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: 0,
				Kind:               "request_error",
				Message:            safeErr,
			})
			if attempt < geminiMaxRetries {
				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream request failed, retry %d/%d: %v", account.ID, attempt, geminiMaxRetries, err)
				sleepGeminiBackoff(attempt)
				continue
			}
			setOpsUpstreamError(c, 0, safeErr, "")
			return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed after retries: "+safeErr)
		}

		// Special-case: signature/thought_signature validation errors are not transient, but may be fixed by
		// downgrading Claude thinking/tool history to plain text (conservative two-stage retry).
		if resp.StatusCode == http.StatusBadRequest && signatureRetryStage < 2 {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()

			if isGeminiSignatureRelatedError(respBody) {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "signature_error",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				var strippedClaudeBody []byte
				stageName := ""
				switch signatureRetryStage {
				case 0:
					// Stage 1: disable thinking + thinking->text
					strippedClaudeBody = FilterThinkingBlocksForRetry(originalClaudeBody)
					stageName = "thinking-only"
					signatureRetryStage = 1
				default:
					// Stage 2: additionally downgrade tool_use/tool_result blocks to text
					strippedClaudeBody = FilterSignatureSensitiveBlocksForRetry(originalClaudeBody)
					stageName = "thinking+tools"
					signatureRetryStage = 2
				}
				retryGeminiReq, txErr := convertClaudeMessagesToGeminiGenerateContent(strippedClaudeBody)
				if txErr == nil {
					logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: detected signature-related 400, retrying with downgraded Claude blocks (%s)", account.ID, stageName)
					geminiReq = retryGeminiReq
					// Consume one retry budget attempt and continue with the updated request payload.
					sleepGeminiBackoff(1)
					continue
				}
			}

			// Restore body for downstream error handling.
			resp = &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		// 错误策略优先：匹配则跳过重试直接处理。
		if matched, rebuilt := s.checkErrorPolicyInLoop(ctx, account, resp); matched {
			resp = rebuilt
			break
		} else {
			resp = rebuilt
		}

		if resp.StatusCode >= 400 && s.shouldRetryGeminiUpstreamError(account, resp.StatusCode) {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()
			// Don't treat insufficient-scope as transient.
			if resp.StatusCode == 403 && isGeminiInsufficientScope(resp.Header, respBody) {
				resp = &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(respBody)),
				}
				break
			}
			if resp.StatusCode == 429 {
				// Mark as rate-limited early so concurrent requests avoid this account.
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			if attempt < geminiMaxRetries {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "retry",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, resp.StatusCode, attempt, geminiMaxRetries)
				sleepGeminiBackoff(attempt)
				continue
			}
			// Final attempt: surface the upstream error body (mapped below) instead of a generic retry error.
			resp = &http.Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header.Clone(),
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}
			break
		}

		break
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		// 统一错误策略：自定义错误码 + 临时不可调度
		if s.rateLimitService != nil {
			switch s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody) {
			case ErrorPolicySkipped:
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				return nil, s.writeGeminiMappedError(c, account, http.StatusInternalServerError, upstreamReqID, respBody)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
			}
		}

		// ErrorPolicyNone → 原有逻辑
		s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		// 精确匹配服务端配置类 400 错误，触发 failover + 临时封禁
		if resp.StatusCode == http.StatusBadRequest {
			msg400 := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			if isGoogleProjectConfigError(msg400) {
				upstreamReqID := resp.Header.Get(requestIDHeader)
				if upstreamReqID == "" {
					upstreamReqID = resp.Header.Get("x-goog-request-id")
				}
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  upstreamReqID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody, RetryableOnSameAccount: true}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
			upstreamReqID := resp.Header.Get(requestIDHeader)
			if upstreamReqID == "" {
				upstreamReqID = resp.Header.Get("x-goog-request-id")
			}
			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  upstreamReqID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
		}
		upstreamReqID := resp.Header.Get(requestIDHeader)
		if upstreamReqID == "" {
			upstreamReqID = resp.Header.Get("x-goog-request-id")
		}
		return nil, s.writeGeminiMappedError(c, account, resp.StatusCode, upstreamReqID, respBody)
	}

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	if req.Stream {
		streamRes, err := s.handleStreamingResponse(c, resp, startTime, originalModel)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	} else {
		if useUpstreamStream {
			collected, usageObj, err := collectGeminiSSE(resp.Body, true)
			if err != nil {
				return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream stream")
			}
			collectedBytes, _ := json.Marshal(collected)
			claudeResp, usageObj2 := convertGeminiToClaudeMessage(collected, originalModel, collectedBytes)
			c.JSON(http.StatusOK, claudeResp)
			usage = usageObj2
			if usageObj != nil && (usageObj.InputTokens > 0 || usageObj.OutputTokens > 0) {
				usage = usageObj
			}
		} else {
			usage, err = s.handleNonStreamingResponse(c, resp, originalModel)
			if err != nil {
				return nil, err
			}
		}
	}

	// 图片生成计费
	imageCount := 0
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	if isImageGenerationModel(originalModel) {
		imageCount = 1
	}

	return &ForwardResult{
		RequestID:      requestID,
		Usage:          *usage,
		Model:          originalModel,
		UpstreamModel:  mappedModel,
		Stream:         req.Stream,
		Duration:       time.Since(startTime),
		FirstTokenMs:   firstTokenMs,
		ImageCount:     imageCount,
		ImageSize:      imageSize,
		ImageInputSize: imageInputSize,
	}, nil
}

func isGeminiSignatureRelatedError(respBody []byte) bool {
	msg := strings.ToLower(strings.TrimSpace(extractAntigravityErrorMessage(respBody)))
	if msg == "" {
		msg = strings.ToLower(string(respBody))
	}
	return strings.Contains(msg, "thought_signature") || strings.Contains(msg, "signature")
}
