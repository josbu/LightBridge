package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const (
	providerAdapterService     = "lightbridge.modules.ProviderAdapter"
	jsonCodecName              = "json"
	moduleID                   = "openai"
	openAIAPIBaseURL           = "https://api.openai.com"
	openAIPlatformResponsesURL = "https://api.openai.com/v1/responses"
	chatGPTCodexResponsesURL   = "https://chatgpt.com/backend-api/codex/responses"
	openAIAuthAuthorizeURL     = "https://auth.openai.com/oauth/authorize"
	openAIAuthTokenURL         = "https://auth.openai.com/oauth/token"
	openAIClientID             = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAIDefaultScopes        = "openid profile email offline_access"
	openAIRefreshScopes        = "openid profile email"
)

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

type jsonCodec struct{}

func (jsonCodec) Name() string { return jsonCodecName }

func (jsonCodec) Marshal(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 || v == nil {
		return nil
	}
	return json.Unmarshal(data, v)
}

type emptyMessage struct{}

type providerService interface {
	mustEmbedUnimplementedProviderService()
}

type openAIProvider struct{}

func (openAIProvider) mustEmbedUnimplementedProviderService() {}

type ProviderMetadata struct {
	ID              string          `json:"id"`
	DisplayName     string          `json:"display_name"`
	Supports        map[string]bool `json:"supports"`
	CredentialTypes []string        `json:"credential_types,omitempty"`
	Extra           map[string]any  `json:"extra,omitempty"`
}

type ModelInfo struct {
	ID           string          `json:"id"`
	DisplayName  string          `json:"display_name,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
}

type ListModelsRequest struct {
	CredentialRef string         `json:"credential_ref,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
}

type ListModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

type ProviderAccount struct {
	ID            string         `json:"id,omitempty"`
	ProviderID    string         `json:"provider_id,omitempty"`
	DisplayName   string         `json:"display_name,omitempty"`
	CredentialRef string         `json:"credential_ref,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	Secrets       map[string]any `json:"secrets,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type AccountValidationResult struct {
	Valid    bool           `json:"valid"`
	Warnings []string       `json:"warnings,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type TestAccountRequest struct {
	Account ProviderAccount `json:"account"`
	Mode    string          `json:"mode,omitempty"`
}

type TestAccountResult struct {
	OK       bool           `json:"ok"`
	Message  string         `json:"message,omitempty"`
	Latency  *DurationSpec  `json:"latency,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type DurationSpec struct {
	Seconds int64 `json:"seconds"`
	Nanos   int32 `json:"nanos"`
}

type UpstreamError struct {
	StatusCode int            `json:"status_code,omitempty"`
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Headers    map[string]any `json:"headers,omitempty"`
	Body       any            `json:"body,omitempty"`
}

type NormalizedError struct {
	Retryable   bool           `json:"retryable"`
	StatusCode  int            `json:"status_code,omitempty"`
	Code        string         `json:"code,omitempty"`
	Message     string         `json:"message,omitempty"`
	ProviderRaw map[string]any `json:"provider_raw,omitempty"`
}

type GatewayRequest struct {
	DownstreamProtocol string              `json:"downstream_protocol,omitempty"`
	Endpoint           string              `json:"endpoint"`
	Method             string              `json:"method,omitempty"`
	Headers            map[string][]string `json:"headers,omitempty"`
	Body               json.RawMessage     `json:"body,omitempty"`
	Stream             bool                `json:"stream"`
	Account            ProviderAccount     `json:"account,omitempty"`
	Metadata           map[string]any      `json:"metadata,omitempty"`
}

type GatewayEvent struct {
	Type       string              `json:"type"`
	StatusCode int                 `json:"status_code,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Data       json.RawMessage     `json:"data,omitempty"`
	Usage      *TokenUsage         `json:"usage,omitempty"`
	Error      *NormalizedError    `json:"error,omitempty"`
	Metadata   map[string]any      `json:"metadata,omitempty"`
}

type TokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens"`
}

