package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

func (s *OpenAIGatewayService) buildUpstreamRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token string, isStream bool, promptCacheKey string, isCodexCLI bool) (*http.Request, error) {
	// Determine target URL based on account type
	var targetURL string
	switch account.Type {
	case AccountTypeOAuth:
		// OAuth accounts use ChatGPT internal API
		targetURL = chatgptCodexURL
	case AccountTypeAPIKey:
		// API Key accounts use Platform API or custom base URL
		baseURL := account.GetOpenAIBaseURL()
		if baseURL == "" {
			targetURL = openaiPlatformAPIURL
		} else {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = buildOpenAIResponsesURL(validatedURL)
		}
	default:
		targetURL = openaiPlatformAPIURL
	}
	targetURL = appendOpenAIResponsesRequestPathSuffix(targetURL, openAIResponsesRequestPathSuffix(c))

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))

	// Set authentication header
	req.Header.Set("authorization", "Bearer "+token)

	// Set headers specific to OAuth accounts (ChatGPT internal API)
	if account.Type == AccountTypeOAuth {
		// Required: set Host for ChatGPT API (must use req.Host, not Header.Set)
		req.Host = "chatgpt.com"
		// Required: set chatgpt-account-id header
		chatgptAccountID := account.GetChatGPTAccountID()
		if chatgptAccountID != "" {
			req.Header.Set("chatgpt-account-id", chatgptAccountID)
		}
	}

	compatMessagesBridge := isOpenAICompatMessagesBridgeContext(c) || isOpenAICompatMessagesBridgeBody(body)

	// Whitelist passthrough headers
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiAllowedHeaders[lowerKey] {
			for _, v := range values {
				req.Header.Add(key, v)
			}
		}
	}
	if account.Type == AccountTypeOAuth {
		// 清除客户端透传的 session 头，后续用隔离后的值重新设置，防止跨用户会话碰撞。
		clientConversationID := strings.TrimSpace(req.Header.Get("conversation_id"))
		req.Header.Del("conversation_id")
		req.Header.Del("session_id")

		if compatMessagesBridge {
			req.Header.Del("OpenAI-Beta")
			req.Header.Del("originator")
		} else {
			req.Header.Set("OpenAI-Beta", "responses=experimental")
			req.Header.Set("originator", resolveOpenAIUpstreamOriginator(c, isCodexCLI))
		}
		apiKeyID := getAPIKeyIDFromContext(c)
		if isOpenAIResponsesCompactPath(c) {
			req.Header.Set("accept", "application/json")
			if req.Header.Get("version") == "" {
				req.Header.Set("version", codexCLIVersion)
			}
			compactSession := resolveOpenAICompactSessionID(c)
			req.Header.Set("session_id", isolateOpenAISessionID(apiKeyID, compactSession))
		} else {
			req.Header.Set("accept", "text/event-stream")
		}
		if promptCacheKey != "" {
			isolated := isolateOpenAISessionID(apiKeyID, promptCacheKey)
			req.Header.Set("session_id", isolated)
			if !compatMessagesBridge || clientConversationID != "" {
				req.Header.Set("conversation_id", isolated)
			}
		}
	}

	if account.Type == AccountTypeAPIKey && compatMessagesBridge {
		// Anthropic client identity/session headers have no meaning on a third-party
		// Responses endpoint and can trigger provider-specific validation. Preserve
		// them inside LightBridge context, but expose a stable Router identity upstream.
		req.Header.Del("originator")
		req.Header.Del("conversation_id")
		req.Header.Del("session_id")
		req.Header.Del("x-codex-turn-state")
		req.Header.Del("x-codex-turn-metadata")
		if isStream {
			req.Header.Set("accept", "text/event-stream")
		} else {
			req.Header.Set("accept", "application/json")
		}
		req.Header.Set("user-agent", openAIRouterBridgeUserAgent)
	}

	// Apply custom User-Agent if configured
	customUA := account.GetOpenAIUserAgent()
	if customUA != "" {
		req.Header.Set("user-agent", customUA)
	}

	// 若开启 ForceCodexCLI，则强制将上游 User-Agent 伪装为 Codex CLI。
	// 用于网关未透传/改写 User-Agent 时，仍能命中 Codex 侧识别逻辑。
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI && !compatMessagesBridge {
		req.Header.Set("user-agent", codexCLIUserAgent)
	}

	// 浏览器型 UA 兜底：仅 OAuth（ChatGPT 内部接口）账号生效，若最终 user-agent 仍为浏览器
	// （Chrome/Firefox/Safari/Edge 等），替换为后台配置的 Codex UA，避免 Cloudflare 触发 JS 质询。
	s.overrideBrowserUserAgent(ctx, account, req)

	// Ensure required headers exist
	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}

	return req, nil
}

// overrideBrowserUserAgent 检查请求的最终 user-agent，若为浏览器 UA 则替换为后台配置的 Codex UA。
// 用于规避 Cloudflare 对浏览器型 UA 在 ChatGPT 内部接口上的访问质询。
// 影响范围严格限定：仅 OAuth（Codex/ChatGPT 内部接口）账号生效；API Key 等其他账号原样透传。
// 仅在识别为浏览器（Mozilla/...）时改写，其他 CLI/工具 UA 不动。
func (s *OpenAIGatewayService) overrideBrowserUserAgent(ctx context.Context, account *Account, req *http.Request) {
	if req == nil || account == nil {
		return
	}
	if account.Type != AccountTypeOAuth {
		return
	}
	currentUA := req.Header.Get("user-agent")
	if !openai.IsBrowserUserAgent(currentUA) {
		return
	}
	codexUA := DefaultOpenAICodexUserAgent
	if s != nil && s.settingService != nil {
		if v := strings.TrimSpace(s.settingService.GetOpenAICodexUserAgent(ctx)); v != "" {
			codexUA = v
		}
	}
	req.Header.Set("user-agent", codexUA)
}

