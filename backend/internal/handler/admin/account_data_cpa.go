package admin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/service"
)

type cpaImportPayload struct {
	Type               any              `json:"type,omitempty"`
	Email              string           `json:"email,omitempty"`
	AccountID          string           `json:"account_id,omitempty"`
	ChatGPTAccountID   string           `json:"chatgpt_account_id,omitempty"`
	PlanType           string           `json:"plan_type,omitempty"`
	ChatGPTPlanType    string           `json:"chatgpt_plan_type,omitempty"`
	IDToken            string           `json:"id_token,omitempty"`
	AccessToken        string           `json:"access_token,omitempty"`
	AccessTokenCamel   string           `json:"accessToken,omitempty"`
	RefreshToken       string           `json:"refresh_token,omitempty"`
	SessionToken       string           `json:"session_token,omitempty"`
	SessionTokenCamel  string           `json:"sessionToken,omitempty"`
	Expired            string           `json:"expired,omitempty"`
	Expires            string           `json:"expires,omitempty"`
	Disabled           *bool            `json:"disabled,omitempty"`
	IDTokenSynthetic   *bool            `json:"id_token_synthetic,omitempty"`
	LoadFactor         any              `json:"load_factor,omitempty"`
	Concurrency        any              `json:"concurrency,omitempty"`
	Priority           any              `json:"priority,omitempty"`
	RateMultiplier     any              `json:"rate_multiplier,omitempty"`
	AutoPauseOnExpired any              `json:"auto_pause_on_expired,omitempty"`
	Account            cpaAccountObject `json:"-"`
	User               cpaUserObject    `json:"user,omitempty"`
	Extra              map[string]any   `json:"extra,omitempty"`
	Proxies            []DataProxy      `json:"proxies,omitempty"`
	Accounts           []DataAccount    `json:"accounts,omitempty"`
	RefreshTokenSingle string           `json:"refreshToken,omitempty"`
	IDTokenSingle      string           `json:"idToken,omitempty"`
	OAuthType          string           `json:"oauth_type,omitempty"`
	AccountSingle      string           `json:"-"`
}

