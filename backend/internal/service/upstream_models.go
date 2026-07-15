package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/antigravity"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/claude"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/geminicli"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/WilliamWang1721/LightBridge/internal/util/logredact"
)

const upstreamModelsBodyLimit int64 = 8 << 20

// UpstreamModelSyncErrorKind classifies model sync failures for safe HTTP mapping.
type UpstreamModelSyncErrorKind string

const (
	// UpstreamModelSyncErrorConfiguration means the account or server configuration cannot perform the sync.
	UpstreamModelSyncErrorConfiguration UpstreamModelSyncErrorKind = "configuration"
	// UpstreamModelSyncErrorUnsupported means the account format is intentionally unsupported for live model sync.
	UpstreamModelSyncErrorUnsupported UpstreamModelSyncErrorKind = "unsupported"
	// UpstreamModelSyncErrorUpstream means the configured upstream failed or returned an unusable response.
	UpstreamModelSyncErrorUpstream UpstreamModelSyncErrorKind = "upstream"
)

// UpstreamModelSyncError keeps internal failure details wrapped while exposing a safe client message.
type UpstreamModelSyncError struct {
	Kind           UpstreamModelSyncErrorKind
	Message        string
	Detail         string
	UpstreamURL    string
	UpstreamStatus int
	Err            error
}

func (e *UpstreamModelSyncError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *UpstreamModelSyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SafeMessage returns the sanitized message that can be sent to API clients.
func (e *UpstreamModelSyncError) SafeMessage() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "Failed to sync upstream models"
	}
	message := strings.TrimSpace(e.Message)
	detail := strings.TrimSpace(e.Detail)
	if detail == "" && e.Err != nil {
		detail = logredact.RedactText(e.Err.Error(), "api_key", "x-api-key", "authorization", "token", "key")
	}
	if detail == "" || strings.Contains(message, detail) {
		return message
	}
	return message + ": " + detail
}

func (e *UpstreamModelSyncError) withUpstreamResponse(rawURL string, status int, detail string) error {
	if e == nil {
		return e
	}
	e.UpstreamURL = sanitizeUpstreamModelURL(rawURL)
	e.UpstreamStatus = status
	e.Detail = detail
	return e
}

func newUpstreamModelSyncConfigError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorConfiguration, Message: message, Err: err}
}

func newUpstreamModelSyncUnsupportedError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUnsupported, Message: message, Err: err}
}

func newUpstreamModelSyncUpstreamError(message string, err error) error {
	return &UpstreamModelSyncError{Kind: UpstreamModelSyncErrorUpstream, Message: message, Err: err}
}

// FetchUpstreamSupportedModels fetches the live model list from the account's upstream API format.
func (s *AccountTestService) FetchUpstreamSupportedModels(ctx context.Context, account *Account) ([]string, error) {
	if s == nil {
		return nil, newUpstreamModelSyncConfigError("Account test service is not configured", nil)
	}
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}

	if account.IsAntigravity() && account.Type != AccountTypeAPIKey {
		return s.fetchAntigravityOAuthUpstreamModels(ctx, account)
	}

	if s.httpUpstream == nil {
		return nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}

	req, err := s.buildUpstreamModelsRequest(ctx, account)
	if err != nil {
		return nil, err
	}

	proxyURL := upstreamModelsProxyURL(account)
	resp, err := s.doUpstreamModelsRequest(req, proxyURL, account)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to request upstream model list", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamModelsBodyLimit+1))
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to read upstream model list", err)
	}
	if int64(len(body)) > upstreamModelsBodyLimit {
		return nil, newUpstreamModelSyncUpstreamError("Upstream model list response is too large", fmt.Errorf("response exceeds %d bytes", upstreamModelsBodyLimit))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		syncErr := newUpstreamModelSyncUpstreamError(
			fmt.Sprintf("Upstream model list request failed with HTTP %d", resp.StatusCode),
			fmt.Errorf("upstream model list returned HTTP %d", resp.StatusCode),
		)
		if typed, ok := syncErr.(*UpstreamModelSyncError); ok {
			return nil, typed.withUpstreamResponse(req.URL.String(), resp.StatusCode, sanitizeUpstreamModelErrorDetail(body))
		}
		return nil, syncErr
	}

	models, err := extractUpstreamModelIDs(body)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Upstream model list response was not valid JSON", err)
	}
	if len(models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}

	return models, nil
}

