package config

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	defaultWeChatConnectMode             = "open"
	defaultWeChatConnectScopes           = "snsapi_login"
	defaultWeChatConnectFrontendRedirect = "/auth/wechat/callback"
)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeWeChatConnectMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mp":
		return "mp"
	case "mobile":
		return "mobile"
	default:
		return defaultWeChatConnectMode
	}
}

func normalizeWeChatConnectStoredMode(openEnabled, mpEnabled, mobileEnabled bool, mode string) string {
	mode = normalizeWeChatConnectMode(mode)
	switch mode {
	case "open":
		if openEnabled {
			return "open"
		}
	case "mp":
		if mpEnabled {
			return "mp"
		}
	case "mobile":
		if mobileEnabled {
			return "mobile"
		}
	}
	switch {
	case openEnabled:
		return "open"
	case mpEnabled:
		return "mp"
	case mobileEnabled:
		return "mobile"
	default:
		return mode
	}
}

func defaultWeChatConnectScopesForMode(mode string) string {
	switch normalizeWeChatConnectMode(mode) {
	case "mp":
		return "snsapi_userinfo"
	case "mobile":
		return ""
	default:
		return defaultWeChatConnectScopes
	}
}

func normalizeWeChatConnectScopes(raw, mode string) string {
	switch normalizeWeChatConnectMode(mode) {
	case "mp":
		switch strings.TrimSpace(raw) {
		case "snsapi_base":
			return "snsapi_base"
		case "snsapi_userinfo":
			return "snsapi_userinfo"
		default:
			return defaultWeChatConnectScopesForMode(mode)
		}
	case "mobile":
		return ""
	default:
		return defaultWeChatConnectScopes
	}
}

func shouldApplyLegacyWeChatEnv(configKey, envKey string) bool {
	if viper.InConfig(configKey) {
		return false
	}
	_, hasNewEnv := os.LookupEnv(envKey)
	return !hasNewEnv
}

func hasExplicitConfigOrEnv(configKey, envKey string) bool {
	if viper.InConfig(configKey) {
		return true
	}
	_, ok := os.LookupEnv(envKey)
	return ok
}

func applyLegacyWeChatConnectEnvCompatibility(cfg *WeChatConnectConfig) {
	if cfg == nil {
		return
	}

	legacyOpenAppID := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_app_id", "WECHAT_CONNECT_OPEN_APP_ID") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_id", "WECHAT_CONNECT_APP_ID") {
		legacyOpenAppID = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_OPEN_APP_ID"))
		if legacyOpenAppID != "" {
			cfg.OpenAppID = legacyOpenAppID
		}
	}

	legacyOpenAppSecret := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_app_secret", "WECHAT_CONNECT_OPEN_APP_SECRET") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_secret", "WECHAT_CONNECT_APP_SECRET") {
		legacyOpenAppSecret = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_OPEN_APP_SECRET"))
		if legacyOpenAppSecret != "" {
			cfg.OpenAppSecret = legacyOpenAppSecret
		}
	}

	legacyMPAppID := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_app_id", "WECHAT_CONNECT_MP_APP_ID") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_id", "WECHAT_CONNECT_APP_ID") {
		legacyMPAppID = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_MP_APP_ID"))
		if legacyMPAppID != "" {
			cfg.MPAppID = legacyMPAppID
		}
	}

	legacyMPAppSecret := ""
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_app_secret", "WECHAT_CONNECT_MP_APP_SECRET") &&
		shouldApplyLegacyWeChatEnv("wechat_connect.app_secret", "WECHAT_CONNECT_APP_SECRET") {
		legacyMPAppSecret = strings.TrimSpace(os.Getenv("WECHAT_OAUTH_MP_APP_SECRET"))
		if legacyMPAppSecret != "" {
			cfg.MPAppSecret = legacyMPAppSecret
		}
	}

	if shouldApplyLegacyWeChatEnv("wechat_connect.frontend_redirect_url", "WECHAT_CONNECT_FRONTEND_REDIRECT_URL") {
		if legacyFrontend := strings.TrimSpace(os.Getenv("WECHAT_OAUTH_FRONTEND_REDIRECT_URL")); legacyFrontend != "" {
			cfg.FrontendRedirectURL = legacyFrontend
		}
	}

	hasLegacyOpen := legacyOpenAppID != "" && legacyOpenAppSecret != ""
	hasLegacyMP := legacyMPAppID != "" && legacyMPAppSecret != ""

	if shouldApplyLegacyWeChatEnv("wechat_connect.enabled", "WECHAT_CONNECT_ENABLED") && (hasLegacyOpen || hasLegacyMP) {
		cfg.Enabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.open_enabled", "WECHAT_CONNECT_OPEN_ENABLED") && hasLegacyOpen {
		cfg.OpenEnabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.mp_enabled", "WECHAT_CONNECT_MP_ENABLED") && hasLegacyMP {
		cfg.MPEnabled = true
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.mode", "WECHAT_CONNECT_MODE") {
		switch {
		case hasLegacyMP && !hasLegacyOpen:
			cfg.Mode = "mp"
		case hasLegacyOpen:
			cfg.Mode = "open"
		}
	}
	if shouldApplyLegacyWeChatEnv("wechat_connect.scopes", "WECHAT_CONNECT_SCOPES") {
		switch {
		case hasLegacyMP && !hasLegacyOpen:
			cfg.Scopes = defaultWeChatConnectScopesForMode("mp")
		case hasLegacyOpen:
			cfg.Scopes = defaultWeChatConnectScopesForMode("open")
		}
	}
}

