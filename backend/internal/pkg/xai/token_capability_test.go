//go:build unit

package xai

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func jwtWithPayload(raw string) string {
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(raw)) + ".signature"
}

func TestBuildOAuthModeUsesGrokBuildReferrer(t *testing.T) {
	mode, err := ParseOAuthMode("")
	require.NoError(t, err)
	require.Equal(t, OAuthModeBuildProxy, mode)
	require.Equal(t, GrokBuildTokenReferrer, mode.AuthorizationReferrer())
}

func TestValidateAccessTokenForBuildMode(t *testing.T) {
	valid := ValidateAccessTokenForMode(jwtWithPayload(`{"sub":"user","referrer":"grok-build"}`), OAuthModeBuildProxy)
	require.True(t, valid.Compatible)
	require.Equal(t, TokenCapabilityGrokBuild, valid.Capability)

	missing := ValidateAccessTokenForMode(jwtWithPayload(`{"sub":"user"}`), OAuthModeBuildProxy)
	require.True(t, missing.Compatible)
	require.Equal(t, TokenCapabilityUnknown, missing.Capability)
	require.True(t, missing.Inspection.Parsed)
	require.Contains(t, missing.Reason, "verified by the Grok Build upstream")

	conflicting := ValidateAccessTokenForMode(jwtWithPayload(`{"sub":"user","referrer":"lightbridge"}`), OAuthModeBuildProxy)
	require.False(t, conflicting.Compatible)
	require.Equal(t, TokenCapabilityIncompatible, conflicting.Capability)
	require.Contains(t, conflicting.Reason, "not grok-build")
}

func TestValidateOpaqueAccessTokenDefersToUpstream(t *testing.T) {
	validation := ValidateAccessTokenForMode("opaque-token", OAuthModeBuildProxy)
	require.True(t, validation.Compatible)
	require.Equal(t, TokenCapabilityUnknown, validation.Capability)
	require.False(t, validation.Inspection.Parsed)
}

func TestOfficialModeDoesNotRequireBuildReferrer(t *testing.T) {
	validation := ValidateAccessTokenForMode(jwtWithPayload(`{"sub":"user"}`), OAuthModeOfficialAPI)
	require.True(t, validation.Compatible)
	require.Equal(t, TokenCapabilityOfficialAPI, validation.Capability)
}