func sanitizeUpstreamModelErrorDetail(body []byte) string {
	const maxDetailBytes = 16 << 10
	if len(body) > maxDetailBytes {
		body = body[:maxDetailBytes]
	}
	return logredact.RedactText(string(body), "api_key", "x-api-key", "authorization", "token", "key")
}

func sanitizeUpstreamModelURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return logredact.RedactText(raw, "api_key", "key", "token")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (s *AccountTestService) buildUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	switch {
	case account.IsAntigravity():
		return s.buildAntigravityAPIKeyModelsRequest(ctx, account)
	case account.IsGrok():
		return s.buildGrokUpstreamModelsRequest(ctx, account)
	case account.IsOpenAI():
		return s.buildOpenAIUpstreamModelsRequest(ctx, account)
	case account.IsGemini():
		return s.buildGeminiUpstreamModelsRequest(ctx, account)
	case account.IsAnthropic():
		return s.buildAnthropicUpstreamModelsRequest(ctx, account)
	default:
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported platform for upstream model sync: %s", account.Platform), nil,
		)
	}
}

func (s *AccountTestService) buildAnthropicUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.IsBedrock() || account.Type == AccountTypeServiceAccount {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Anthropic account type for upstream model sync: %s", account.Type), nil,
		)
	}

	baseURL := "https://api.anthropic.com"
	authHeaderName := ""
	authHeaderValue := ""
	betaHeader := ""

	if account.IsOAuth() {
		accessToken := strings.TrimSpace(account.GetCredential("access_token"))
		if accessToken == "" && s.claudeTokenProvider != nil {
			token, tokenErr := s.claudeTokenProvider.GetAccessToken(ctx, account)
			if tokenErr != nil {
				return nil, newUpstreamModelSyncUpstreamError("Failed to get Anthropic access token", tokenErr)
			}
			accessToken = strings.TrimSpace(token)
		}
		if accessToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Anthropic access token is available", nil)
		}
		authHeaderName = "Authorization"
		authHeaderValue = "Bearer " + accessToken
		betaHeader = claude.DefaultBetaHeader
	} else if account.Type == AccountTypeAPIKey {
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, newUpstreamModelSyncConfigError("No Anthropic API key is available", nil)
		}
		baseURL = strings.TrimSpace(account.GetCredential("base_url"))
		if baseURL == "" {
			baseURL = account.GetBaseURL()
		}
		if strings.TrimSpace(baseURL) == "" {
			baseURL = "https://api.anthropic.com"
		}
		authHeaderName = "x-api-key"
		authHeaderValue = apiKey
		betaHeader = claude.APIKeyBetaHeader
	} else {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Anthropic account type for upstream model sync: %s", account.Type), nil,
		)
	}

	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Anthropic base URL", err)
	}
	modelsURL, err := s.resolveUpstreamModelsURL(account, buildV1ModelsURL(normalizedBaseURL))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Anthropic model list URL", err)
	}
	for key, value := range claude.DefaultHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set(authHeaderName, authHeaderValue)
	return req, nil
}

func (s *AccountTestService) buildAntigravityAPIKeyModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Antigravity account type for upstream model sync: %s", account.Type), nil,
		)
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No Antigravity API key is available", nil)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/")
	if baseURL == "" {
		return nil, newUpstreamModelSyncConfigError("Antigravity API-key base URL is required for upstream model sync", nil)
	}
	if !strings.HasSuffix(strings.ToLower(baseURL), "/antigravity") {
		return nil, newUpstreamModelSyncUnsupportedError(
			"Antigravity API-key upstream model sync requires a compatible gateway base URL ending in /antigravity; use Antigravity OAuth for official Cloud Code upstreams",
			nil,
		)
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Antigravity base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildV1ModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Antigravity model list URL", err)
	}
	for key, value := range claude.DefaultHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", claude.APIKeyBetaHeader)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}