type ChatRequest struct {
	Model         string          `json:"model"`
	Messages      []ChatMessage   `json:"messages,omitempty"`
	Stream        bool            `json:"stream"`
	CredentialRef string          `json:"credential_ref,omitempty"`
	Config        map[string]any  `json:"config,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatEvent struct {
	Type     string           `json:"type"`
	Data     json.RawMessage  `json:"data,omitempty"`
	Usage    *TokenUsage      `json:"usage,omitempty"`
	Error    *NormalizedError `json:"error,omitempty"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

type EmbeddingRequest struct {
	Model         string          `json:"model"`
	Input         any             `json:"input,omitempty"`
	CredentialRef string          `json:"credential_ref,omitempty"`
	Config        map[string]any  `json:"config,omitempty"`
	Account       ProviderAccount `json:"account,omitempty"`
}

type EmbeddingResponse struct {
	Model      string      `json:"model,omitempty"`
	Embeddings []Embedding `json:"embeddings"`
	Usage      *TokenUsage `json:"usage,omitempty"`
}

type Embedding struct {
	Index  int       `json:"index"`
	Vector []float64 `json:"vector"`
}

type TokenCountRequest struct {
	Model    string          `json:"model"`
	Messages []ChatMessage   `json:"messages,omitempty"`
	Input    any             `json:"input,omitempty"`
	Config   map[string]any  `json:"config,omitempty"`
	Account  ProviderAccount `json:"account,omitempty"`
}

type TokenCountResponse struct {
	Usage TokenUsage `json:"usage"`
}

func main() {
	socketPath := strings.TrimSpace(os.Getenv("LIGHTBRIDGE_MODULE_SOCKET"))
	if socketPath == "" {
		fatal("LIGHTBRIDGE_MODULE_SOCKET is required")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		fatal("create socket dir: %v", err)
	}
	if err := removeStaleSocket(socketPath); err != nil {
		fatal("remove stale socket: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fatal("listen provider socket: %v", err)
	}
	defer func() { _ = os.Remove(socketPath) }()

	server := grpc.NewServer(grpc.ForceServerCodec(jsonCodec{}))
	registerProviderService(server)
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		fatal("serve provider socket: %v", err)
	}
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket", socketPath)
	}
	return os.Remove(socketPath)
}

func registerProviderService(server *grpc.Server) {
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: providerAdapterService,
		HandlerType: (*providerService)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Metadata", Handler: metadataHandler},
			{MethodName: "HealthCheck", Handler: healthCheckHandler},
			{MethodName: "ListModels", Handler: listModelsHandler},
			{MethodName: "ValidateAccount", Handler: validateAccountHandler},
			{MethodName: "RefreshAccount", Handler: refreshAccountHandler},
			{MethodName: "TestAccount", Handler: testAccountHandler},
			{MethodName: "NormalizeError", Handler: normalizeErrorHandler},
			{MethodName: "Embed", Handler: embedHandler},
			{MethodName: "CountTokens", Handler: countTokensHandler},
		},
		Streams: []grpc.StreamDesc{{
			StreamName:    "Forward",
			Handler:       forwardHandler,
			ServerStreams: true,
			ClientStreams: true,
		}, {
			StreamName:    "ChatStream",
			Handler:       chatStreamHandler,
			ServerStreams: true,
			ClientStreams: true,
		}},
	}, openAIProvider{})
}

func metadataHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(emptyMessage)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(context.Context, any) (any, error) {
		return ProviderMetadata{
			ID:              moduleID,
			DisplayName:     "OpenAI Provider",
			CredentialTypes: []string{"api_key", "oauth"},
			Supports:        map[string]bool{"chat": true, "stream": true, "responses": true, "tools": true, "vision": true, "embeddings": true, "images": true},
			Extra: map[string]any{
				"downstream_protocols": []string{"chat_completions", "openai-compatible"},
				"oauth_authorize_url":  openAIAuthAuthorizeURL,
				"oauth_client_id":      openAIClientID,
				"oauth_scopes":         openAIDefaultScopes,
				"oauth_redirect_uri":   "http://localhost:1455/auth/callback",
				"endpoints": []string{
					"/v1/chat/completions",
					"/v1/responses",
				},
			},
		}, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("Metadata")}, handler)
}

func healthCheckHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(emptyMessage)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(context.Context, any) (any, error) { return emptyMessage{}, nil }
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("HealthCheck")}, handler)
}

func listModelsHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ListModelsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(context.Context, any) (any, error) {
		return ListModelsResponse{Models: []ModelInfo{
			{ID: "gpt-5.1", DisplayName: "GPT-5.1", Capabilities: map[string]bool{"chat": true, "stream": true, "tools": true, "vision": true}},
			{ID: "gpt-5.1-codex", DisplayName: "GPT-5.1 Codex", Capabilities: map[string]bool{"chat": true, "stream": true, "tools": true}},
			{ID: "gpt-4.1", DisplayName: "GPT-4.1", Capabilities: map[string]bool{"chat": true, "stream": true, "tools": true, "vision": true}},
			{ID: "text-embedding-3-large", DisplayName: "text-embedding-3-large", Capabilities: map[string]bool{"embeddings": true}},
		}}, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("ListModels")}, handler)
}

func validateAccountHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ProviderAccount)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		account := req.(*ProviderAccount)
		if token := accessToken(account); token != "" {
			return AccountValidationResult{Valid: true, Metadata: map[string]any{"provider_id": moduleID, "auth_type": "oauth"}}, nil
		}
		if key := apiKey(account); key != "" {
			return AccountValidationResult{Valid: true, Metadata: map[string]any{"provider_id": moduleID, "auth_type": "api_key"}}, nil
		}
		if refresh := secretString(account.Secrets, "refresh_token"); refresh != "" {
			refreshed, err := refreshOpenAIToken(ctx, refresh, proxyURL(account), clientID(account))
			if err != nil {
				return AccountValidationResult{Valid: false, Warnings: []string{sanitizeMessage(err.Error())}}, nil
			}
			return AccountValidationResult{Valid: refreshed.AccessToken != "", Metadata: map[string]any{"provider_id": moduleID, "auth_type": "oauth_refresh"}}, nil
		}
		return AccountValidationResult{Valid: false, Warnings: []string{"api_key or access_token is required"}}, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("ValidateAccount")}, handler)
}

func refreshAccountHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(ProviderAccount)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		account := *req.(*ProviderAccount)
		if code := secretString(account.Secrets, "authorization_code"); code != "" {
			token, err := exchangeOpenAICode(ctx, code, secretString(account.Secrets, "code_verifier"), redirectURI(&account), proxyURL(&account), clientID(&account))
			if err != nil {
				return nil, err
			}
			applyTokenResponse(&account, token)
			delete(account.Secrets, "authorization_code")
			delete(account.Secrets, "code_verifier")
			return account, nil
		}
		refresh := secretString(account.Secrets, "refresh_token")
		if refresh == "" {
			return account, nil
		}
		token, err := refreshOpenAIToken(ctx, refresh, proxyURL(&account), clientID(&account))
		if err != nil {
			return nil, err
		}
		if account.Secrets == nil {
			account.Secrets = map[string]any{}
		}
		account.Secrets["access_token"] = token.AccessToken
		if token.RefreshToken != "" {
			account.Secrets["refresh_token"] = token.RefreshToken
		}
		if token.IDToken != "" {
			account.Secrets["id_token"] = token.IDToken
		}
		if account.Metadata == nil {
			account.Metadata = map[string]any{}
		}
		applyTokenResponse(&account, token)
		return account, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("RefreshAccount")}, handler)
}

func testAccountHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(TestAccountRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		start := time.Now()
		request := req.(*TestAccountRequest)
		result, err := validateAccountForTest(ctx, &request.Account)
		if err != nil {
			return TestAccountResult{OK: false, Message: sanitizeMessage(err.Error())}, nil
		}
		result.Latency = durationSpec(time.Since(start))
		return result, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("TestAccount")}, handler)
}

func normalizeErrorHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(UpstreamError)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(_ context.Context, req any) (any, error) {
		upstream := req.(*UpstreamError)
		code := strings.TrimSpace(upstream.Code)
		if code == "" {
			code = "openai_error"
		}
		return NormalizedError{
			Retryable:  upstream.StatusCode == 408 || upstream.StatusCode == 409 || upstream.StatusCode == 429 || upstream.StatusCode >= 500,
			StatusCode: upstream.StatusCode,
			Code:       code,
			Message:    sanitizeMessage(upstream.Message),
			ProviderRaw: map[string]any{
				"provider": moduleID,
			},
		}, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("NormalizeError")}, handler)
}

