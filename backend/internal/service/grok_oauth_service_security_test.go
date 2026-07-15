//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthSecurityClient struct {
	redirectURI        string
	proxyURL           string
	accessToken        string
	refreshAccessToken string
}

func (c *grokOAuthSecurityClient) ExchangeCode(_ context.Context, _ string, _ string, redirectURI string, proxyURL string, _ string) (*xai.TokenResponse, error) {
	c.redirectURI = redirectURI
	c.proxyURL = proxyURL
	accessToken := c.accessToken
	if accessToken == "" {
		accessToken = "access"
	}
	return &xai.TokenResponse{AccessToken: accessToken, RefreshToken: "refresh", ExpiresIn: 3600}, nil
}

func (c *grokOAuthSecurityClient) RefreshToken(_ context.Context, _ string, _ string, _ string) (*xai.TokenResponse, error) {
	accessToken := c.refreshAccessToken
	if accessToken == "" {
		accessToken = "access"
	}
	return &xai.TokenResponse{AccessToken: accessToken, ExpiresIn: 3600}, nil
}

func serviceJWT(rawPayload string) string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(rawPayload)) + ".signature"
}

func TestGrokOAuthAuthorizationDefaultsToBuildContext(t *testing.T) {
	client := &grokOAuthSecurityClient{}
	service := NewGrokOAuthService(nil, client)
	result, err := service.GenerateAuthURL(context.Background(), nil, "http://localhost:1455/auth/callback")
	require.NoError(t, err)
	require.Equal(t, xai.OAuthModeBuildProxy, result.OAuthMode)
	parsed, err := url.Parse(result.AuthURL)
	require.NoError(t, err)
	require.Equal(t, xai.GrokBuildTokenReferrer, parsed.Query().Get("referrer"))
}

func TestGrokOAuthDefersParsedTokenWithoutBuildReferrerToUpstream(t *testing.T) {
	client := &grokOAuthSecurityClient{accessToken: serviceJWT(`{"sub":"user"}`)}
	service := NewGrokOAuthService(nil, client)
	result, err := service.GenerateAuthURL(context.Background(), nil, "http://localhost:1455/auth/callback")
	require.NoError(t, err)

	info, err := service.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "bare-code",
		State:     result.State,
	})
	require.NoError(t, err)
	require.Equal(t, xai.TokenCapabilityUnknown, info.TokenCapability)
	require.Empty(t, info.TokenReferrer)
	require.False(t, info.UsingAPI)
	require.Equal(t, xai.DefaultCLIBaseURL, info.BaseURL)

	creds := service.BuildAccountCredentials(info)
	require.Equal(t, string(xai.TokenCapabilityUnknown), creds[GrokCredentialTokenCapability])
	require.Equal(t, false, creds[GrokCredentialReauthRequired])
}

func TestGrokOAuthRejectsParsedTokenWithConflictingBuildReferrer(t *testing.T) {
	client := &grokOAuthSecurityClient{accessToken: serviceJWT(`{"sub":"user","referrer":"lightbridge"}`)}
	service := NewGrokOAuthService(nil, client)
	result, err := service.GenerateAuthURL(context.Background(), nil, "http://localhost:1455/auth/callback")
	require.NoError(t, err)

	_, err = service.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "bare-code",
		State:     result.State,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "grok_build_token_context_missing")
}

func TestGrokOAuthAcceptsBuildReferrerAndPersistsCapability(t *testing.T) {
	client := &grokOAuthSecurityClient{accessToken: serviceJWT(`{"sub":"user","referrer":"grok-build"}`)}
	service := NewGrokOAuthService(nil, client)
	result, err := service.GenerateAuthURL(context.Background(), nil, "http://localhost:1455/auth/callback")
	require.NoError(t, err)

	info, err := service.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "bare-code",
		State:     result.State,
	})
	require.NoError(t, err)
	require.Equal(t, xai.TokenCapabilityGrokBuild, info.TokenCapability)
	require.Equal(t, "grok-build", info.TokenReferrer)
	require.False(t, info.UsingAPI)
	require.Equal(t, xai.DefaultCLIBaseURL, info.BaseURL)

	creds := service.BuildAccountCredentials(info)
	require.Equal(t, string(xai.OAuthModeBuildProxy), creds[GrokCredentialOAuthMode])
	require.Equal(t, string(xai.TokenCapabilityGrokBuild), creds[GrokCredentialTokenCapability])
	require.Equal(t, false, creds[GrokCredentialReauthRequired])
}

func TestGrokOAuthExchangeRequiresStateForBareCode(t *testing.T) {
	client := &grokOAuthSecurityClient{}
	service := NewGrokOAuthService(nil, client)
	result, err := service.GenerateAuthURL(context.Background(), nil, "http://localhost:1455/auth/callback")
	require.NoError(t, err)

	_, err = service.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "bare-code",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth state is required")
}

func TestGrokOAuthExchangeFreezesRedirectURIFromSession(t *testing.T) {
	client := &grokOAuthSecurityClient{}
	service := NewGrokOAuthService(nil, client)
	const originalRedirect = "http://localhost:1455/auth/callback"
	result, err := service.GenerateAuthURL(context.Background(), nil, originalRedirect)
	require.NoError(t, err)

	_, err = service.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID:   result.SessionID,
		Code:        "bare-code",
		State:       result.State,
		RedirectURI: "http://attacker.invalid/callback",
	})
	require.NoError(t, err)
	require.Equal(t, originalRedirect, client.redirectURI)
	require.Empty(t, client.proxyURL)
}

func TestGrokOAuthRefreshDefersBuildTokenWithoutReferrerToUpstream(t *testing.T) {
	client := &grokOAuthSecurityClient{refreshAccessToken: serviceJWT(`{"sub":"user","exp":4102444800}`)}
	oauthService := NewGrokOAuthService(nil, client)

	info, err := oauthService.RefreshToken(context.Background(), "refresh-token", "", "", xai.OAuthModeBuildProxy)
	require.NoError(t, err)
	require.Equal(t, xai.TokenCapabilityUnknown, info.TokenCapability)
	require.Empty(t, info.TokenReferrer)
	require.False(t, info.UsingAPI)
	require.Equal(t, xai.DefaultCLIBaseURL, info.BaseURL)
}

func TestGrokOAuthRefreshAllowsOfficialTokenWithoutBuildReferrer(t *testing.T) {
	client := &grokOAuthSecurityClient{refreshAccessToken: serviceJWT(`{"sub":"user","exp":4102444800}`)}
	oauthService := NewGrokOAuthService(nil, client)

	info, err := oauthService.RefreshToken(context.Background(), "refresh-token", "", "", xai.OAuthModeOfficialAPI)
	require.NoError(t, err)
	require.Equal(t, xai.TokenCapabilityOfficialAPI, info.TokenCapability)
	require.True(t, info.UsingAPI)
	require.Equal(t, xai.DefaultBaseURL, info.BaseURL)
}