func (s *AccountTestService) buildOpenAIUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported OpenAI account type for upstream model sync: %s", account.Type), nil,
		)
	}
	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No OpenAI API key is available", nil)
	}

	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = account.GetOpenAIBaseURL()
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com"
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI base URL", err)
	}

	modelsURL, err := s.resolveUpstreamModelsURL(account, buildOpenAIModelsURL(normalizedBaseURL))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return req, nil
}

func (s *AccountTestService) buildGrokUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account.Type != AccountTypeOAuth {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Grok account type for upstream model sync: %s", account.Type), nil,
		)
	}

	accessToken := strings.TrimSpace(account.GetGrokAccessToken())
	if accessToken == "" && s.grokTokenProvider != nil {
		token, tokenErr := s.grokTokenProvider.GetAccessToken(ctx, account)
		if tokenErr != nil {
			return nil, newUpstreamModelSyncUpstreamError("Failed to get Grok access token", tokenErr)
		}
		accessToken = strings.TrimSpace(token)
	}
	if accessToken == "" {
		return nil, newUpstreamModelSyncConfigError("No Grok access token is available", nil)
	}

	normalizedBaseURL, err := xai.ValidatedBaseURL(account.GetGrokBaseURL())
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Grok base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIModelsURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Grok model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "lightbridge-grok/1.0")
	return req, nil
}

func (s *AccountTestService) buildGeminiUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = geminicli.AIStudioBaseURL
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Gemini base URL", err)
	}

	modelsURL, err := s.resolveUpstreamModelsURL(account, buildGeminiModelsURL(normalizedBaseURL))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Gemini model list URL", err)
	}
	req.Header.Set("Accept", "application/json")

	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, newUpstreamModelSyncConfigError("No Gemini API key is available", nil)
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case AccountTypeOAuth:
		if strings.TrimSpace(account.GetCredential("project_id")) != "" {
			return nil, newUpstreamModelSyncUnsupportedError("Gemini Code Assist model listing is not supported by this sync button", nil)
		}
		if s.geminiTokenProvider == nil {
			return nil, newUpstreamModelSyncConfigError("Gemini token provider is not configured", nil)
		}
		accessToken, tokenErr := s.geminiTokenProvider.GetAccessToken(ctx, account)
		if tokenErr != nil {
			return nil, newUpstreamModelSyncUpstreamError("Failed to get Gemini access token", tokenErr)
		}
		accessToken = strings.TrimSpace(accessToken)
		if accessToken == "" {
			return nil, newUpstreamModelSyncConfigError("No Gemini access token is available", nil)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	default:
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported Gemini account type for upstream model sync: %s", account.Type), nil,
		)
	}

	return req, nil
}

func (s *AccountTestService) fetchAntigravityOAuthUpstreamModels(ctx context.Context, account *Account) ([]string, error) {
	if s.antigravityGatewayService == nil || s.antigravityGatewayService.GetTokenProvider() == nil {
		return nil, newUpstreamModelSyncConfigError("Antigravity token provider is not configured", nil)
	}

	accessToken, err := s.antigravityGatewayService.GetTokenProvider().GetAccessToken(ctx, account)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to get Antigravity access token", err)
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, newUpstreamModelSyncConfigError("No Antigravity access token is available", nil)
	}

	client, err := antigravity.NewClient(upstreamModelsProxyURL(account))
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Failed to configure Antigravity client", err)
	}
	modelsResp, _, err := client.FetchAvailableModels(ctx, accessToken, strings.TrimSpace(account.GetCredential("project_id")))
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to fetch Antigravity available models", err)
	}
	if modelsResp == nil || len(modelsResp.Models) == 0 {
		return nil, newUpstreamModelSyncUpstreamError("Upstream returned no supported models", nil)
	}

	models := make([]string, 0, len(modelsResp.Models))
	for modelID := range modelsResp.Models {
		models = append(models, strings.TrimSpace(modelID))
	}
	return dedupeAndSortModelIDs(models), nil
}

