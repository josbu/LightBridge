package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/claude"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ForwardCountTokens 转发 count_tokens 请求到上游 API
// 特点：不记录使用量、仅支持非流式响应
func (s *GatewayService) ForwardCountTokens(ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest) error {
	if parsed == nil {
		s.countTokensError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return fmt.Errorf("parse request: empty request")
	}

	if account != nil && account.IsAnthropicAPIKeyPassthroughEnabled() {
		passthroughBody := parsed.Body
		if reqModel := parsed.Model; reqModel != "" {
			if mappedModel := account.GetMappedModel(reqModel); mappedModel != reqModel {
				passthroughBody = s.replaceModelInBody(passthroughBody, mappedModel)
				logger.LegacyPrintf("service.gateway", "CountTokens passthrough model mapping: %s -> %s (account: %s)", reqModel, mappedModel, account.Name)
			}
		}
		return s.forwardCountTokensAnthropicAPIKeyPassthrough(ctx, c, account, passthroughBody)
	}

	// Bedrock 不支持 count_tokens 端点
	if account != nil && account.IsBedrock() {
		s.countTokensError(c, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported for Bedrock")
		return nil
	}

	body := parsed.Body
	reqModel := parsed.Model

	// Pre-filter: strip empty text blocks to prevent upstream 400.
	body = StripEmptyTextBlocks(body)

	isClaudeCodeCT := IsClaudeCodeClient(ctx) || isClaudeCodeClient(c.GetHeader("User-Agent"), parsed.MetadataUserID)
	shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCodeCT

	if shouldMimicClaudeCode {
		normalizeOpts := claudeOAuthNormalizeOptions{stripSystemCacheControl: true}
		body, reqModel = normalizeClaudeOAuthRequestBody(body, reqModel, normalizeOpts)

		body = s.rewriteMessageCacheControlIfEnabled(ctx, body)
		if rw := buildToolNameRewriteFromBody(body); rw != nil {
			body = applyToolNameRewriteToBody(body, rw)
		} else {
			body = applyToolsLastCacheBreakpoint(body)
		}
	}

	// Antigravity 账户不支持 count_tokens，返回 404 让客户端 fallback 到本地估算。
	// 返回 nil 避免 handler 层记录为错误，也不设置 ops 上游错误上下文。
	if account.IsAntigravity() {
		s.countTokensError(c, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported for this platform")
		return nil
	}

	// 应用模型映射：
	// - APIKey 账号：使用账号级别的显式映射（如果配置），否则透传原始模型名
	// - OAuth/SetupToken 账号：使用 Anthropic 标准映射（短ID → 长ID）
	if reqModel != "" {
		mappedModel := reqModel
		mappingSource := ""
		if account.Type == AccountTypeAPIKey {
			mappedModel = account.GetMappedModel(reqModel)
			if mappedModel != reqModel {
				mappingSource = "account"
			}
		}
		if mappingSource == "" && account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
			normalized := claude.NormalizeModelID(reqModel)
			if normalized != reqModel {
				mappedModel = normalized
				mappingSource = "prefix"
			}
		}
		if mappedModel != reqModel {
			body = s.replaceModelInBody(body, mappedModel)
			reqModel = mappedModel
			logger.LegacyPrintf("service.gateway", "CountTokens model mapping applied: %s -> %s (account: %s, source=%s)", parsed.Model, mappedModel, account.Name, mappingSource)
		}
	}

	// 获取凭证
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return err
	}

	// 构建上游请求
	upstreamReq, err := s.buildCountTokensRequest(ctx, c, account, body, token, tokenType, reqModel, shouldMimicClaudeCode)
	if err != nil {
		s.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}

	// 获取代理URL（自定义 base URL 模式下，proxy 通过 buildCustomRelayURL 作为查询参数传递）
	proxyURL := ""
	if !account.IsCustomBaseURLEnabled() || account.GetCustomBaseURL() == "" {
		proxyURL, err = s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
		if err != nil {
			s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to resolve proxy")
			return err
		}
	}

	// 发送请求
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		setOpsUpstreamError(c, 0, sanitizeUpstreamErrorMessage(err.Error()), "")
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Request failed")
		return fmt.Errorf("upstream request failed: %w", err)
	}

	// 读取响应体
	countTokensTooLarge := func(c *gin.Context) {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Upstream response too large")
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, countTokensTooLarge)
	_ = resp.Body.Close()
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
		}
		return err
	}

	// 检测 thinking block 签名错误（400）并重试一次（过滤 thinking blocks）
	if resp.StatusCode == 400 && s.shouldRectifySignatureError(ctx, account, respBody) {
		logger.LegacyPrintf("service.gateway", "Account %d: detected thinking block signature error on count_tokens, retrying with filtered thinking blocks", account.ID)

		filteredBody := FilterThinkingBlocksForRetry(body)
		retryReq, buildErr := s.buildCountTokensRequest(ctx, c, account, filteredBody, token, tokenType, reqModel, shouldMimicClaudeCode)
		if buildErr == nil {
			retryResp, retryErr := s.httpUpstream.DoWithTLS(retryReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
			if retryErr == nil {
				resp = retryResp
				respBody, err = ReadUpstreamResponseBody(resp.Body, s.cfg, c, countTokensTooLarge)
				_ = resp.Body.Close()
				if err != nil {
					if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
						s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
					}
					return err
				}
			}
		}
	}

	// 处理错误响应
	if resp.StatusCode >= 400 {
		// 标记账号状态（429/529等）
		s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)

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
		setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)

		// 记录上游错误摘要便于排障（不回显请求内容）
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			logger.LegacyPrintf("service.gateway",
				"count_tokens upstream error %d (account=%d platform=%s type=%s): %s",
				resp.StatusCode,
				account.ID,
				account.Platform,
				account.Type,
				truncateForLog(respBody, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes),
			)
		}

		// 返回简化的错误响应
		errMsg := "Upstream request failed"
		switch resp.StatusCode {
		case 429:
			errMsg = "Rate limit exceeded"
		case 529:
			errMsg = "Service overloaded"
		}
		s.countTokensError(c, resp.StatusCode, "upstream_error", errMsg)
		if upstreamMsg == "" {
			return fmt.Errorf("upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
	}

	// 透传成功响应
	c.Data(resp.StatusCode, "application/json", respBody)
	return nil
}

