package xai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	maxInspectableAccessTokenLen = 64 * 1024
	maxInspectableJWTPayloadLen  = 16 * 1024
	GrokBuildTokenReferrer       = "grok-build"
)

// OAuthMode identifies the xAI entitlement path selected when the OAuth
// session is created. Build proxy and official API tokens are not assumed to
// be interchangeable.
type OAuthMode string

const (
	OAuthModeBuildProxy  OAuthMode = "build_proxy"
	OAuthModeOfficialAPI OAuthMode = "official_api"
)

// ParseOAuthMode validates a user supplied mode. Empty values intentionally
// default to Build Proxy because LightBridge's Grok OAuth integration is aimed
// at Grok Build subscriptions; official API usage must be explicit.
func ParseOAuthMode(raw string) (OAuthMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "build", "build_proxy", "grok-build", "grok_build", "cli", "cli_proxy":
		return OAuthModeBuildProxy, nil
	case "api", "official", "official_api", "xai_api":
		return OAuthModeOfficialAPI, nil
	default:
		return "", fmt.Errorf("unsupported Grok OAuth mode %q", strings.TrimSpace(raw))
	}
}

// NormalizeOAuthMode is intended for persisted credentials created by older
// LightBridge versions. Unknown values fail closed to Build Proxy rather than
// silently enabling billable official API traffic.
func NormalizeOAuthMode(raw string) OAuthMode {
	mode, err := ParseOAuthMode(raw)
	if err != nil {
		return OAuthModeBuildProxy
	}
	return mode
}

func (m OAuthMode) UsingAPI() bool {
	return NormalizeOAuthMode(string(m)) == OAuthModeOfficialAPI
}

func (m OAuthMode) AuthorizationReferrer() string {
	if NormalizeOAuthMode(string(m)) == OAuthModeBuildProxy {
		return GrokBuildTokenReferrer
	}
	return "lightbridge"
}

// TokenCapability is a diagnostic classification. It is not an authorization
// decision: JWT payloads are decoded without signature verification and xAI
// remains the authority that accepts or rejects the bearer token.
type TokenCapability string

const (
	TokenCapabilityUnknown      TokenCapability = "unknown"
	TokenCapabilityGrokBuild    TokenCapability = "grok_build"
	TokenCapabilityOfficialAPI  TokenCapability = "official_api"
	TokenCapabilityIncompatible TokenCapability = "incompatible"
)

// AccessTokenInspection contains non-secret claims decoded from the JWT
// payload. Parsed means only that the payload was syntactically valid; it does
// not mean the token signature was verified.
type AccessTokenInspection struct {
	Parsed    bool
	Referrer  string
	ExpiresAt int64
	Subject   string
	Email     string
}

// InspectAccessToken performs a bounded, unverified JWT payload decode for
// compatibility diagnostics. It never logs or returns the original token.
func InspectAccessToken(token string) AccessTokenInspection {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxInspectableAccessTokenLen {
		return AccessTokenInspection{}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return AccessTokenInspection{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) == 0 || len(payload) > maxInspectableJWTPayloadLen {
		return AccessTokenInspection{}
	}
	var claims struct {
		Referrer string      `json:"referrer"`
		Exp      json.Number `json:"exp"`
		Sub      string      `json:"sub"`
		Email    string      `json:"email"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&claims); err != nil {
		return AccessTokenInspection{}
	}
	expiresAt := int64(0)
	if claims.Exp != "" {
		if parsed, err := claims.Exp.Int64(); err == nil {
			expiresAt = parsed
		} else if parsedFloat, err := strconv.ParseFloat(string(claims.Exp), 64); err == nil {
			expiresAt = int64(parsedFloat)
		}
	}
	return AccessTokenInspection{
		Parsed:    true,
		Referrer:  strings.TrimSpace(claims.Referrer),
		ExpiresAt: expiresAt,
		Subject:   strings.TrimSpace(claims.Sub),
		Email:     strings.TrimSpace(claims.Email),
	}
}

type TokenModeValidation struct {
	Mode       OAuthMode
	Capability TokenCapability
	Compatible bool
	Reason     string
	Inspection AccessTokenInspection
}

// ValidateAccessTokenForMode classifies whether a token payload is compatible
// with the selected OAuth mode. Opaque/non-JWT tokens and JWTs that omit the
// optional referrer claim remain "unknown" and are allowed to reach xAI for
// authoritative validation. Only an explicit, conflicting referrer fails fast.
func ValidateAccessTokenForMode(token string, mode OAuthMode) TokenModeValidation {
	mode = NormalizeOAuthMode(string(mode))
	inspection := InspectAccessToken(token)
	validation := TokenModeValidation{
		Mode:       mode,
		Capability: TokenCapabilityUnknown,
		Compatible: strings.TrimSpace(token) != "",
		Inspection: inspection,
	}
	if strings.TrimSpace(token) == "" {
		validation.Capability = TokenCapabilityIncompatible
		validation.Reason = "access token is empty"
		return validation
	}
	if mode == OAuthModeOfficialAPI {
		validation.Capability = TokenCapabilityOfficialAPI
		validation.Reason = "official API mode does not require a Grok Build referrer claim"
		return validation
	}
	if !inspection.Parsed {
		validation.Reason = "access token is opaque; capability will be verified by the Grok Build upstream"
		return validation
	}
	if strings.EqualFold(inspection.Referrer, GrokBuildTokenReferrer) {
		validation.Capability = TokenCapabilityGrokBuild
		validation.Reason = "access token carries the Grok Build referrer claim"
		return validation
	}
	if inspection.Referrer == "" {
		validation.Reason = "access token does not expose a referrer claim; capability will be verified by the Grok Build upstream"
		return validation
	}
	validation.Capability = TokenCapabilityIncompatible
	validation.Compatible = false
	validation.Reason = "access token referrer is not grok-build"
	return validation
}
