package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/geminicli"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/tlsfingerprint"
	"github.com/WilliamWang1721/LightBridge/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const fullPassthroughUsageCaptureLimit = 8 << 20

type FullPassthroughInput struct {
	Body          []byte
	Token         string
	TokenType     string
	RequestModel  string
	RequestStream bool
}

// ForwardFullPassthrough preserves the inbound protocol wire format and sends
// it to the selected account with only authentication and transport headers
// rewritten. It is intentionally narrow and complements the existing provider
// passthrough implementations, which keep their richer provider-specific
// behavior for normal same-protocol paths.
func (s *GatewayService) ForwardFullPassthrough(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	input FullPassthroughInput,
) (*ForwardResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, errors.New("gateway service is not configured")
	}
	if account == nil {
		return nil, errors.New("account is required")
	}
	startTime := time.Now()
	body := input.Body
	reqModel := strings.TrimSpace(input.RequestModel)
	if reqModel == "" {
		reqModel = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	}
	reqStream := input.RequestStream
	if !reqStream {
		reqStream = gjson.GetBytes(body, "stream").Bool() || fullPassthroughGeminiStreamRequest(c)
	}

	targetURL, err := s.buildFullPassthroughURL(c, account)
	if err != nil {
		return nil, err
	}
	method := http.MethodPost
	if c != nil && c.Request != nil && strings.TrimSpace(c.Request.Method) != "" {
		method = c.Request.Method
	}
	upstreamReq, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if account.IsOpenAI() {
		upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	}
	copyFullPassthroughRequestHeaders(upstreamReq.Header, c)
	applyFullPassthroughAuthHeaders(upstreamReq, account, input.Token, input.TokenType)

	proxyURL, err := s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
	if err != nil {
		return nil, err
	}
	if c != nil {
		c.Set("full_passthrough", true)
		switch {
		case account.IsOpenAI():
			c.Set("openai_passthrough", true)
		case account.IsAnthropic():
			c.Set("anthropic_passthrough", true)
		}
	}

	upstreamStart := time.Now()
	var tlsProfile *tlsfingerprint.Profile
	if s.tlsFPProfileService != nil {
		tlsProfile = s.tlsFPProfileService.ResolveTLSProfile(account)
	}
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, tlsProfile)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(targetURL),
			Passthrough:        true,
			Kind:               "request_error",
			Message:            safeErr,
		})
		return nil, fmt.Errorf("full passthrough upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	usage, err := s.writeFullPassthroughResponse(c, resp, reqStream)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		setOpsUpstreamError(c, resp.StatusCode, "", "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			UpstreamURL:        safeUpstreamURL(targetURL),
			Passthrough:        true,
			Kind:               "full_passthrough",
		})
	}

	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         usage,
		Model:         reqModel,
		UpstreamModel: reqModel,
		Stream:        reqStream,
		Duration:      time.Since(startTime),
	}, nil
}

func (s *GatewayService) buildFullPassthroughURL(c *gin.Context, account *Account) (string, error) {
	baseURL, validate, err := fullPassthroughBaseURL(c, account)
	if err != nil {
		return "", err
	}
	if validate {
		baseURL, err = s.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return "", err
		}
	}
	path, query := fullPassthroughRequestPath(c)
	return appendFullPassthroughPath(baseURL, path, query)
}

func fullPassthroughBaseURL(c *gin.Context, account *Account) (baseURL string, validate bool, err error) {
	if account == nil {
		return "", false, errors.New("account is required")
	}
	if account.IsCustom() {
		baseURL = strings.TrimSpace(account.GetCredential("base_url"))
		if baseURL == "" {
			return "", false, errors.New("custom account base_url is required for full passthrough")
		}
		return baseURL, true, nil
	}
	switch {
	case account.IsOpenAI():
		if account.Type == AccountTypeOAuth {
			return "https://chatgpt.com", false, nil
		}
		return account.GetOpenAIBaseURL(), true, nil
	case account.IsGemini():
		return account.GetGeminiBaseURL(geminicli.AIStudioBaseURL), true, nil
	case account.IsAnthropic():
		return account.GetBaseURL(), true, nil
	default:
		return strings.TrimSpace(account.GetCredential("base_url")), true, nil
	}
}

func fullPassthroughRequestPath(c *gin.Context) (string, string) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return "/", ""
	}
	path := c.Request.URL.Path
	if path == "" {
		path = "/"
	}
	if c.Request.URL.RawPath != "" {
		if unescaped, err := url.PathUnescape(c.Request.URL.RawPath); err == nil && unescaped != "" {
			path = unescaped
		}
	}
	return path, c.Request.URL.RawQuery
}

func appendFullPassthroughPath(baseURL, requestPath, rawQuery string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", errors.New("base_url is required for full passthrough")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base_url: %w", err)
	}
	reqPath := "/" + strings.TrimLeft(strings.TrimSpace(requestPath), "/")
	if reqPath == "/" {
		reqPath = "/"
	}
	basePath := strings.TrimRight(u.Path, "/")
	if basePath != "" && (reqPath == basePath || strings.HasPrefix(reqPath, basePath+"/")) {
		u.Path = reqPath
	} else {
		u.Path = basePath + reqPath
	}
	u.RawQuery = rawQuery
	return u.String(), nil
}

