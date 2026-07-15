package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/tlsfingerprint"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func grokQuotaJWT(rawPayload string) string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(rawPayload)) + ".signature"
}

type grokQuotaAccountRepoForTest struct {
	AccountRepository
	accounts          map[int64]*Account
	updates           map[int64]map[string]any
	tempUntil         map[int64]time.Time
	tempUnschedReason map[int64]string
}

func (r *grokQuotaAccountRepoForTest) GetByID(_ context.Context, id int64) (*Account, error) {
	if account, ok := r.accounts[id]; ok {
		return account, nil
	}
	return nil, infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
}

func (r *grokQuotaAccountRepoForTest) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	r.updates[id] = updates
	return nil
}

func (r *grokQuotaAccountRepoForTest) SetError(_ context.Context, id int64, message string) error {
	if account := r.accounts[id]; account != nil {
		account.Status = StatusError
		account.Schedulable = false
		account.ErrorMessage = message
	}
	return nil
}

func (r *grokQuotaAccountRepoForTest) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	if r.tempUntil == nil {
		r.tempUntil = make(map[int64]time.Time)
		r.tempUnschedReason = make(map[int64]string)
	}
	r.tempUntil[id] = until
	r.tempUnschedReason[id] = reason
	if account := r.accounts[id]; account != nil {
		account.TempUnschedulableUntil = &until
		account.TempUnschedulableReason = reason
	}
	return nil
}

type grokQuotaProxyRepoForTest struct {
	ProxyRepository
	proxies map[int64]*Proxy
	calls   int
}

func (r *grokQuotaProxyRepoForTest) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.calls++
	return r.proxies[id], nil
}

type grokQuotaHTTPUpstreamForTest struct {
	resp         *http.Response
	lastReq      *http.Request
	lastBody     []byte
	lastProxyURL string
}

func (u *grokQuotaHTTPUpstreamForTest) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	u.lastReq = req
	u.lastProxyURL = proxyURL
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.lastBody = body
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	return u.resp, nil
}