type cpaAccountObject struct {
	ID       string `json:"id,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

type cpaUserObject struct {
	Email string `json:"email,omitempty"`
	ID    string `json:"id,omitempty"`
}

type cpaIDTokenClaims struct {
	Email string `json:"email,omitempty"`
	Auth  struct {
		ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
		ChatGPTPlanType  string `json:"chatgpt_plan_type,omitempty"`
		ChatGPTUserID    string `json:"chatgpt_user_id,omitempty"`
		UserID           string `json:"user_id,omitempty"`
	} `json:"https://api.openai.com/auth"`
}

func normalizeImportDataPayload(raw json.RawMessage) (DataPayload, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return DataPayload{}, errors.New("data is required")
	}

	var payload DataPayload
	if err := json.Unmarshal(raw, &payload); err == nil && looksLikeLightBridgeData(payload) {
		if err := validateDataHeader(payload); err != nil {
			return DataPayload{}, err
		}
		return payload, nil
	}

	converted, ok, err := convertCPAImportPayload(raw)
	if err != nil {
		return DataPayload{}, err
	}
	if ok {
		if err := validateDataHeader(converted); err != nil {
			return DataPayload{}, err
		}
		return converted, nil
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return DataPayload{}, fmt.Errorf("Invalid data JSON: %w", err)
	}
	return DataPayload{}, validateDataHeader(payload)
}

func looksLikeLightBridgeData(payload DataPayload) bool {
	if payload.Type == dataType || payload.Type == legacyDataType {
		return true
	}
	return payload.Type == "" && payload.Proxies != nil && payload.Accounts != nil
}

func convertCPAImportPayload(raw json.RawMessage) (DataPayload, bool, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return DataPayload{}, false, fmt.Errorf("Invalid data JSON: %w", err)
	}
	if !looksLikeCPAImportPayload(probe) {
		return DataPayload{}, false, nil
	}

	var src cpaImportPayload
	if err := json.Unmarshal(raw, &src); err != nil {
		return DataPayload{}, false, fmt.Errorf("Invalid CPA data JSON: %w", err)
	}
	if rawAccount, ok := probe["account"]; ok {
		if isJSONObject(rawAccount) {
			_ = json.Unmarshal(rawAccount, &src.Account)
		} else {
			src.AccountSingle = cpaStringFromRawJSON(rawAccount)
		}
	}

	if len(src.Accounts) > 0 || len(src.Proxies) > 0 {
		return normalizeSub2APIStylePayload(src), true, nil
	}

	account := cpaToDataAccount(src)
	if strings.TrimSpace(account.Name) == "" {
		account.Name = "openai-account"
	}
	return DataPayload{
		Type:       dataType,
		Version:    dataVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    nonNilDataProxies(src.Proxies),
		Accounts:   []DataAccount{account},
	}, true, nil
}

func looksLikeCPAImportPayload(probe map[string]json.RawMessage) bool {
	if _, ok := probe["account_id"]; ok {
		if _, hasAccessToken := probe["access_token"]; hasAccessToken {
			return true
		}
		if _, hasRefreshToken := probe["refresh_token"]; hasRefreshToken {
			return true
		}
	}
	if _, ok := probe["account"]; ok {
		if _, hasAccessToken := probe["accessToken"]; hasAccessToken {
			return true
		}
		if _, hasAccessToken := probe["access_token"]; hasAccessToken {
			return true
		}
	}
	return false
}

func normalizeSub2APIStylePayload(src cpaImportPayload) DataPayload {
	payload := DataPayload{
		Type:       dataType,
		Version:    dataVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    nonNilDataProxies(src.Proxies),
		Accounts:   make([]DataAccount, 0, len(src.Accounts)),
	}
	for i := range src.Accounts {
		account := src.Accounts[i]
		if account.Concurrency == 0 {
			account.Concurrency = 10
		}
		if account.Priority == 0 {
			account.Priority = 1
		}
		if account.RateMultiplier == nil {
			v := 1.0
			account.RateMultiplier = &v
		}
		if account.AutoPauseOnExpired == nil {
			v := true
			account.AutoPauseOnExpired = &v
		}
		payload.Accounts = append(payload.Accounts, account)
	}
	return payload
}

func cpaToDataAccount(src cpaImportPayload) DataAccount {
	claims := decodeCPAIDToken(src.IDToken)
	accountID := cpaFirstNonEmpty(
		src.AccountID,
		src.ChatGPTAccountID,
		src.Account.ID,
		src.AccountSingle,
		claims.Auth.ChatGPTAccountID,
	)
	planType := cpaFirstNonEmpty(
		src.PlanType,
		src.ChatGPTPlanType,
		src.Account.PlanType,
		claims.Auth.ChatGPTPlanType,
		"plus",
	)
	email := cpaFirstNonEmpty(src.Email, src.User.Email, claims.Email)
	accessToken := cpaFirstNonEmpty(src.AccessToken, src.AccessTokenCamel)
	sessionToken := cpaFirstNonEmpty(src.SessionToken, src.SessionTokenCamel)
	refreshToken := cpaFirstNonEmpty(src.RefreshToken, src.RefreshTokenSingle)
	idToken := cpaFirstNonEmpty(src.IDToken, src.IDTokenSingle)
	if idToken == "" {
		idToken = buildCPASyntheticIDToken(accountID, planType, email, cpaFirstNonEmpty(src.User.ID, claims.Auth.UserID, claims.Auth.ChatGPTUserID), cpaFirstNonEmpty(src.Expired, src.Expires))
	}

	credentials := map[string]any{
		"refresh_token":      refreshToken,
		"id_token":           idToken,
		"access_token":       accessToken,
		"session_token":      sessionToken,
		"chatgpt_account_id": accountID,
		"email":              email,
	}
	if planType != "" {
		credentials["chatgpt_plan_type"] = planType
	}
	if src.IDTokenSynthetic != nil {
		credentials["id_token_synthetic"] = *src.IDTokenSynthetic
	} else if src.IDToken == "" {
		credentials["id_token_synthetic"] = true
	}

	extra := map[string]any{}
	for k, v := range src.Extra {
		extra[k] = v
	}
	if _, ok := extra["load_factor"]; !ok {
		extra["load_factor"] = numberOrDefault(src.LoadFactor, 10)
	}
	extra["import_source"] = "cliproxyapi"

	rateMultiplier := floatOrDefaultPtr(src.RateMultiplier, 1)
	autoPause := boolOrDefaultPtr(src.AutoPauseOnExpired, true)

	return DataAccount{
		Name:               cpaFirstNonEmpty(email, accountID, "openai-account"),
		Platform:           service.PlatformOpenAI,
		Type:               service.AccountTypeOAuth,
		Credentials:        credentials,
		Extra:              extra,
		Concurrency:        intOrDefault(src.Concurrency, 10),
		Priority:           intOrDefault(src.Priority, 1),
		RateMultiplier:     rateMultiplier,
		AutoPauseOnExpired: autoPause,
	}
}

func decodeCPAIDToken(token string) cpaIDTokenClaims {
	var claims cpaIDTokenClaims
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return claims
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims
	}
	_ = json.Unmarshal(payload, &claims)
	return claims
}

func buildCPASyntheticIDToken(accountID, planType, email, userID, expired string) string {
	now := time.Now().Unix()
	exp := now + 60*60*24*90
	if parsed := parseCPAUnixSeconds(expired); parsed > 0 {
		exp = parsed
	}
	if userID == "" {
		userID = "user-unknown"
	}
	payload := map[string]any{
		"iat":   now,
		"exp":   exp,
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  planType,
			"chatgpt_user_id":    userID,
			"user_id":            userID,
		},
	}
	header := map[string]any{"alg": "none", "typ": "JWT", "cpa_synthetic": true}
	headerBytes, _ := json.Marshal(header)
	payloadBytes, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(payloadBytes) + "."
}

func parseCPAUnixSeconds(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed.Unix()
	}
	return 0
}

func nonNilDataProxies(proxies []DataProxy) []DataProxy {
	if proxies == nil {
		return []DataProxy{}
	}
	return proxies
}

func cpaFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func intOrDefault(value any, fallback int) int {
	switch v := value.(type) {
	case float64:
		if v != 0 {
			return int(v)
		}
	case int:
		if v != 0 {
			return v
		}
	case json.Number:
		if parsed, err := v.Int64(); err == nil && parsed != 0 {
			return int(parsed)
		}
	}
	return fallback
}

func numberOrDefault(value any, fallback int) any {
	if value == nil {
		return fallback
	}
	return value
}

func floatOrDefaultPtr(value any, fallback float64) *float64 {
	result := fallback
	switch v := value.(type) {
	case float64:
		result = v
	case int:
		result = float64(v)
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			result = parsed
		}
	}
	return &result
}

func boolOrDefaultPtr(value any, fallback bool) *bool {
	result := fallback
	if v, ok := value.(bool); ok {
		result = v
	}
	return &result
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{")
}

func cpaStringFromRawJSON(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}