func (s *GatewayService) forwardCountTokensAnthropicAPIKeyPassthrough(ctx context.Context, c *gin.Context, account *Account, body []byte) error {
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to get access token")
		return err
	}
	if tokenType != "apikey" {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Invalid account token type")
		return fmt.Errorf("anthropic api key passthrough requires apikey token, got: %s", tokenType)
	}

	upstreamReq, err := s.buildCountTokensRequestAnthropicAPIKeyPassthrough(ctx, c, account, body, token)
	if err != nil {
		s.countTokensError(c, http.StatusInternalServerError, "api_error", "Failed to build request")
		return err
	}

	proxyURL := ""
	proxyURL, err = s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
	if err != nil {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to resolve proxy")
		return err
	}

	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		setOpsUpstreamError(c, 0, sanitizeUpstreamErrorMessage(err.Error()), "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Passthrough:        true,
			Kind:               "request_error",
			Message:            sanitizeUpstreamErrorMessage(err.Error()),
		})
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Request failed")
		return fmt.Errorf("upstream request failed: %w", err)
	}

	countTokensTooLarge := func(c *gin.Context) {
		s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Upstream response too large")
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, countTokensTooLarge)
	_ = resp.Body.Close()
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			s.countTokensError(c, http.StatusBadGateway, "upstream_error", "Failed to read response")
		}
		return err
	}

	if resp.StatusCode >= 400 {
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)

		// 中转站不支持 count_tokens 端点时（404），返回 404 让客户端 fallback 到本地估算。
		// 仅在错误消息明确指向 count_tokens endpoint 不存在时生效，避免误吞其他 404（如错误 base_url）。
		// 返回 nil 避免 handler 层记录为错误，也不设置 ops 上游错误上下文。
		if isCountTokensUnsupported404(resp.StatusCode, respBody) {
			logger.LegacyPrintf("service.gateway",
				"[count_tokens] Upstream does not support count_tokens (404), returning 404: account=%d name=%s msg=%s",
				account.ID, account.Name, truncateString(upstreamMsg, 512))
			s.countTokensError(c, http.StatusNotFound, "not_found_error", "count_tokens endpoint is not supported by upstream")
			return nil
		}

		upstreamDetail := ""
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
			if maxBytes <= 0 {
				maxBytes = 2048
			}
			upstreamDetail = truncateString(string(respBody), maxBytes)
		}
		setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Passthrough:        true,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})

		errMsg := "Upstream request failed"
		switch resp.StatusCode {
		case 429:
			errMsg = "Rate limit exceeded"
		case 529:
			errMsg = "Service overloaded"
		}
		s.countTokensError(c, resp.StatusCode, "upstream_error", errMsg)
		if upstreamMsg == "" {
			return fmt.Errorf("upstream error: %d", resp.StatusCode)
		}
		return fmt.Errorf("upstream error: %d message=%s", resp.StatusCode, upstreamMsg)
	}

	writeAnthropicPassthroughResponseHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, respBody)
	return nil
}