func (u *grokQuotaHTTPUpstreamForTest) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGrokQuotaServiceProbeUsageStoresHeaders(t *testing.T) {
	account := &Account{
		ID:          42,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":          "access-token",
			"expires_at":            time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			GrokCredentialOAuthMode: string(xai.OAuthModeOfficialAPI),
		},
	}
	repo := &grokQuotaAccountRepoForTest{
		accounts: map[int64]*Account{42: account},
	}
	upstream := &grokQuotaHTTPUpstreamForTest{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"7"},
			"X-Ratelimit-Reset-Requests":     []string{"2000000000"},
			"X-Ratelimit-Limit-Tokens":       []string{"1000"},
			"X-Ratelimit-Remaining-Tokens":   []string{"900"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.True(t, result.HeadersObserved)
	require.NotNil(t, result.Snapshot)
	require.True(t, result.Snapshot.HeadersObserved)
	require.Equal(t, "active_probe", result.Snapshot.ObservationSource)
	require.NotEmpty(t, result.Snapshot.LastProbeAt)
	require.NotEmpty(t, result.Snapshot.LastHeadersSeenAt)
	require.NotNil(t, result.Snapshot.Requests)
	require.EqualValues(t, 10, *result.Snapshot.Requests.Limit)
	require.EqualValues(t, 7, *result.Snapshot.Requests.Remaining)
	require.Equal(t, "https://api.x.ai/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "lightbridge-grok-quota-probe/1.1", upstream.lastReq.Header.Get("User-Agent"))
	require.Contains(t, string(upstream.lastBody), `"max_output_tokens":1`)
	require.Contains(t, string(upstream.lastBody), `"store":false`)
	require.NotNil(t, repo.updates[42][grokQuotaSnapshotExtraKey])
}

func TestGrokQuotaServiceProbeUsageLoadsProxyWhenAccountEdgeMissing(t *testing.T) {
	proxyID := int64(7)
	account := &Account{
		ID:          46,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		ProxyID:     &proxyID,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	repo := &grokQuotaAccountRepoForTest{
		accounts: map[int64]*Account{46: account},
	}
	proxyRepo := &grokQuotaProxyRepoForTest{
		proxies: map[int64]*Proxy{
			proxyID: {
				ID:       proxyID,
				Protocol: "http",
				Host:     "proxy.test",
				Port:     3128,
			},
		},
	}
	upstream := &grokQuotaHTTPUpstreamForTest{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, proxyRepo, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 46)
	require.NoError(t, err)
	require.Equal(t, 1, proxyRepo.calls)
	require.Equal(t, "http://proxy.test:3128", upstream.lastProxyURL)
}

func TestGrokQuotaServiceProbeUsageReturnsRateLimitedSnapshot(t *testing.T) {
	account := &Account{
		ID:       43,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	repo := &grokQuotaAccountRepoForTest{
		accounts: map[int64]*Account{43: account},
	}
	upstream := &grokQuotaHTTPUpstreamForTest{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"45"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 43)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, result.StatusCode)
	require.NotNil(t, result.Snapshot)
	require.NotNil(t, result.Snapshot.RetryAfterSeconds)
	require.Equal(t, 45, *result.Snapshot.RetryAfterSeconds)
	require.Contains(t, repo.tempUnschedReason[43], "rate limited")
	require.WithinDuration(t, time.Now().Add(45*time.Second), repo.tempUntil[43], 2*time.Second)
	require.NotNil(t, account.TempUnschedulableUntil)
}

func TestGrokQuotaServiceResetQuotaUnsupported(t *testing.T) {
	account := &Account{
		ID:       44,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
	}
	repo := &grokQuotaAccountRepoForTest{
		accounts: map[int64]*Account{44: account},
	}
	svc := NewGrokQuotaService(repo, nil, nil, nil)

	_, err := svc.ResetQuota(context.Background(), 44)
	require.Error(t, err)
	require.Equal(t, http.StatusNotImplemented, infraerrors.Code(err))
	require.Equal(t, "GROK_QUOTA_RESET_UNSUPPORTED", infraerrors.Reason(err))
}

func TestShouldAutoPauseGrokAccountByQuota(t *testing.T) {
	zero := int64(0)
	limit := int64(10)
	resetFuture := time.Now().Add(time.Minute).Unix()
	retryAfter := 30
	tests := []struct {
		name     string
		snapshot xai.QuotaSnapshot
		want     bool
	}{
		{
			name: "trusted build response exhausted",
			snapshot: xai.QuotaSnapshot{
				Requests:          &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				HeadersObserved:   true,
				ObservationSource: "gateway_response",
				UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
			},
			want: true,
		},
		{
			name: "source-less official quota zero ignored for build",
			snapshot: xai.QuotaSnapshot{
				Requests:        &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				HeadersObserved: true,
				UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "trusted retry after active",
			snapshot: xai.QuotaSnapshot{
				RetryAfterSeconds: &retryAfter,
				ObservationSource: "active_probe",
				UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
			},
			want: true,
		},
		{
			name: "source-less retry after ignored for build",
			snapshot: xai.QuotaSnapshot{
				RetryAfterSeconds: &retryAfter,
				UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "retry after expired",
			snapshot: xai.QuotaSnapshot{
				RetryAfterSeconds: &retryAfter,
				UpdatedAt:         time.Now().Add(-time.Duration(retryAfter+1) * time.Second).UTC().Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "stale snapshot ignored",
			snapshot: xai.QuotaSnapshot{
				Requests:          &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				HeadersObserved:   true,
				ObservationSource: "active_probe",
				UpdatedAt:         time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					grokQuotaSnapshotExtraKey: tt.snapshot,
				},
			}

			got, _ := shouldAutoPauseGrokAccountByQuota(account)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGrokQuotaServiceProbeUsageUsesBuildProxyIdentity(t *testing.T) {
	account := &Account{
		ID:          47,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":                grokQuotaJWT(`{"referrer":"grok-build","exp":4102444800}`),
			"expires_at":                  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			GrokCredentialOAuthMode:       string(xai.OAuthModeBuildProxy),
			GrokCredentialTokenCapability: string(xai.TokenCapabilityGrokBuild),
		},
	}
	repo := &grokQuotaAccountRepoForTest{accounts: map[int64]*Account{47: account}}
	upstream := &grokQuotaHTTPUpstreamForTest{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 47)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "xai-grok-cli", upstream.lastReq.Header.Get("X-XAI-Token-Auth"))
	require.Equal(t, "grok-pager", upstream.lastReq.Header.Get("x-grok-client-identifier"))
	require.Equal(t, "0.2.93", upstream.lastReq.Header.Get("x-grok-client-version"))
	require.Equal(t, "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)", upstream.lastReq.Header.Get("User-Agent"))
}

func TestGrokQuotaServiceProbeUsageDefersBuildTokenWithoutReferrerToUpstream(t *testing.T) {
	account := &Account{
		ID:       48,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":          grokQuotaJWT(`{"sub":"user-1","exp":4102444800}`),
			"expires_at":            time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}
	repo := &grokQuotaAccountRepoForTest{accounts: map[int64]*Account{48: account}}
	upstream := &grokQuotaHTTPUpstreamForTest{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 48)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", upstream.lastReq.URL.String())
}

func TestGrokQuotaServiceProbeUsageRejectsBuildTokenWithConflictingReferrer(t *testing.T) {
	account := &Account{
		ID:       49,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":          grokQuotaJWT(`{"sub":"user-1","referrer":"lightbridge","exp":4102444800}`),
			"expires_at":            time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}
	repo := &grokQuotaAccountRepoForTest{accounts: map[int64]*Account{49: account}}
	upstream := &grokQuotaHTTPUpstreamForTest{}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 49)
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
	require.Equal(t, "GROK_BUILD_TOKEN_CONTEXT_MISSING", infraerrors.Reason(err))
	require.Nil(t, upstream.lastReq, "an incompatible token must never reach the upstream")
}

func TestIsGrokProbeAuthorizationFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unauthorized", err: infraerrors.New(http.StatusUnauthorized, "UPSTREAM", "unauthorized"), want: true},
		{name: "forbidden", err: infraerrors.New(http.StatusForbidden, "UPSTREAM", "forbidden"), want: true},
		{name: "spending limit returned as bad request", err: infraerrors.New(http.StatusBadRequest, "GROK_QUOTA_PROBE_UPSTREAM_ERROR", "personal-team-blocked:spending-limit"), want: true},
		{name: "unsupported model is not an auth failure", err: infraerrors.New(http.StatusBadRequest, "GROK_QUOTA_PROBE_UPSTREAM_ERROR", "model not found"), want: false},
		{name: "rate limit", err: infraerrors.New(http.StatusTooManyRequests, "RATE_LIMITED", "retry later"), want: false},
		{name: "upstream unavailable", err: infraerrors.New(http.StatusBadGateway, "UPSTREAM", "network error"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsGrokProbeAuthorizationFailure(tt.err))
		})
	}
}

func TestBuildGrokQuotaProbeBodyUsesGrok45AndExplicitMapping(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"grok-4.5": "grok-4.5-proxy-alias",
			},
		},
	}
	body, err := buildGrokQuotaProbeBody(account)
	require.NoError(t, err)
	require.Contains(t, string(body), `"model":"grok-4.5-proxy-alias"`)

	body, err = buildGrokQuotaProbeBody(nil)
	require.NoError(t, err)
	require.Contains(t, string(body), `"model":"grok-4.5"`)
	require.NotContains(t, string(body), `"model":"grok"`)
}

func TestSummarizeGrokProbeFailureDoesNotLeakUpstreamIdentifiers(t *testing.T) {
	raw := `{"error":"personal-team-blocked:spending-limit","team_id":"team-secret-123","email":"private@example.com"}`
	summary := summarizeGrokProbeFailure(http.StatusBadRequest, raw)
	require.Equal(t, "personal-team-blocked:spending-limit", summary)
	require.NotContains(t, summary, "team-secret-123")
	require.NotContains(t, summary, "private@example.com")
}