func forwardHandler(_ any, stream grpc.ServerStream) error {
	var req GatewayRequest
	if err := stream.RecvMsg(&req); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	ctx := stream.Context()
	if err := forwardOpenAI(ctx, stream, req); err != nil {
		return stream.SendMsg(&GatewayEvent{
			Type: "error",
			Error: &NormalizedError{
				Retryable:  false,
				StatusCode: http.StatusBadGateway,
				Code:       "openai_forward_failed",
				Message:    sanitizeMessage(err.Error()),
			},
		})
	}
	return nil
}

func forwardOpenAI(ctx context.Context, stream grpc.ServerStream, req GatewayRequest) error {
	token := accessToken(&req.Account)
	if token == "" {
		return fmt.Errorf("missing OpenAI credential")
	}
	upstreamURL, body, oauthAccount, err := prepareOpenAIUpstreamRequest(req)
	if err != nil {
		return err
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = http.MethodPost
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream, application/json")
	if oauthAccount {
		httpReq.Host = "chatgpt.com"
		httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
		httpReq.Header.Set("originator", originator(&req.Account))
		if chatGPTAccountID := chatGPTAccountID(&req.Account); chatGPTAccountID != "" {
			httpReq.Header.Set("chatgpt-account-id", chatGPTAccountID)
		}
	}
	for key, values := range req.Headers {
		if !forwardableHeader(key) {
			continue
		}
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	client := httpClient(proxyURL(&req.Account))
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	headers := sanitizeHeaders(resp.Header)
	if err := stream.SendMsg(&GatewayEvent{Type: "headers", StatusCode: resp.StatusCode, Headers: headers}); err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return stream.SendMsg(&GatewayEvent{
			Type: "error",
			Error: &NormalizedError{
				Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
				StatusCode: resp.StatusCode,
				Code:       "openai_upstream_error",
				Message:    sanitizeMessage(extractOpenAIError(body)),
			},
		})
	}
	if req.Stream || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return forwardSSE(stream, resp.Body)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	usage := usageFromJSON(body)
	if err := stream.SendMsg(&GatewayEvent{Type: "data", Data: json.RawMessage(body), Usage: usage}); err != nil {
		return err
	}
	if usage != nil {
		if err := stream.SendMsg(&GatewayEvent{Type: "usage", Usage: usage}); err != nil {
			return err
		}
	}
	return stream.SendMsg(&GatewayEvent{Type: "done"})
}

func forwardSSE(stream grpc.ServerStream, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload == "[DONE]" {
				return stream.SendMsg(&GatewayEvent{Type: "done"})
			}
			raw := json.RawMessage(payload)
			event := GatewayEvent{Type: "data", Data: raw, Usage: usageFromJSON(raw)}
			if err := stream.SendMsg(&event); err != nil {
				return err
			}
			if event.Usage != nil {
				if err := stream.SendMsg(&GatewayEvent{Type: "usage", Usage: event.Usage}); err != nil {
					return err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return stream.SendMsg(&GatewayEvent{Type: "done"})
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func exchangeOpenAICode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*tokenResponse, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openAIClientID
	}
	if strings.TrimSpace(redirectURI) == "" {
		redirectURI = "http://localhost:1455/auth/callback"
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "codex-cli/0.91.0")
	return doOpenAITokenRequest(req, proxyURL)
}

func refreshOpenAIToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*tokenResponse, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openAIClientID
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	form.Set("scope", openAIRefreshScopes)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "codex-cli/0.91.0")
	return doOpenAITokenRequest(req, proxyURL)
}

func doOpenAITokenRequest(req *http.Request, proxyURL string) (*tokenResponse, error) {
	resp, err := httpClient(proxyURL).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token refresh failed: status %d: %s", resp.StatusCode, sanitizeMessage(string(body)))
	}
	var out tokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func applyTokenResponse(account *ProviderAccount, token *tokenResponse) {
	if account.Secrets == nil {
		account.Secrets = map[string]any{}
	}
	account.Secrets["access_token"] = token.AccessToken
	if token.RefreshToken != "" {
		account.Secrets["refresh_token"] = token.RefreshToken
	}
	if token.IDToken != "" {
		account.Secrets["id_token"] = token.IDToken
	}
	if account.Metadata == nil {
		account.Metadata = map[string]any{}
	}
	account.Metadata["refreshed_at"] = time.Now().UTC().Format(time.RFC3339)
	account.Metadata["expires_in"] = token.ExpiresIn
}