func normalizeWeChatConnectConfig(cfg *WeChatConnectConfig) {
	if cfg == nil {
		return
	}

	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
	cfg.OpenAppID = strings.TrimSpace(cfg.OpenAppID)
	cfg.OpenAppSecret = strings.TrimSpace(cfg.OpenAppSecret)
	cfg.MPAppID = strings.TrimSpace(cfg.MPAppID)
	cfg.MPAppSecret = strings.TrimSpace(cfg.MPAppSecret)
	cfg.MobileAppID = strings.TrimSpace(cfg.MobileAppID)
	cfg.MobileAppSecret = strings.TrimSpace(cfg.MobileAppSecret)
	cfg.Mode = normalizeWeChatConnectMode(cfg.Mode)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	cfg.FrontendRedirectURL = strings.TrimSpace(cfg.FrontendRedirectURL)

	cfg.AppID = firstNonEmptyString(cfg.AppID, cfg.OpenAppID, cfg.MPAppID, cfg.MobileAppID)
	cfg.AppSecret = firstNonEmptyString(cfg.AppSecret, cfg.OpenAppSecret, cfg.MPAppSecret, cfg.MobileAppSecret)
	cfg.OpenAppID = firstNonEmptyString(cfg.OpenAppID, cfg.AppID)
	cfg.OpenAppSecret = firstNonEmptyString(cfg.OpenAppSecret, cfg.AppSecret)
	cfg.MPAppID = firstNonEmptyString(cfg.MPAppID, cfg.AppID)
	cfg.MPAppSecret = firstNonEmptyString(cfg.MPAppSecret, cfg.AppSecret)
	cfg.MobileAppID = firstNonEmptyString(cfg.MobileAppID, cfg.AppID)
	cfg.MobileAppSecret = firstNonEmptyString(cfg.MobileAppSecret, cfg.AppSecret)

	if !cfg.OpenEnabled && !cfg.MPEnabled && !cfg.MobileEnabled && cfg.Enabled {
		switch cfg.Mode {
		case "mp":
			cfg.MPEnabled = true
		case "mobile":
			cfg.MobileEnabled = true
		default:
			cfg.OpenEnabled = true
		}
	}
	cfg.Mode = normalizeWeChatConnectStoredMode(cfg.OpenEnabled, cfg.MPEnabled, cfg.MobileEnabled, cfg.Mode)
	cfg.Scopes = normalizeWeChatConnectScopes(cfg.Scopes, cfg.Mode)
	if cfg.FrontendRedirectURL == "" {
		cfg.FrontendRedirectURL = defaultWeChatConnectFrontendRedirect
	}
}
