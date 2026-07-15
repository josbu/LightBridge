//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func grokServiceJWT(rawPayload string) string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(rawPayload)) + ".signature"
}

func TestGrokBuildAccountWithoutReferrerDefersToUpstream(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":          grokServiceJWT(`{"sub":"user"}`),
			"using_api":             false,
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}
	require.True(t, account.GrokBuildTokenCompatible())
	require.True(t, account.IsSchedulable())
}

func TestGrokBuildAccountWithConflictingReferrerIsNotSchedulable(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":          grokServiceJWT(`{"sub":"user","referrer":"lightbridge"}`),
			"using_api":             false,
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}
	require.False(t, account.GrokBuildTokenCompatible())
	require.False(t, account.IsSchedulable())
}

func TestOfficialGrokAccountDoesNotRequireBuildReferrer(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":          grokServiceJWT(`{"sub":"user"}`),
			"using_api":             true,
			GrokCredentialOAuthMode: string(xai.OAuthModeOfficialAPI),
		},
	}
	require.True(t, account.GrokBuildTokenCompatible())
	require.True(t, account.IsSchedulable())
}

type grokCapabilityCache struct {
	token   string
	deleted bool
}

func (c *grokCapabilityCache) GetAccessToken(context.Context, string) (string, error) {
	return c.token, nil
}
func (c *grokCapabilityCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}
func (c *grokCapabilityCache) DeleteAccessToken(context.Context, string) error {
	c.deleted = true
	return nil
}
func (c *grokCapabilityCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (c *grokCapabilityCache) ReleaseRefreshLock(context.Context, string) error { return nil }

func TestGrokTokenProviderDefersCachedTokenWithoutBuildReferrerToUpstream(t *testing.T) {
	cache := &grokCapabilityCache{token: grokServiceJWT(`{"sub":"user"}`)}
	provider := NewGrokTokenProvider(nil, cache)
	account := &Account{
		ID:       91,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":          cache.token,
			"using_api":             false,
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, cache.token, token)
	require.False(t, cache.deleted)
}

func TestGrokTokenProviderRejectsCachedTokenWithConflictingBuildReferrer(t *testing.T) {
	cache := &grokCapabilityCache{token: grokServiceJWT(`{"sub":"user","referrer":"lightbridge"}`)}
	provider := NewGrokTokenProvider(nil, cache)
	account := &Account{
		ID:       92,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":          cache.token,
			"using_api":             false,
			GrokCredentialOAuthMode: string(xai.OAuthModeBuildProxy),
		},
	}

	_, err := provider.GetAccessToken(context.Background(), account)
	require.ErrorIs(t, err, ErrGrokBuildTokenContextMissing)
	require.True(t, cache.deleted)
}