func prepareOpenAIUpstreamRequest(req GatewayRequest) (string, []byte, bool, error) {
	endpoint := normalizeEndpoint(req.Endpoint)
	body := append([]byte(nil), req.Body...)
	oauthAccount := isOAuthAccount(&req.Account)
	if oauthAccount {
		if endpoint == "/v1/chat/completions" {
			converted, err := chatCompletionsToResponsesBody(body)
			if err != nil {
				return "", nil, false, err
			}
			body = applyCodexOAuthTransformLite(converted)
		} else {
			body = applyCodexOAuthTransformLite(body)
		}
		return chatGPTCodexResponsesURL, body, true, nil
	}
	if endpoint == "/v1/responses" {
		return responseURLForAPIKey(&req.Account), body, false, nil
	}
	return strings.TrimRight(baseURL(&req.Account), "/") + endpoint, body, false, nil
}

func responseURLForAPIKey(account *ProviderAccount) string {
	base := strings.TrimRight(baseURL(account), "/")
	if base == openAIAPIBaseURL {
		return openAIPlatformResponsesURL
	}
	return base + "/v1/responses"
}

func isOAuthAccount(account *ProviderAccount) bool {
	if account == nil {
		return false
	}
	accountType := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		stringFromMap(account.Config, "type"),
		stringFromMap(account.Metadata, "type"),
	)))
	if accountType == "oauth" {
		return true
	}
	return apiKey(account) == "" && strings.TrimSpace(secretString(account.Secrets, "access_token")) != ""
}

func chatCompletionsToResponsesBody(body []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	input, err := chatMessagesToResponsesInput(root["messages"])
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"model":   firstNonEmpty(stringFromMap(root, "model"), "gpt-5.1"),
		"input":   input,
		"stream":  true,
		"store":   false,
		"include": []string{"reasoning.encrypted_content"},
	}
	copyOptional(out, root, "instructions", "service_tier", "tools", "tool_choice")
	if maxTokens := firstNumber(root["max_completion_tokens"], root["max_tokens"]); maxTokens > 0 {
		if maxTokens < 16 {
			maxTokens = 16
		}
		out["max_output_tokens"] = maxTokens
	}
	if effort := stringFromMap(root, "reasoning_effort"); effort != "" {
		out["reasoning"] = map[string]any{"effort": effort, "summary": "auto"}
	}
	if !isReasoningModel(stringFromMap(out, "model")) {
		copyOptional(out, root, "temperature", "top_p")
	}
	return json.Marshal(out)
}

func chatMessagesToResponsesInput(raw any) ([]map[string]any, error) {
	messages, ok := raw.([]any)
	if !ok {
		return []map[string]any{}, nil
	}
	out := make([]map[string]any, 0, len(messages))
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(stringFromMap(message, "role"))
		switch role {
		case "assistant":
			out = append(out, assistantMessageToResponsesItems(message)...)
		case "tool", "function":
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": firstNonEmpty(stringFromMap(message, "tool_call_id"), stringFromMap(message, "name")),
				"output":  contentText(message["content"]),
			})
		default:
			if role == "" {
				role = "user"
			}
			out = append(out, map[string]any{
				"role":    role,
				"content": inputContentParts(message["content"], role == "assistant"),
			})
		}
	}
	return out, nil
}

func assistantMessageToResponsesItems(message map[string]any) []map[string]any {
	var out []map[string]any
	text := contentText(message["content"])
	if text != "" {
		out = append(out, map[string]any{
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": text,
			}},
		})
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		for _, rawCall := range calls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := call["function"].(map[string]any)
			out = append(out, map[string]any{
				"type":      "function_call",
				"call_id":   stringFromMap(call, "id"),
				"name":      stringFromMap(fn, "name"),
				"arguments": firstNonEmpty(stringFromMap(fn, "arguments"), "{}"),
			})
		}
	}
	return out
}

func inputContentParts(raw any, assistant bool) []map[string]any {
	if parts, ok := raw.([]any); ok {
		out := make([]map[string]any, 0, len(parts))
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			switch strings.TrimSpace(stringFromMap(part, "type")) {
			case "image_url":
				imageURL, _ := part["image_url"].(map[string]any)
				if url := stringFromMap(imageURL, "url"); url != "" {
					out = append(out, map[string]any{"type": "input_image", "image_url": url})
				}
			default:
				if text := firstNonEmpty(stringFromMap(part, "text"), contentText(part["content"])); text != "" {
					partType := "input_text"
					if assistant {
						partType = "output_text"
					}
					out = append(out, map[string]any{"type": partType, "text": text})
				}
			}
		}
		return out
	}
	text := contentText(raw)
	if text == "" {
		text = ""
	}
	partType := "input_text"
	if assistant {
		partType = "output_text"
	}
	return []map[string]any{{"type": partType, "text": text}}
}

