package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
)

const (
	GrokCredentialOAuthMode           = "oauth_mode"
	GrokCredentialTokenCapability     = "token_capability"
	GrokCredentialTokenReferrer       = "token_referrer"
	GrokCredentialTokenContextChecked = "token_context_checked_at"
	GrokCredentialReauthRequired      = "grok_reauth_required"
)

var ErrGrokBuildTokenContextMissing = errors.New("grok_build_token_context_missing")

func (a *Account) GrokOAuthMode() xai.OAuthMode {
	if a == nil || !a.IsGrok() {
		return xai.OAuthModeBuildProxy
	}
	if raw := strings.TrimSpace(a.GetCredential(GrokCredentialOAuthMode)); raw != "" {
		return xai.NormalizeOAuthMode(raw)
	}
	if a.GrokUsingAPI() {
		return xai.OAuthModeOfficialAPI
	}
	return xai.OAuthModeBuildProxy
}

func (a *Account) GrokTokenCapability() xai.TokenCapability {
	if a == nil || !a.IsGrok() {
		return xai.TokenCapabilityUnknown
	}
	switch xai.TokenCapability(strings.TrimSpace(a.GetCredential(GrokCredentialTokenCapability))) {
	case xai.TokenCapabilityGrokBuild:
		return xai.TokenCapabilityGrokBuild
	case xai.TokenCapabilityOfficialAPI:
		return xai.TokenCapabilityOfficialAPI
	case xai.TokenCapabilityIncompatible:
		return xai.TokenCapabilityIncompatible
	}
	return xai.ValidateAccessTokenForMode(a.GetGrokAccessToken(), a.GrokOAuthMode()).Capability
}

// GrokBuildTokenCompatible performs a cheap scheduling guard. Opaque tokens and
// JWTs without a referrer claim are allowed to reach xAI for authoritative
// validation, while an explicitly conflicting referrer is kept out of the
// scheduler.
func (a *Account) GrokBuildTokenCompatible() bool {
	if a == nil || !a.IsGrok() || a.GrokUsingAPI() {
		return true
	}
	if raw, ok := a.Credentials[GrokCredentialReauthRequired].(bool); ok && raw {
		return false
	}
	return xai.ValidateAccessTokenForMode(a.GetGrokAccessToken(), a.GrokOAuthMode()).Compatible
}

func (a *Account) GrokTokenReferrer() string {
	if a == nil || !a.IsGrok() {
		return ""
	}
	if stored := strings.TrimSpace(a.GetCredential(GrokCredentialTokenReferrer)); stored != "" {
		return stored
	}
	return xai.InspectAccessToken(a.GetGrokAccessToken()).Referrer
}

func (a *Account) GrokReauthRequired() bool {
	return a != nil && a.IsGrok() && !a.GrokUsingAPI() && !a.GrokBuildTokenCompatible()
}

func validateGrokAccessTokenForAccount(account *Account, token string) error {
	if account == nil || !account.IsGrok() || account.GrokUsingAPI() {
		return nil
	}
	validation := xai.ValidateAccessTokenForMode(token, account.GrokOAuthMode())
	if validation.Compatible {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrGrokBuildTokenContextMissing, validation.Reason)
}