var fullPassthroughSkippedRequestHeaders = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
	"proxy-authorization": {},
	"host":                {},
	"content-length":      {},
	"transfer-encoding":   {},
	"connection":          {},
}

func copyFullPassthroughRequestHeaders(dst http.Header, c *gin.Context) {
	if dst == nil || c == nil || c.Request == nil {
		return
	}
	for key, values := range c.Request.Header {
		lower := strings.ToLower(strings.TrimSpace(key))
		if _, skipped := fullPassthroughSkippedRequestHeaders[lower]; skipped {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	if dst.Get("Content-Type") == "" {
		dst.Set("Content-Type", "application/json")
	}
}

func applyFullPassthroughAuthHeaders(req *http.Request, account *Account, token, tokenType string) {
	if req == nil || account == nil {
		return
	}
	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	token = strings.TrimSpace(token)
	tokenType = strings.TrimSpace(tokenType)

	switch {
	case account.IsOpenAI() || account.IsCustomOpenAIProtocol():
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if account.IsOpenAI() && account.Type == AccountTypeOAuth {
			req.Host = "chatgpt.com"
			if chatgptAccountID := account.GetChatGPTAccountID(); chatgptAccountID != "" {
				req.Header.Set("chatgpt-account-id", chatgptAccountID)
			}
			if req.Header.Get("OpenAI-Beta") == "" {
				req.Header.Set("OpenAI-Beta", "responses=experimental")
			}
		}
	case account.IsGemini() || (account.IsCustom() && account.CustomProtocol() == CustomProtocolGemini):
		if token == "" {
			return
		}
		if tokenType == "oauth" || account.UsesBearerAuth() {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("x-goog-api-key", token)
		}
	default:
		if token == "" {
			return
		}
		if tokenType == "oauth" || tokenType == "service_account" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("x-api-key", token)
		}
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	}
}

func (s *GatewayService) writeFullPassthroughResponse(c *gin.Context, resp *http.Response, stream bool) (ClaudeUsage, error) {
	if resp == nil || resp.Body == nil {
		return ClaudeUsage{}, errors.New("empty upstream response")
	}
	if c == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return ClaudeUsage{}, err
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	status := resp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	if !stream {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return ClaudeUsage{}, err
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(status, contentType, body)
		return usageFromFullPassthroughPayload(body), nil
	}

	c.Status(status)
	capture := &limitedCaptureBuffer{limit: fullPassthroughUsageCaptureLimit}
	if _, err := io.Copy(c.Writer, io.TeeReader(resp.Body, capture)); err != nil {
		return ClaudeUsage{}, err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return usageFromFullPassthroughPayload(capture.Bytes()), nil
}

type limitedCaptureBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedCaptureBuffer) Write(p []byte) (int, error) {
	if b == nil || b.limit <= 0 {
		return len(p), nil
	}
	remain := b.limit - b.buf.Len()
	if remain > 0 {
		if len(p) <= remain {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remain])
		}
	}
	return len(p), nil
}

func (b *limitedCaptureBuffer) Bytes() []byte {
	if b == nil {
		return nil
	}
	return b.buf.Bytes()
}

func usageFromFullPassthroughPayload(body []byte) ClaudeUsage {
	if len(body) == 0 {
		return ClaudeUsage{}
	}
	if direct := usageFromFullPassthroughJSON(body); !claudeUsageIsZero(direct) {
		return direct
	}
	var out ClaudeUsage
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if usage := usageFromFullPassthroughJSON([]byte(payload)); !claudeUsageIsZero(usage) {
			out = usage
		}
	}
	return out
}

func claudeUsageIsZero(usage ClaudeUsage) bool {
	return usage.InputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.CacheCreationInputTokens == 0 &&
		usage.CacheReadInputTokens == 0 &&
		usage.CacheCreation5mTokens == 0 &&
		usage.CacheCreation1hTokens == 0 &&
		usage.ImageOutputTokens == 0
}

func usageFromFullPassthroughJSON(body []byte) ClaudeUsage {
	if !gjson.ValidBytes(body) {
		return ClaudeUsage{}
	}
	var usage ClaudeUsage
	getInt := func(path string) int {
		if v := gjson.GetBytes(body, path); v.Exists() {
			return int(v.Int())
		}
		return 0
	}
	usage.InputTokens = firstPositiveInt(
		getInt("usage.input_tokens"),
		getInt("usage.prompt_tokens"),
		getInt("usageMetadata.promptTokenCount"),
	)
	usage.OutputTokens = firstPositiveInt(
		getInt("usage.output_tokens"),
		getInt("usage.completion_tokens"),
		getInt("usageMetadata.candidatesTokenCount"),
	)
	usage.CacheCreationInputTokens = firstPositiveInt(getInt("usage.cache_creation_input_tokens"))
	usage.CacheReadInputTokens = firstPositiveInt(getInt("usage.cache_read_input_tokens"))
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		total := getInt("usageMetadata.totalTokenCount")
		if total > 0 {
			usage.InputTokens = total
		}
	}
	return usage
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func fullPassthroughGeminiStreamRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	return strings.Contains(c.Request.URL.Path, ":streamGenerateContent") ||
		strings.EqualFold(c.Request.URL.Query().Get("alt"), "sse")
}