func applyCodexOAuthTransformLite(body []byte) []byte {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}
	root["stream"] = true
	root["store"] = false
	if _, ok := root["include"]; !ok {
		root["include"] = []string{"reasoning.encrypted_content"}
	}
	if model := normalizeOpenAIModelForOAuth(stringFromMap(root, "model")); model != "" {
		root["model"] = model
	}
	out, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return out
}

func normalizeOpenAIModelForOAuth(model string) string {
	model = strings.TrimSpace(model)
	switch model {
	case "", "auto":
		return "gpt-5.1"
	case "codex", "codex-mini-latest":
		return "gpt-5.1-codex"
	default:
		return model
	}
}

func contentText(raw any) string {
	switch typed := raw.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func copyOptional(dst map[string]any, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok && value != nil {
			dst[key] = value
		}
	}
}

func firstNumber(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case int64:
			return int(typed)
		}
	}
	return 0
}

func isReasoningModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-5") || strings.Contains(model, "o1") || strings.Contains(model, "o3") || strings.Contains(model, "o4")
}

func validateAccountForTest(ctx context.Context, account *ProviderAccount) (TestAccountResult, error) {
	if accessToken(account) != "" || apiKey(account) != "" {
		return TestAccountResult{OK: true, Message: "OpenAI account is usable", Metadata: map[string]any{"provider_id": moduleID}}, nil
	}
	refresh := secretString(account.Secrets, "refresh_token")
	if refresh == "" {
		return TestAccountResult{OK: false, Message: "api_key, access_token, or refresh_token is required"}, nil
	}
	token, err := refreshOpenAIToken(ctx, refresh, proxyURL(account), clientID(account))
	if err != nil {
		return TestAccountResult{OK: false, Message: sanitizeMessage(err.Error())}, nil
	}
	return TestAccountResult{OK: token.AccessToken != "", Message: "OpenAI OAuth token refresh succeeded", Metadata: map[string]any{"provider_id": moduleID}}, nil
}

func apiKey(account *ProviderAccount) string {
	return strings.TrimSpace(secretString(account.Secrets, "api_key"))
}

func accessToken(account *ProviderAccount) string {
	if key := apiKey(account); key != "" {
		return key
	}
	return strings.TrimSpace(secretString(account.Secrets, "access_token"))
}

func clientID(account *ProviderAccount) string {
	if account == nil {
		return ""
	}
	if v := stringFromMap(account.Config, "client_id"); v != "" {
		return v
	}
	return stringFromMap(account.Metadata, "client_id")
}

func redirectURI(account *ProviderAccount) string {
	if account == nil {
		return ""
	}
	if v := stringFromMap(account.Config, "redirect_uri"); v != "" {
		return v
	}
	return stringFromMap(account.Metadata, "redirect_uri")
}

func proxyURL(account *ProviderAccount) string {
	if account == nil {
		return ""
	}
	if v := stringFromMap(account.Config, "proxy_url"); v != "" {
		return v
	}
	return stringFromMap(account.Metadata, "proxy_url")
}

func baseURL(account *ProviderAccount) string {
	if account == nil {
		return openAIAPIBaseURL
	}
	if v := strings.TrimRight(stringFromMap(account.Config, "base_url"), "/"); v != "" {
		return v
	}
	return openAIAPIBaseURL
}

func originator(account *ProviderAccount) string {
	if account == nil {
		return "codex_cli_rs"
	}
	return firstNonEmpty(
		stringFromMap(account.Config, "originator"),
		stringFromMap(account.Metadata, "originator"),
		"codex_cli_rs",
	)
}

func chatGPTAccountID(account *ProviderAccount) string {
	if account == nil {
		return ""
	}
	return firstNonEmpty(
		stringFromMap(account.Config, "chatgpt_account_id"),
		stringFromMap(account.Metadata, "chatgpt_account_id"),
		secretString(account.Secrets, "chatgpt_account_id"),
	)
}

