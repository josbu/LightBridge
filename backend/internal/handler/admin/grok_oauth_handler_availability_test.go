package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/tlsfingerprint"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokAvailabilityAdminService struct {
	*stubAdminService
	account          *service.Account
	setErrorCalls    int
	clearErrorCalls  int
	schedulableCalls []bool
}

func (s *grokAvailabilityAdminService) GetAccount(_ context.Context, _ int64) (*service.Account, error) {
	return s.account, nil
}

func (s *grokAvailabilityAdminService) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	if s.account == nil {
		s.account = grokAvailabilityAccount()
	}
	if s.account.ID == 0 {
		s.account.ID = 300
	}
	if input != nil {
		s.account.Name = input.Name
		s.account.Platform = input.Platform
		s.account.Type = input.Type
		s.account.Credentials = input.Credentials
		s.account.Extra = input.Extra
		s.account.ProxyID = input.ProxyID
		s.account.Concurrency = input.Concurrency
		s.account.Status = service.StatusActive
		s.account.Schedulable = true
	}
	return s.account, nil
}

func (s *grokAvailabilityAdminService) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	if input != nil {
		if input.Type != "" {
			s.account.Type = input.Type
		}
		if input.Credentials != nil {
			s.account.Credentials = input.Credentials
		}
	}
	return s.account, nil
}

func (s *grokAvailabilityAdminService) SetAccountError(_ context.Context, _ int64, message string) error {
	s.setErrorCalls++
	s.account.Status = service.StatusError
	s.account.ErrorMessage = message
	s.account.Schedulable = false
	return nil
}

func (s *grokAvailabilityAdminService) ClearAccountError(_ context.Context, _ int64) (*service.Account, error) {
	s.clearErrorCalls++
	s.account.Status = service.StatusActive
	s.account.ErrorMessage = ""
	return s.account, nil
}

func (s *grokAvailabilityAdminService) SetAccountSchedulable(_ context.Context, _ int64, schedulable bool) (*service.Account, error) {
	s.schedulableCalls = append(s.schedulableCalls, schedulable)
	s.account.Schedulable = schedulable
	return s.account, nil
}

type grokAvailabilityAccountRepo struct {
	service.AccountRepository
	account           *service.Account
	tempUnschedCalls  int
	tempUnschedUntil  time.Time
	tempUnschedReason string
}

func (r *grokAvailabilityAccountRepo) GetByID(_ context.Context, _ int64) (*service.Account, error) {
	return r.account, nil
}

func (r *grokAvailabilityAccountRepo) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	return nil
}

func (r *grokAvailabilityAccountRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.tempUnschedCalls++
	r.tempUnschedUntil = until
	r.tempUnschedReason = reason
	if r.account != nil {
		r.account.TempUnschedulableUntil = &until
		r.account.TempUnschedulableReason = reason
	}
	return nil
}

type grokAvailabilityUpstream struct {
	status int
	body   string
	err    error
	calls  int
}