func (s *AccountTestService) doUpstreamModelsRequest(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if s.tlsFPProfileService == nil {
		return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, nil)
	}
	return s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
}

func upstreamModelsProxyURL(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

// resolveUpstreamModelsURL lets Custom providers override the conventional
// /v1/models or /v1beta/models endpoint while retaining the same URL security
// validation used by normal upstream requests.
func (s *AccountTestService) resolveUpstreamModelsURL(account *Account, fallback string) (string, error) {
	if account == nil || !account.IsCustom() {
		return fallback, nil
	}
	configured := strings.TrimSpace(account.GetCredential("models_url"))
	if configured == "" {
		return fallback, nil
	}
	normalized, err := s.validateUpstreamBaseURL(configured)
	if err != nil {
		return "", newUpstreamModelSyncConfigError("Invalid Custom model list URL", err)
	}
	return normalized, nil
}

func buildV1ModelsURL(base string) string {
	return buildModelsURLForProtocol(base, CustomProtocolAnthropicMessages)
}

func buildOpenAIModelsURL(base string) string {
	return buildModelsURLForProtocol(base, CustomProtocolOpenAIResponses)
}

func buildGeminiModelsURL(base string) string {
	return buildModelsURLForProtocol(base, CustomProtocolGemini)
}

// buildModelsURLForProtocol accepts a provider root URL, a versioned API root,
// or a full generation endpoint. It preserves any path prefix while replacing
// the protocol endpoint suffix with the matching model-list endpoint.
func buildModelsURLForProtocol(base, protocol string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return normalized
	}

	targetVersion := "v1"
	if protocol == CustomProtocolGemini {
		targetVersion = "v1beta"
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		segments = nil
	}
	versionIndex := -1
	for i, segment := range segments {
		switch strings.ToLower(strings.TrimSpace(segment)) {
		case "v1", "v1beta":
			versionIndex = i
		}
	}

	var modelPath []string
	if versionIndex >= 0 {
		modelPath = append(modelPath, segments[:versionIndex]...)
		modelPath = append(modelPath, targetVersion, "models")
	} else if len(segments) > 0 && strings.EqualFold(segments[len(segments)-1], "models") {
		modelPath = segments
	} else {
		modelPath = append(modelPath, segments...)
		modelPath = append(modelPath, targetVersion, "models")
	}

	parsed.Path = "/" + strings.Join(modelPath, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func extractUpstreamModelIDs(body []byte) ([]string, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse upstream model list: %w", err)
	}
	models := make([]string, 0)
	collectUpstreamModelIDs(payload, &models, 0)
	return dedupeAndSortModelIDs(models), nil
}

func collectUpstreamModelIDs(value any, models *[]string, depth int) {
	if depth > 8 || value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		*models = append(*models, typed)
	case []any:
		for _, item := range typed {
			collectUpstreamModelIDs(item, models, depth+1)
		}
	case map[string]any:
		for _, key := range []string{"id", "name", "model", "model_id"} {
			if raw, ok := typed[key].(string); ok && strings.TrimSpace(raw) != "" {
				*models = append(*models, raw)
				return
			}
		}
		for _, key := range []string{"data", "models", "items", "result"} {
			nested, ok := typed[key]
			if !ok {
				continue
			}
			if modelMap, ok := nested.(map[string]any); ok && key == "models" {
				for modelID := range modelMap {
					*models = append(*models, modelID)
				}
				continue
			}
			collectUpstreamModelIDs(nested, models, depth+1)
		}
	}
}

func dedupeAndSortModelIDs(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	sort.Strings(result)
	return result
}