func httpClient(proxyRaw string) *http.Client {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	proxyRaw = strings.TrimSpace(proxyRaw)
	if proxyRaw != "" {
		if parsed, err := url.Parse(proxyRaw); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{Timeout: 120 * time.Second, Transport: transport}
}

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || endpoint == "/" {
		return "/v1/chat/completions"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return endpoint
}

func forwardableHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "authorization", "cookie", "set-cookie", "content-length", "connection", "host":
		return false
	default:
		return true
	}
}

func sanitizeHeaders(headers http.Header) map[string][]string {
	out := map[string][]string{}
	for key, values := range headers {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "authorization", "cookie", "set-cookie", "www-authenticate":
			continue
		default:
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

func extractOpenAIError(body []byte) string {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err == nil {
		if errObj, ok := root["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return msg
			}
		}
	}
	return string(body)
}

func usageFromJSON(body []byte) *TokenUsage {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	usage, ok := root["usage"].(map[string]any)
	if !ok {
		return nil
	}
	input := numberToInt64(usage["prompt_tokens"])
	if input == 0 {
		input = numberToInt64(usage["input_tokens"])
	}
	output := numberToInt64(usage["completion_tokens"])
	if output == 0 {
		output = numberToInt64(usage["output_tokens"])
	}
	total := numberToInt64(usage["total_tokens"])
	if total == 0 {
		total = input + output
	}
	return &TokenUsage{InputTokens: input, OutputTokens: output, TotalTokens: total}
}

func numberToInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}

func durationSpec(d time.Duration) *DurationSpec {
	return &DurationSpec{Seconds: int64(d / time.Second), Nanos: int32(d % time.Second)}
}

func secretString(secrets map[string]any, key string) string {
	return stringFromMap(secrets, key)
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sanitizeMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

func methodName(method string) string {
	return "/" + providerAdapterService + "/" + method
}

func embedHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(EmbeddingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, req any) (any, error) {
		request := req.(*EmbeddingRequest)
		account := ProviderAccount{Secrets: map[string]any{}, Config: request.Config}
		if request.CredentialRef != "" {
			account.Secrets["api_key"] = request.CredentialRef
		}
		body, err := json.Marshal(map[string]any{"model": request.Model, "input": request.Input})
		if err != nil {
			return nil, err
		}
		gatewayReq := GatewayRequest{Endpoint: "/v1/embeddings", Method: http.MethodPost, Body: body, Account: account}
		collector := &collectingServerStream{ctx: ctx}
		if err := forwardOpenAI(ctx, collector, gatewayReq); err != nil {
			return nil, err
		}
		var out EmbeddingResponse
		if len(collector.lastData) > 0 {
			if err := json.Unmarshal(collector.lastData, &out); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("Embed")}, handler)
}

func countTokensHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(TokenCountRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(context.Context, any) (any, error) {
		tokens := estimateTokenCount(in)
		return TokenCountResponse{Usage: TokenUsage{InputTokens: tokens, TotalTokens: tokens}}, nil
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: methodName("CountTokens")}, handler)
}

func chatStreamHandler(_ any, stream grpc.ServerStream) error {
	var req ChatRequest
	if err := stream.RecvMsg(&req); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	body, err := json.Marshal(map[string]any{"model": req.Model, "messages": req.Messages, "stream": req.Stream})
	if err != nil {
		return err
	}
	account := ProviderAccount{Secrets: map[string]any{}, Config: req.Config}
	if req.CredentialRef != "" {
		account.Secrets["api_key"] = req.CredentialRef
	}
	gatewayReq := GatewayRequest{Endpoint: "/v1/chat/completions", Method: http.MethodPost, Body: body, Stream: req.Stream, Account: account}
	return forwardOpenAI(stream.Context(), stream, gatewayReq)
}

type collectingServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	lastData json.RawMessage
}

func (s *collectingServerStream) Context() context.Context { return s.ctx }

func (s *collectingServerStream) SendMsg(m any) error {
	if event, ok := m.(*GatewayEvent); ok && event.Type == "data" {
		s.lastData = append(json.RawMessage(nil), event.Data...)
	}
	return nil
}

func estimateTokenCount(req *TokenCountRequest) int64 {
	payload, _ := json.Marshal(req)
	count := int64(len(payload) / 4)
	if count < 1 {
		count = 1
	}
	return count
}