func (s *GatewayService) buildCountTokensRequestAnthropicAPIKeyPassthrough(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	token string,
) (*http.Request, error) {
	targetURL := claudeAPICountTokensURL
	baseURL := account.GetBaseURL()
	if baseURL != "" {
		validatedURL, err := s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return nil, err
		}
		targetURL = validatedURL + "/v1/messages/count_tokens?beta=true"
	}
	body = sanitizeCountTokensRequestBody(body)

	// 同 buildUpstreamRequestAnthropicAPIKeyPassthrough：能力维度 sanitize。
	clientBeta := ""
	if c != nil && c.Request != nil {
		clientBeta = getHeaderRaw(c.Request.Header, "anthropic-beta")
	}
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(body, clientBeta); changed {
		body = sanitized
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if !allowedHeaders[lowerKey] {
				continue
			}
			wireKey := resolveWireCasing(key)
			for _, v := range values {
				addHeaderRaw(req.Header, wireKey, v)
			}
		}
	}

	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Set("x-api-key", token)

	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}
	if req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	return req, nil
}

// buildCountTokensRequest 构建 count_tokens 上游请求
func (s *GatewayService) buildCountTokensRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, token, tokenType, modelID string, mimicClaudeCode bool) (*http.Request, error) {
	// 确定目标 URL
	targetURL := claudeAPICountTokensURL
	if account.Type == AccountTypeAPIKey {
		baseURL := account.GetBaseURL()
		if baseURL != "" {
			validatedURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, err
			}
			targetURL = validatedURL + "/v1/messages/count_tokens?beta=true"
		}
	} else if account.IsCustomBaseURLEnabled() {
		customURL := account.GetCustomBaseURL()
		if customURL == "" {
			return nil, fmt.Errorf("custom_base_url is enabled but not configured for account %d", account.ID)
		}
		validatedURL, err := s.validateUpstreamBaseURL(customURL)
		if err != nil {
			return nil, err
		}
		targetURL = s.buildCustomRelayURL(validatedURL, "/v1/messages/count_tokens", account)
	}

	clientHeaders := http.Header{}
	if c != nil && c.Request != nil {
		clientHeaders = c.Request.Header
	}

	// OAuth 账号：应用统一指纹和重写 userID（受设置开关控制）
	// 如果启用了会话ID伪装，会在重写后替换 session 部分为固定值
	ctEnableFP, ctEnableMPT, ctEnableCCH := true, false, false
	if s.settingService != nil {
		ctEnableFP, ctEnableMPT, ctEnableCCH = s.settingService.GetGatewayForwardingSettings(ctx)
	}
	var ctFingerprint *Fingerprint
	if account.IsOAuth() && s.identityService != nil {
		fp, err := s.identityService.GetOrCreateFingerprint(ctx, account.ID, clientHeaders)
		if err == nil {
			ctFingerprint = fp
			if !ctEnableMPT {
				accountUUID := account.GetExtraString("account_uuid")
				if accountUUID != "" && fp.ClientID != "" {
					if newBody, err := s.identityService.RewriteUserIDWithMasking(ctx, body, account, accountUUID, fp.ClientID, fp.UserAgent); err == nil && len(newBody) > 0 {
						body = newBody
					}
				}
			}
		}
	}

	// 同步 billing header cc_version 与实际发送的 User-Agent 版本
	if ctFingerprint != nil && ctEnableFP {
		body = syncBillingHeaderVersion(body, ctFingerprint.UserAgent)
	}

	// === 计算最终 anthropic-beta header（先于 body sanitize 与 CCH 签名）===
	// 顺序约束同 buildUpstreamRequest。
	ctEffectiveDropSet := mergeDropSets(s.getBetaPolicyFilterSet(ctx, c, account, modelID))
	finalBetaHeader, finalBetaShouldSet := s.computeFinalCountTokensAnthropicBeta(
		tokenType, mimicClaudeCode, modelID, clientHeaders, body, ctEffectiveDropSet,
	)

	// 能力维度 body sanitize：与最终 anthropic-beta header 对称
	if sanitized, changed := sanitizeAnthropicBodyForBetaTokens(body, finalBetaHeader); changed {
		body = sanitized
	}

	if ctEnableCCH {
		body = signBillingHeaderCCH(body)
	}
	body = sanitizeCountTokensRequestBody(body)

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// 设置认证头（保持原始大小写）
	if tokenType == "oauth" {
		setHeaderRaw(req.Header, "authorization", "Bearer "+token)
	} else {
		setHeaderRaw(req.Header, "x-api-key", token)
	}

	// 白名单透传 headers（恢复真实 wire casing）
	for key, values := range clientHeaders {
		lowerKey := strings.ToLower(key)
		if allowedHeaders[lowerKey] {
			wireKey := resolveWireCasing(key)
			for _, v := range values {
				addHeaderRaw(req.Header, wireKey, v)
			}
		}
	}

	// OAuth 账号：应用指纹到请求头（受设置开关控制）
	if ctEnableFP && ctFingerprint != nil {
		s.identityService.ApplyFingerprint(req, ctFingerprint)
	}

	// 确保必要的 headers 存在（保持原始大小写）
	if getHeaderRaw(req.Header, "content-type") == "" {
		setHeaderRaw(req.Header, "content-type", "application/json")
	}
	if getHeaderRaw(req.Header, "anthropic-version") == "" {
		setHeaderRaw(req.Header, "anthropic-version", "2023-06-01")
	}
	if tokenType == "oauth" {
		applyClaudeOAuthHeaderDefaults(req)
	}

	// OAuth + mimic Claude Code：强制注入 CLI 指纹 header
	if tokenType == "oauth" && mimicClaudeCode {
		applyClaudeCodeMimicHeaders(req, false)
	}

	// 写入最终 anthropic-beta header（Del 一次避免白名单透传值残留）
	deleteHeaderAllForms(req.Header, "anthropic-beta")
	if finalBetaShouldSet {
		setHeaderRaw(req.Header, "anthropic-beta", finalBetaHeader)
	}

	// 同步 X-Claude-Code-Session-Id 头：取 body 中已处理的 metadata.user_id 的 session_id 覆盖
	if sessionHeader := getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"); sessionHeader != "" {
		if uid := gjson.GetBytes(body, "metadata.user_id").String(); uid != "" {
			if parsed := ParseMetadataUserID(uid); parsed != nil {
				setHeaderRaw(req.Header, "X-Claude-Code-Session-Id", parsed.SessionID)
			}
		}
	}

	if c != nil && tokenType == "oauth" {
		c.Set(claudeMimicDebugInfoKey, buildClaudeMimicDebugLine(req, body, account, tokenType, mimicClaudeCode))
	}
	if s.debugClaudeMimicEnabled() {
		logClaudeMimicDebug(req, body, account, tokenType, mimicClaudeCode)
	}

	return req, nil
}

func sanitizeCountTokensRequestBody(body []byte) []byte {
	out := body
	for _, path := range []string{
		"temperature",
		"top_p",
		"top_k",
		"stream",
		"stop_sequences",
		"stop",
	} {
		if gjson.GetBytes(out, path).Exists() {
			if next, ok := deleteJSONPathBytes(out, path); ok {
				out = next
			}
		}
	}
	return out
}

// countTokensError 返回 count_tokens 错误响应
func (s *GatewayService) countTokensError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

// buildCustomRelayURL 构建自定义中继转发 URL
// 在 path 后附加 beta=true 和可选的 proxy 查询参数
func (s *GatewayService) buildCustomRelayURL(baseURL, path string, account *Account) string {
	u := strings.TrimRight(baseURL, "/") + path + "?beta=true"
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL := account.Proxy.URL()
		if proxyURL != "" {
			u += "&proxy=" + url.QueryEscape(proxyURL)
		}
	}
	return u
}

func (s *GatewayService) validateUpstreamBaseURL(raw string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}
