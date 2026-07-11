package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/geminicli"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

func (s *GeminiMessagesCompatService) ForwardNative(ctx context.Context, c *gin.Context, account *Account, originalModel string, action string, stream bool, body []byte) (*ForwardResult, error) {
	startTime := time.Now()

	if strings.TrimSpace(originalModel) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing model in URL")
	}
	if strings.TrimSpace(action) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing action in URL")
	}
	if len(body) == 0 {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Request body is empty")
	}

	// 过滤掉 parts 为空的消息（Gemini API 不接受空 parts）
	if filteredBody, err := filterEmptyPartsFromGeminiRequest(body); err == nil {
		body = filteredBody
	}

	switch action {
	case "generateContent", "streamGenerateContent", "countTokens":
		// ok
	default:
		return nil, s.writeGoogleError(c, http.StatusNotFound, "Unsupported action: "+action)
	}

	// Some Gemini upstreams validate tool call parts strictly; ensure any `functionCall` part includes a
	// `thoughtSignature` to avoid frequent INVALID_ARGUMENT 400s.
	body = ensureGeminiFunctionCallThoughtSignatures(body)

	mappedModel := originalModel
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(originalModel)
	}

	proxyURL, err := s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
	if err != nil {
		return nil, err
	}

	useUpstreamStream := stream
	upstreamAction := action
	if account.Type == AccountTypeOAuth && !stream && action == "generateContent" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
		upstreamAction = "streamGenerateContent"
	}
	forceAIStudio := action == "countTokens"

	var requestIDHeader string
	var buildReq func(ctx context.Context) (*http.Request, string, error)

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

			fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, upstreamAction)
			if useUpstreamStream {
				fullURL += "?alt=sse"
			}

			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
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

			// Two modes for OAuth:
			// 1. With project_id -> Code Assist API (wrapped request)
			// 2. Without project_id -> AI Studio API (direct OAuth, like API key but with Bearer token)
			if projectID != "" && !forceAIStudio {
				// Mode 1: Code Assist API
				baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
				if err != nil {
					return nil, "", err
				}
				fullURL := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), upstreamAction)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				wrapped := map[string]any{
					"model":   mappedModel,
					"project": projectID,
				}
				var inner any
				if err := json.Unmarshal(body, &inner); err != nil {
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

				fullURL := fmt.Sprintf("%s/v1beta/models/%s:%s", strings.TrimRight(normalizedBaseURL, "/"), mappedModel, upstreamAction)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
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

			fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, upstreamAction, useUpstreamStream)
			if err != nil {
				return nil, "", err
			}

			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	default:
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Unsupported account type: "+account.Type)
	}

	var resp *http.Response
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Local build error: don't retry.
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeGoogleError(c, http.StatusBadRequest, err.Error())
			}
			return nil, s.writeGoogleError(c, http.StatusBadGateway, err.Error())
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
			if action == "countTokens" {
				estimated := estimateGeminiCountTokens(body)
				c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{
					RequestID:     "",
					Usage:         ClaudeUsage{},
					Model:         originalModel,
					UpstreamModel: mappedModel,
					Stream:        false,
					Duration:      time.Since(startTime),
					FirstTokenMs:  nil,
				}, nil
			}
			setOpsUpstreamError(c, 0, safeErr, "")
			return nil, s.writeGoogleError(c, http.StatusBadGateway, "Upstream request failed after retries: "+safeErr)
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
			if action == "countTokens" {
				estimated := estimateGeminiCountTokens(body)
				c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{
					RequestID:     "",
					Usage:         ClaudeUsage{},
					Model:         originalModel,
					UpstreamModel: mappedModel,
					Stream:        false,
					Duration:      time.Since(startTime),
					FirstTokenMs:  nil,
				}, nil
			}
			// Final attempt: surface the upstream error body (passed through below) instead of a generic retry error.
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

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	isOAuth := account.Type == AccountTypeOAuth

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		// Best-effort fallback for OAuth tokens missing AI Studio scopes when calling countTokens.
		// This avoids Gemini SDKs failing hard during preflight token counting.
		// Checked before error policy so it always works regardless of custom error codes.
		if action == "countTokens" && isOAuth && isGeminiInsufficientScope(resp.Header, respBody) {
			estimated := estimateGeminiCountTokens(body)
			c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
			return &ForwardResult{
				RequestID:     requestID,
				Usage:         ClaudeUsage{},
				Model:         originalModel,
				UpstreamModel: mappedModel,
				Stream:        false,
				Duration:      time.Since(startTime),
				FirstTokenMs:  nil,
			}, nil
		}

		// 统一错误策略：自定义错误码 + 临时不可调度
		if s.rateLimitService != nil {
			switch s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody) {
			case ErrorPolicySkipped:
				respBody = unwrapIfNeeded(isOAuth, respBody)
				contentType := resp.Header.Get("Content-Type")
				if contentType == "" {
					contentType = "application/json"
				}
				c.Data(http.StatusInternalServerError, contentType, respBody)
				return nil, fmt.Errorf("gemini upstream error: %d (skipped by error policy)", resp.StatusCode)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				s.handleGeminiUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(evBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  requestID,
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
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  requestID,
					Kind:               "failover",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: evBody, RetryableOnSameAccount: true}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(resp.StatusCode) {
			evBody := unwrapIfNeeded(isOAuth, respBody)
			upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(evBody))
			upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(evBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: evBody}
		}

		respBody = unwrapIfNeeded(isOAuth, respBody)
		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		upstreamDetail := ""
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
			if maxBytes <= 0 {
				maxBytes = 2048
			}
			upstreamDetail = truncateString(string(respBody), maxBytes)
			logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini] native upstream error %d: %s", resp.StatusCode, truncateForLog(respBody, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes))
		}
		setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestID,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})

		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, respBody)
		if upstreamMsg == "" {
			return nil, fmt.Errorf("gemini upstream error: %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("gemini upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int

	if stream {
		streamRes, err := s.handleNativeStreamingResponse(c, resp, startTime, isOAuth)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	} else {
		if useUpstreamStream {
			collected, usageObj, err := collectGeminiSSE(resp.Body, isOAuth)
			if err != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to read upstream stream")
			}
			b, _ := json.Marshal(collected)
			c.Data(http.StatusOK, "application/json", b)
			usage = usageObj
		} else {
			usageResp, err := s.handleNativeNonStreamingResponse(c, resp, isOAuth)
			if err != nil {
				return nil, err
			}
			usage = usageResp
		}
	}

	if usage == nil {
		usage = &ClaudeUsage{}
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
		Stream:         stream,
		Duration:       time.Since(startTime),
		FirstTokenMs:   firstTokenMs,
		ImageCount:     imageCount,
		ImageSize:      imageSize,
		ImageInputSize: imageInputSize,
	}, nil
}

