package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/service"
)

type compatImportCandidate struct {
	Fields map[string]string
}

var compatKeyValuePattern = regexp.MustCompile(`(?i)(refresh[\s_-]*token|access[\s_-]*token|id[\s_-]*token|session[\s_-]*token|account[\s_-]*id|chatgpt[\s_-]*account[\s_-]*id|chatgpt[\s_-]*user[\s_-]*id|user[\s_-]*id|organization[\s_-]*id|org[\s_-]*id|email|plan[\s_-]*type|chatgpt[\s_-]*plan[\s_-]*type|expires[\s_-]*at|expired|expires)\s*[:=]\s*["']?([^"',;\s\r\n]+)`)

func normalizeCompatibilityImportPayload(raw json.RawMessage) (DataPayload, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return DataPayload{}, errors.New("data is required")
	}

	var candidates []compatImportCandidate
	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err == nil {
		if text, ok := parsed.(string); ok {
			candidates = append(candidates, compatCandidatesFromText(text)...)
		} else {
			candidates = append(candidates, compatCandidatesFromJSON(parsed)...)
		}
	} else {
		candidates = append(candidates, compatCandidatesFromText(string(raw))...)
	}

	accounts := compatCandidatesToDataAccounts(candidates)
	if len(accounts) == 0 {
		return DataPayload{}, errors.New("compatibility import could not find supported token fields")
	}

	return DataPayload{
		Type:       dataType,
		Version:    dataVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    []DataProxy{},
		Accounts:   accounts,
	}, nil
}

func compatCandidatesFromJSON(value any) []compatImportCandidate {
	var out []compatImportCandidate
	compatCollectJSONCandidates(value, &out)
	return out
}

func compatCollectJSONCandidates(value any, out *[]compatImportCandidate) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			compatCollectJSONCandidates(item, out)
		}
	case map[string]any:
		if fields := compatExtractFieldsFromMap(v); compatHasTokenField(fields) {
			*out = append(*out, compatImportCandidate{Fields: fields})
		}
		for _, item := range v {
			switch item.(type) {
			case []any:
				compatCollectJSONCandidates(item, out)
			}
		}
	}
}

func compatExtractFieldsFromMap(source map[string]any) map[string]string {
	fields := map[string]string{}
	var walk func(map[string]any)
	walk = func(current map[string]any) {
		for key, raw := range current {
			canonical := compatCanonicalField(key)
			switch v := raw.(type) {
			case string:
				if canonical != "" && strings.TrimSpace(v) != "" {
					fields[canonical] = strings.TrimSpace(v)
				}
			case json.Number:
				if canonical != "" {
					fields[canonical] = v.String()
				}
			case float64:
				if canonical != "" {
					fields[canonical] = strconv.FormatFloat(v, 'f', -1, 64)
				}
			case bool:
				if canonical != "" {
					fields[canonical] = strconv.FormatBool(v)
				}
			case map[string]any:
				walk(v)
			}
		}
	}
	walk(source)
	return fields
}

func compatCandidatesFromText(content string) []compatImportCandidate {
	fields := map[string]string{}
	for _, match := range compatKeyValuePattern.FindAllStringSubmatch(content, -1) {
		if len(match) != 3 {
			continue
		}
		canonical := compatCanonicalField(match[1])
		value := strings.Trim(strings.TrimSpace(match[2]), `"'`)
		if canonical != "" && value != "" {
			fields[canonical] = value
		}
	}
	if !compatHasTokenField(fields) {
		return nil
	}
	return []compatImportCandidate{{Fields: fields}}
}

func compatCandidatesToDataAccounts(candidates []compatImportCandidate) []DataAccount {
	seen := map[string]struct{}{}
	accounts := make([]DataAccount, 0, len(candidates))
	for _, candidate := range candidates {
		fields := candidate.Fields
		if !compatHasTokenField(fields) {
			continue
		}
		key := compatCandidateKey(fields)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		credentials := map[string]any{}
		for _, name := range []string{
			"refresh_token",
			"access_token",
			"id_token",
			"session_token",
			"chatgpt_account_id",
			"chatgpt_user_id",
			"organization_id",
			"email",
			"chatgpt_plan_type",
			"expires_at",
		} {
			if value := strings.TrimSpace(fields[name]); value != "" {
				credentials[name] = value
			}
		}

		var expiresAt *int64
		if parsed := compatParseTimeUnix(fields["expires_at"]); parsed > 0 {
			expiresAt = &parsed
		}
		autoPause := true
		rateMultiplier := 1.0
		accounts = append(accounts, DataAccount{
			Name:               compatAccountName(fields),
			Platform:           service.PlatformOpenAI,
			Type:               service.AccountTypeOAuth,
			Credentials:        credentials,
			Extra:              map[string]any{"import_source": "compatibility"},
			Concurrency:        10,
			Priority:           1,
			RateMultiplier:     &rateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &autoPause,
		})
	}
	return accounts
}

func compatCanonicalField(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	for strings.Contains(normalized, "__") {
		normalized = strings.ReplaceAll(normalized, "__", "_")
	}
	switch normalized {
	case "refresh_token", "refreshtoken", "oauth_refresh_token", "rt":
		return "refresh_token"
	case "access_token", "accesstoken", "oauth_access_token", "at":
		return "access_token"
	case "id_token", "idtoken":
		return "id_token"
	case "session_token", "sessiontoken":
		return "session_token"
	case "account_id", "accountid", "chatgpt_account_id", "chatgptaccountid":
		return "chatgpt_account_id"
	case "user_id", "userid", "chatgpt_user_id", "chatgptuserid":
		return "chatgpt_user_id"
	case "organization_id", "organizationid", "org_id", "orgid":
		return "organization_id"
	case "email", "mail":
		return "email"
	case "plan_type", "plantype", "chatgpt_plan_type", "chatgptplantype":
		return "chatgpt_plan_type"
	case "expires_at", "expiresat", "expired", "expires", "expiry", "expiration":
		return "expires_at"
	default:
		return ""
	}
}

func compatHasTokenField(fields map[string]string) bool {
	for _, key := range []string{"refresh_token", "access_token", "id_token", "session_token"} {
		if strings.TrimSpace(fields[key]) != "" {
			return true
		}
	}
	return false
}

func compatCandidateKey(fields map[string]string) string {
	return strings.Join([]string{
		fields["refresh_token"],
		fields["access_token"],
		fields["id_token"],
		fields["session_token"],
		fields["chatgpt_account_id"],
		fields["email"],
	}, "|")
}

func compatAccountName(fields map[string]string) string {
	for _, key := range []string{"email", "chatgpt_account_id", "chatgpt_user_id"} {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return "compatibility-openai-account"
}

func compatParseTimeUnix(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds > 1_000_000_000_000 {
			return seconds / 1000
		}
		return seconds
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}