func (s *OpenAIGatewayService) handleErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestBody []byte,
	requestedModel ...string,
) (*OpenAIForwardResult, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	logOpenAIInstructionsRequiredDebug(ctx, c, account, resp.StatusCode, upstreamMsg, requestBody, body)

	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		logger.LegacyPrintf("service.openai_gateway",
			"OpenAI upstream error %d (account=%d platform=%s type=%s): %s",
			resp.StatusCode,
			account.ID,
			account.Platform,
			account.Type,
			truncateForLog(body, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes),
		)
	}

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		PlatformOpenAI,
		resp.StatusCode,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		c.JSON(status, gin.H{
			"error": gin.H{
				"type":    errType,
				"message": errMsg,
			},
		})
		if upstreamMsg == "" {
			upstreamMsg = errMsg
		}
		if upstreamMsg == "" {
			return nil, fmt.Errorf("upstream error: %d (passthrough rule matched)", resp.StatusCode)
		}
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}

	// Check custom error codes
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "upstream_error",
				"message": "Upstream gateway error",
			},
		})
		if upstreamMsg == "" {
			return nil, fmt.Errorf("upstream error: %d (not in custom error codes)", resp.StatusCode)
		}
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	// Handle upstream error (mark account status)
	var reqModel string
	if len(requestedModel) > 0 {
		reqModel = strings.TrimSpace(requestedModel[0])
	}
	if reqModel == "" {
		reqModel, _, _ = extractOpenAIRequestMetaFromBody(requestBody)
	}
	shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, reqModel)
	kind := "http_error"
	if shouldDisable {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.EffectivePlatform(),
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               kind,
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	if shouldDisable {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}

	// Return appropriate error response
	var errType, errMsg string
	var statusCode int

	switch resp.StatusCode {
	case 401:
		statusCode = http.StatusBadGateway
		errType = "upstream_error"
		errMsg = "Upstream authentication failed, please contact administrator"
	case 402:
		statusCode = http.StatusBadGateway
		errType = "upstream_error"
		errMsg = "Upstream payment required: insufficient balance or billing issue"
	case 403:
		statusCode = http.StatusBadGateway
		errType = "upstream_error"
		errMsg = "Upstream access forbidden, please contact administrator"
	case 429:
		statusCode = http.StatusTooManyRequests
		errType = "rate_limit_error"
		errMsg = "Upstream rate limit exceeded, please retry later"
	default:
		statusCode = http.StatusBadGateway
		errType = "upstream_error"
		errMsg = "Upstream request failed"
	}

	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": errMsg,
		},
	})

	if upstreamMsg == "" {
		return nil, fmt.Errorf("upstream error: %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
}

// compatErrorWriter is the signature for format-specific error writers used by
// the compat paths (Chat Completions and Anthropic Messages).
type compatErrorWriter func(c *gin.Context, statusCode int, errType, message string)

// handleCompatErrorResponse is the shared non-failover error handler for the
// Chat Completions and Anthropic Messages compat paths. It mirrors the logic of
// handleErrorResponse (passthrough rules, ShouldHandleErrorCode, rate-limit
// tracking, secondary failover) but delegates the final error write to the
// format-specific writer function.
func (s *OpenAIGatewayService) handleCompatErrorResponse(
	resp *http.Response,
	c *gin.Context,
	account *Account,
	writeError compatErrorWriter,
	requestedModel ...string,
) (*OpenAIForwardResult, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("Upstream error: %d", resp.StatusCode)
	}
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)

	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)

	// Apply error passthrough rules
	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c, account.Platform, resp.StatusCode, body,
		http.StatusBadGateway, "api_error", "Upstream request failed",
	); matched {
		writeError(c, status, errType, errMsg)
		if upstreamMsg == "" {
			upstreamMsg = errMsg
		}
		if upstreamMsg == "" {
			return nil, fmt.Errorf("upstream error: %d (passthrough rule matched)", resp.StatusCode)
		}
		return nil, fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", resp.StatusCode, upstreamMsg)
	}

	// Check custom error codes — if the account does not handle this status,
	// return a generic error without exposing upstream details.
	if !account.ShouldHandleErrorCode(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		writeError(c, http.StatusInternalServerError, "api_error", "Upstream gateway error")
		if upstreamMsg == "" {
			return nil, fmt.Errorf("upstream error: %d (not in custom error codes)", resp.StatusCode)
		}
		return nil, fmt.Errorf("upstream error: %d (not in custom error codes) message=%s", resp.StatusCode, upstreamMsg)
	}

	// Track rate limits and decide whether to trigger secondary failover.
	var modelForCooldown string
	if len(requestedModel) > 0 {
		modelForCooldown = requestedModel[0]
	}
	shouldDisable := s.handleOpenAIAccountUpstreamError(
		c.Request.Context(), account, resp.StatusCode, resp.Header, body, modelForCooldown,
	)
	kind := "http_error"
	if shouldDisable {
		kind = "failover"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.EffectivePlatform(),
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               kind,
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	if shouldDisable {
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           body,
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}

	// Map status code to error type and write response
	errType := "api_error"
	switch {
	case resp.StatusCode == 400:
		errType = "invalid_request_error"
	case resp.StatusCode == 404:
		errType = "not_found_error"
	case resp.StatusCode == 429:
		errType = "rate_limit_error"
	case resp.StatusCode >= 500:
		errType = "api_error"
	}

	writeError(c, resp.StatusCode, errType, upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}