// checkErrorPolicyInLoop 在重试循环内预检查错误策略。
// 返回 true 表示策略已匹配（调用者应 break），resp 已重建可直接使用。
// 返回 false 表示 ErrorPolicyNone，resp 已重建，调用者继续走重试逻辑。
func (s *GeminiMessagesCompatService) checkErrorPolicyInLoop(
	ctx context.Context, account *Account, resp *http.Response,
) (matched bool, rebuilt *http.Response) {
	if resp.StatusCode < 400 || s.rateLimitService == nil {
		return false, resp
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	_ = resp.Body.Close()
	rebuilt = &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	policy := s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, body)
	return policy != ErrorPolicyNone, rebuilt
}

func (s *GeminiMessagesCompatService) shouldRetryGeminiUpstreamError(account *Account, statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504, 529:
		return true
	case 403:
		// GeminiCli OAuth occasionally returns 403 transiently (activation/quota propagation); allow retry.
		if account == nil || account.Type != AccountTypeOAuth {
			return false
		}
		oauthType := strings.ToLower(strings.TrimSpace(account.GetCredential("oauth_type")))
		if oauthType == "" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
			// Legacy/implicit Code Assist OAuth accounts.
			oauthType = "code_assist"
		}
		return oauthType == "code_assist"
	default:
		return false
	}
}

func (s *GeminiMessagesCompatService) shouldFailoverGeminiUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

func sleepGeminiBackoff(attempt int) {
	delay := geminiRetryBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > geminiRetryMaxDelay {
		delay = geminiRetryMaxDelay
	}

	// +/- 20% jitter
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	jitter := time.Duration(float64(delay) * 0.2 * (r.Float64()*2 - 1))
	sleepFor := delay + jitter
	if sleepFor < 0 {
		sleepFor = 0
	}
	time.Sleep(sleepFor)
}

var (
	sensitiveQueryParamRegex = regexp.MustCompile(`(?i)([?&](?:key|client_secret|access_token|refresh_token)=)[^&"\s]+`)
	retryInRegex             = regexp.MustCompile(`Please retry in ([0-9.]+)s`)
)