func (u *grokAvailabilityUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{
		StatusCode: u.status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *grokAvailabilityUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func grokAvailabilityJWT(payload string) string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func grokAvailabilityAccount() *service.Account {
	return &service.Account{
		ID:          9001,
		Name:        "grok-build@example.com",
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":                        grokAvailabilityJWT(`{"referrer":"grok-build","exp":4102444800}`),
			"expires_at":                          time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			service.GrokCredentialOAuthMode:       string(xai.OAuthModeBuildProxy),
			service.GrokCredentialTokenCapability: string(xai.TokenCapabilityGrokBuild),
		},
	}
}

func newGrokAvailabilityHandler(account *service.Account, upstream *grokAvailabilityUpstream) (*GrokOAuthHandler, *grokAvailabilityAdminService) {
	adminService := &grokAvailabilityAdminService{
		stubAdminService: newStubAdminService(),
		account:          account,
	}
	repo := &grokAvailabilityAccountRepo{account: account}
	quotaService := service.NewGrokQuotaService(repo, nil, service.NewGrokTokenProvider(repo, nil), upstream)
	return &GrokOAuthHandler{adminService: adminService, quotaService: quotaService}, adminService
}

func TestVerifyGrokAccountAvailabilityDisablesAuthenticationFailure(t *testing.T) {
	account := grokAvailabilityAccount()
	handler, adminService := newGrokAvailabilityHandler(account, &grokAvailabilityUpstream{
		status: http.StatusForbidden,
		body:   `{"error":{"code":"personal-team-blocked:spending-limit","message":"secret upstream detail"}}`,
	})

	result := handler.verifyGrokAccountAvailability(context.Background(), account, false)

	require.Same(t, account, result)
	require.Equal(t, 1, adminService.setErrorCalls)
	require.Equal(t, service.StatusError, result.Status)
	require.False(t, result.Schedulable)
	require.Contains(t, result.ErrorMessage, "Grok Build availability verification failed")
	require.NotContains(t, result.ErrorMessage, "secret upstream detail")
}

func TestVerifyGrokAccountAvailabilityKeepsAccountOnTransientFailure(t *testing.T) {
	account := grokAvailabilityAccount()
	handler, adminService := newGrokAvailabilityHandler(account, &grokAvailabilityUpstream{err: errors.New("temporary network failure")})

	result := handler.verifyGrokAccountAvailability(context.Background(), account, false)

	require.Same(t, account, result)
	require.Zero(t, adminService.setErrorCalls)
	require.Equal(t, service.StatusActive, result.Status)
	require.True(t, result.Schedulable)
}

func TestVerifyGrokAccountAvailabilityProbesTokenWithoutReferrer(t *testing.T) {
	account := grokAvailabilityAccount()
	account.Credentials["access_token"] = grokAvailabilityJWT(`{"sub":"user","exp":4102444800}`)
	account.Credentials[service.GrokCredentialTokenCapability] = string(xai.TokenCapabilityUnknown)
	upstream := &grokAvailabilityUpstream{
		status: http.StatusOK,
		body:   `{"id":"resp_probe","status":"completed"}`,
	}
	handler, adminService := newGrokAvailabilityHandler(account, upstream)

	result := handler.verifyGrokAccountAvailability(context.Background(), account, false)

	require.Same(t, account, result)
	require.Equal(t, 1, upstream.calls)
	require.Zero(t, adminService.setErrorCalls)
	require.Equal(t, service.StatusActive, result.Status)
	require.True(t, result.Schedulable)
}

func TestVerifyGrokAccountAvailabilityRecoversOnlyAfterSuccessfulProbe(t *testing.T) {
	account := grokAvailabilityAccount()
	account.Status = service.StatusError
	account.ErrorMessage = "reauthorization required"
	account.Schedulable = false
	handler, adminService := newGrokAvailabilityHandler(account, &grokAvailabilityUpstream{
		status: http.StatusOK,
		body:   `{"id":"resp_probe","status":"completed"}`,
	})

	result := handler.verifyGrokAccountAvailability(context.Background(), account, true)

	require.Same(t, account, result)
	require.Equal(t, 1, adminService.clearErrorCalls)
	require.Equal(t, []bool{true}, adminService.schedulableCalls)
	require.Equal(t, service.StatusActive, result.Status)
	require.True(t, result.Schedulable)
	require.Empty(t, result.ErrorMessage)
}

func TestVerifyGrokAccountAvailabilityDoesNotRecoverOnRateLimit(t *testing.T) {
	account := grokAvailabilityAccount()
	account.Status = service.StatusError
	account.ErrorMessage = "reauthorization required"
	account.Schedulable = false
	handler, adminService := newGrokAvailabilityHandler(account, &grokAvailabilityUpstream{
		status: http.StatusTooManyRequests,
		body:   `{"error":{"message":"rate limited"}}`,
	})

	result := handler.verifyGrokAccountAvailability(context.Background(), account, true)

	require.Same(t, account, result)
	require.Zero(t, adminService.clearErrorCalls)
	require.Empty(t, adminService.schedulableCalls)
	require.False(t, result.Schedulable)
}

func TestShouldRecoverGrokAccountAfterOAuthOnlyForManagedAvailabilityErrors(t *testing.T) {
	managed := grokAvailabilityAccount()
	managed.ErrorMessage = service.GrokProbeAuthorizationErrorPrefix + " (GROK_QUOTA_PROBE_UPSTREAM_ERROR). Re-authorize."
	require.True(t, shouldRecoverGrokAccountAfterOAuth(managed))

	manual := grokAvailabilityAccount()
	manual.Schedulable = false
	manual.ErrorMessage = "disabled by administrator"
	require.False(t, shouldRecoverGrokAccountAfterOAuth(manual))

	missingContext := grokAvailabilityAccount()
	missingContext.Credentials[service.GrokCredentialReauthRequired] = true
	require.True(t, shouldRecoverGrokAccountAfterOAuth(missingContext))
}

func TestApplyOAuthCredentialsGrokReauthorizationProbesBeforeResuming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := grokAvailabilityAccount()
	account.ID = 501
	account.Status = service.StatusError
	account.Schedulable = false
	account.ErrorMessage = service.GrokProbeAuthorizationErrorPrefix + " (GROK_QUOTA_PROBE_UPSTREAM_ERROR). Re-authorize."
	adminSvc := &grokAvailabilityAdminService{stubAdminService: newStubAdminService(), account: account}
	repo := &grokAvailabilityAccountRepo{account: account}
	upstream := &grokAvailabilityUpstream{status: http.StatusOK, body: `{"id":"resp_probe"}`}
	quotaSvc := service.NewGrokQuotaService(repo, nil, service.NewGrokTokenProvider(repo, nil), upstream)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, quotaSvc)

	payload := map[string]any{
		"type": "oauth",
		"credentials": map[string]any{
			"access_token":                  grokAvailabilityJWT(`{"referrer":"grok-build","exp":4102444800}`),
			"refresh_token":                 "refresh-token",
			"expires_at":                    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			service.GrokCredentialOAuthMode: "build_proxy",
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/accounts/501/apply-oauth-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, adminSvc.clearErrorCalls)
	require.Contains(t, adminSvc.schedulableCalls, true)
	require.True(t, account.Schedulable)
	require.Equal(t, service.StatusActive, account.Status)
}

func TestCreateAccountGrokOAuthProbesAvailabilityBeforeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := grokAvailabilityAccount()
	account.ID = 777
	adminSvc := &grokAvailabilityAdminService{stubAdminService: newStubAdminService(), account: account}
	repo := &grokAvailabilityAccountRepo{account: account}
	upstream := &grokAvailabilityUpstream{
		status: http.StatusForbidden,
		body:   `{"error":{"code":"personal-team-blocked:spending-limit","message":"private upstream detail"}}`,
	}
	quotaSvc := service.NewGrokQuotaService(repo, nil, service.NewGrokTokenProvider(repo, nil), upstream)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, quotaSvc)

	payload := map[string]any{
		"name":        "new-grok-build",
		"platform":    "grok",
		"type":        "oauth",
		"concurrency": 1,
		"credentials": map[string]any{
			"access_token":                  grokAvailabilityJWT(`{"referrer":"grok-build","exp":4102444800}`),
			"refresh_token":                 "refresh-token",
			"expires_at":                    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			service.GrokCredentialOAuthMode: "build_proxy",
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/accounts", handler.Create)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, adminSvc.setErrorCalls)
	require.Equal(t, service.StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, account.ErrorMessage, service.GrokProbeAuthorizationErrorPrefix)
	require.NotContains(t, rec.Body.String(), "private upstream detail")
}
