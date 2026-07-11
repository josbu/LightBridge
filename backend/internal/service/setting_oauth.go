package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/imroc/req/v3"
)

// GetLinuxDoConnectOAuthConfig 返回用于登录的"最终生效" LinuxDo Connect 配置。
//
// 优先级：
// - 若对应系统设置键存在，则覆盖 config.yaml/env 的值
// - 否则回退到 config.yaml/env 的值
func (s *SettingService) GetLinuxDoConnectOAuthConfig(ctx context.Context) (config.LinuxDoConnectConfig, error) {
	if s == nil || s.cfg == nil {
		return config.LinuxDoConnectConfig{}, infraerrors.ServiceUnavailable("CONFIG_NOT_READY", "config not loaded")
	}

	effective := s.cfg.LinuxDo

	keys := []string{
		SettingKeyLinuxDoConnectEnabled,
		SettingKeyLinuxDoConnectClientID,
		SettingKeyLinuxDoConnectClientSecret,
		SettingKeyLinuxDoConnectRedirectURL,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return config.LinuxDoConnectConfig{}, fmt.Errorf("get linuxdo connect settings: %w", err)
	}

	if raw, ok := settings[SettingKeyLinuxDoConnectEnabled]; ok {
		effective.Enabled = raw == "true"
	}
	if v, ok := settings[SettingKeyLinuxDoConnectClientID]; ok && strings.TrimSpace(v) != "" {
		effective.ClientID = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyLinuxDoConnectClientSecret]; ok && strings.TrimSpace(v) != "" {
		effective.ClientSecret = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyLinuxDoConnectRedirectURL]; ok && strings.TrimSpace(v) != "" {
		effective.RedirectURL = strings.TrimSpace(v)
	}
	if !effective.Enabled {
		return config.LinuxDoConnectConfig{}, infraerrors.NotFound("OAUTH_DISABLED", "oauth login is disabled")
	}

	// 基础健壮性校验（避免把用户重定向到一个必然失败或不安全的 OAuth 流程里）。
	if strings.TrimSpace(effective.ClientID) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth client id not configured")
	}
	if strings.TrimSpace(effective.AuthorizeURL) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth authorize url not configured")
	}
	if strings.TrimSpace(effective.TokenURL) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token url not configured")
	}
	if strings.TrimSpace(effective.UserInfoURL) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth userinfo url not configured")
	}
	if strings.TrimSpace(effective.RedirectURL) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth redirect url not configured")
	}
	if strings.TrimSpace(effective.FrontendRedirectURL) == "" {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth frontend redirect url not configured")
	}

	if err := config.ValidateAbsoluteHTTPURL(effective.AuthorizeURL); err != nil {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth authorize url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.TokenURL); err != nil {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.UserInfoURL); err != nil {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth userinfo url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.RedirectURL); err != nil {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth redirect url invalid")
	}
	if err := config.ValidateFrontendRedirectURL(effective.FrontendRedirectURL); err != nil {
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth frontend redirect url invalid")
	}

	method := strings.ToLower(strings.TrimSpace(effective.TokenAuthMethod))
	switch method {
	case "", "client_secret_post", "client_secret_basic":
		if strings.TrimSpace(effective.ClientSecret) == "" {
			return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth client secret not configured")
		}
	case "none":
	default:
		return config.LinuxDoConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token_auth_method invalid")
	}

	return effective, nil
}

// GetDingTalkConnectOAuthConfig 返回用于登录的"最终生效" DingTalk Connect 配置。
//
// 优先级：
// - 若对应系统设置键存在，则覆盖 config.yaml/env 的值
// - 否则回退到 config.yaml/env 的值
func (s *SettingService) GetDingTalkConnectOAuthConfig(ctx context.Context) (config.DingTalkConnectConfig, error) {
	if s == nil || s.cfg == nil {
		return config.DingTalkConnectConfig{}, infraerrors.ServiceUnavailable("CONFIG_NOT_READY", "config not loaded")
	}

	effective := s.cfg.DingTalk

	keys := []string{
		SettingKeyDingTalkConnectEnabled,
		SettingKeyDingTalkConnectClientID,
		SettingKeyDingTalkConnectClientSecret,
		SettingKeyDingTalkConnectRedirectURL,
		SettingKeyDingTalkConnectCorpRestrictionPolicy,
		SettingKeyDingTalkConnectInternalCorpID,
		SettingKeyDingTalkConnectBypassRegistration,
		SettingKeyDingTalkConnectSyncCorpEmail,
		SettingKeyDingTalkConnectSyncDisplayName,
		SettingKeyDingTalkConnectSyncDept,
		SettingKeyDingTalkConnectSyncCorpEmailAttrKey,
		SettingKeyDingTalkConnectSyncDisplayNameAttrKey,
		SettingKeyDingTalkConnectSyncDeptAttrKey,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return config.DingTalkConnectConfig{}, fmt.Errorf("get dingtalk connect settings: %w", err)
	}

	if raw, ok := settings[SettingKeyDingTalkConnectEnabled]; ok {
		effective.Enabled = raw == "true"
	}
	if v, ok := settings[SettingKeyDingTalkConnectClientID]; ok && strings.TrimSpace(v) != "" {
		effective.ClientID = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyDingTalkConnectClientSecret]; ok && strings.TrimSpace(v) != "" {
		effective.ClientSecret = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyDingTalkConnectRedirectURL]; ok && strings.TrimSpace(v) != "" {
		effective.RedirectURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyDingTalkConnectCorpRestrictionPolicy]; ok && strings.TrimSpace(v) != "" {
		effective.CorpRestrictionPolicy = strings.TrimSpace(v)
	}
	effective.CorpRestrictionPolicy = coerceDeprecatedDingTalkCorpPolicy(effective.CorpRestrictionPolicy)
	if v, ok := settings[SettingKeyDingTalkConnectInternalCorpID]; ok && strings.TrimSpace(v) != "" {
		effective.InternalCorpID = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyDingTalkConnectBypassRegistration]; ok && strings.TrimSpace(v) != "" {
		effective.BypassRegistration = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	// bypass_registration 仅在 internal_only 模式下有意义；其它策略下强制 false，
	// 以保证 OAuth callback 看到的 effective config 永远是一致状态。
	if effective.CorpRestrictionPolicy != "internal_only" {
		effective.BypassRegistration = false
	}

	if v, ok := settings[SettingKeyDingTalkConnectSyncCorpEmail]; ok && strings.TrimSpace(v) != "" {
		effective.SyncCorpEmail = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v, ok := settings[SettingKeyDingTalkConnectSyncDisplayName]; ok && strings.TrimSpace(v) != "" {
		effective.SyncDisplayName = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v, ok := settings[SettingKeyDingTalkConnectSyncDept]; ok && strings.TrimSpace(v) != "" {
		effective.SyncDept = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	// 身份同步三开关仅在 internal_only 模式下有意义；其它策略强制 false。
	if effective.CorpRestrictionPolicy != "internal_only" {
		effective.SyncCorpEmail = false
		effective.SyncDisplayName = false
		effective.SyncDept = false
	}

	// 身份同步目标 attr key（DB 空 → fallback 默认值）
	if v := strings.TrimSpace(settings[SettingKeyDingTalkConnectSyncCorpEmailAttrKey]); v != "" {
		effective.SyncCorpEmailAttrKey = v
	}
	if effective.SyncCorpEmailAttrKey == "" {
		effective.SyncCorpEmailAttrKey = "dingtalk_email"
	}
	if v := strings.TrimSpace(settings[SettingKeyDingTalkConnectSyncDisplayNameAttrKey]); v != "" {
		effective.SyncDisplayNameAttrKey = v
	}
	if effective.SyncDisplayNameAttrKey == "" {
		effective.SyncDisplayNameAttrKey = "dingtalk_name"
	}
	if v := strings.TrimSpace(settings[SettingKeyDingTalkConnectSyncDeptAttrKey]); v != "" {
		effective.SyncDeptAttrKey = v
	}
	if effective.SyncDeptAttrKey == "" {
		effective.SyncDeptAttrKey = "dingtalk_department"
	}

	if !effective.Enabled {
		return config.DingTalkConnectConfig{}, infraerrors.NotFound("OAUTH_DISABLED", "dingtalk oauth login is disabled")
	}

	// 基础健壮性校验（避免把用户重定向到一个必然失败或不安全的 OAuth 流程里）。
	if strings.TrimSpace(effective.ClientID) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth client id not configured")
	}
	if strings.TrimSpace(effective.AuthorizeURL) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth authorize url not configured")
	}
	if strings.TrimSpace(effective.TokenURL) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth token url not configured")
	}
	if strings.TrimSpace(effective.UserInfoURL) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth userinfo url not configured")
	}
	if strings.TrimSpace(effective.RedirectURL) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth redirect url not configured")
	}
	if strings.TrimSpace(effective.FrontendRedirectURL) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth frontend redirect url not configured")
	}

	if err := config.ValidateAbsoluteHTTPURL(effective.AuthorizeURL); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth authorize url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.TokenURL); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth token url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.UserInfoURL); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth userinfo url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.RedirectURL); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth redirect url invalid")
	}
	if err := config.ValidateFrontendRedirectURL(effective.FrontendRedirectURL); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth frontend redirect url invalid")
	}
	if strings.TrimSpace(effective.ClientSecret) == "" {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "dingtalk oauth client secret not configured")
	}

	// 镜像 admin handler 行为：internal_only policy 隐式要求 AppType=internal
	if effective.CorpRestrictionPolicy == "internal_only" {
		effective.AppType = "internal"
	}

	if err := config.ValidateDingTalkConfig(effective); err != nil {
		return config.DingTalkConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", err.Error())
	}

	return effective, nil
}

// GetWeChatConnectOAuthConfig 返回用于登录的最终生效 WeChat Connect 配置。
//
// WeChat Connect 已回归 DB 系统设置模型，不再回退到 config/env。
func (s *SettingService) GetWeChatConnectOAuthConfig(ctx context.Context) (WeChatConnectOAuthConfig, error) {
	keys := []string{
		SettingKeyWeChatConnectEnabled,
		SettingKeyWeChatConnectAppID,
		SettingKeyWeChatConnectAppSecret,
		SettingKeyWeChatConnectOpenAppID,
		SettingKeyWeChatConnectOpenAppSecret,
		SettingKeyWeChatConnectMPAppID,
		SettingKeyWeChatConnectMPAppSecret,
		SettingKeyWeChatConnectMobileAppID,
		SettingKeyWeChatConnectMobileAppSecret,
		SettingKeyWeChatConnectOpenEnabled,
		SettingKeyWeChatConnectMPEnabled,
		SettingKeyWeChatConnectMobileEnabled,
		SettingKeyWeChatConnectMode,
		SettingKeyWeChatConnectScopes,
		SettingKeyWeChatConnectRedirectURL,
		SettingKeyWeChatConnectFrontendRedirectURL,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return WeChatConnectOAuthConfig{}, fmt.Errorf("get wechat connect settings: %w", err)
	}
	return s.parseWeChatConnectOAuthConfig(settings)
}

// GetOverloadCooldownSettings 获取529过载冷却配置
func (s *SettingService) GetOverloadCooldownSettings(ctx context.Context) (*OverloadCooldownSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOverloadCooldownSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultOverloadCooldownSettings(), nil
		}
		return nil, fmt.Errorf("get overload cooldown settings: %w", err)
	}
	if value == "" {
		return DefaultOverloadCooldownSettings(), nil
	}

	var settings OverloadCooldownSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultOverloadCooldownSettings(), nil
	}

	// 修正配置值范围
	if settings.CooldownMinutes < 1 {
		settings.CooldownMinutes = 1
	}
	if settings.CooldownMinutes > 120 {
		settings.CooldownMinutes = 120
	}

	return &settings, nil
}

// SetOverloadCooldownSettings 设置529过载冷却配置
func (s *SettingService) SetOverloadCooldownSettings(ctx context.Context, settings *OverloadCooldownSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// 禁用时修正为合法值即可，不拒绝请求
	if settings.CooldownMinutes < 1 || settings.CooldownMinutes > 120 {
		if settings.Enabled {
			return fmt.Errorf("cooldown_minutes must be between 1-120")
		}
		settings.CooldownMinutes = 10 // 禁用状态下归一化为默认值
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal overload cooldown settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyOverloadCooldownSettings, string(data))
}

// GetRateLimit429CooldownSettings 获取429默认回避配置
func (s *SettingService) GetRateLimit429CooldownSettings(ctx context.Context) (*RateLimit429CooldownSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyRateLimit429CooldownSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultRateLimit429CooldownSettings(), nil
		}
		return nil, fmt.Errorf("get 429 cooldown settings: %w", err)
	}
	if value == "" {
		return DefaultRateLimit429CooldownSettings(), nil
	}

	var settings RateLimit429CooldownSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultRateLimit429CooldownSettings(), nil
	}

	if settings.CooldownSeconds < 1 {
		settings.CooldownSeconds = 1
	}
	if settings.CooldownSeconds > 7200 {
		settings.CooldownSeconds = 7200
	}

	return &settings, nil
}

// SetRateLimit429CooldownSettings 设置429默认回避配置
func (s *SettingService) SetRateLimit429CooldownSettings(ctx context.Context, settings *RateLimit429CooldownSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	if settings.CooldownSeconds < 1 || settings.CooldownSeconds > 7200 {
		if settings.Enabled {
			return fmt.Errorf("cooldown_seconds must be between 1-7200")
		}
		settings.CooldownSeconds = 5
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal 429 cooldown settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyRateLimit429CooldownSettings, string(data))
}

// GetOIDCConnectOAuthConfig 返回用于登录的“最终生效” OIDC 配置。
//
// 优先级：
// - 若对应系统设置键存在，则覆盖 config.yaml/env 的值
// - 否则回退到 config.yaml/env 的值
func (s *SettingService) GetOIDCConnectOAuthConfig(ctx context.Context) (config.OIDCConnectConfig, error) {
	if s == nil || s.cfg == nil {
		return config.OIDCConnectConfig{}, infraerrors.ServiceUnavailable("CONFIG_NOT_READY", "config not loaded")
	}

	effective := s.cfg.OIDC

	keys := []string{
		SettingKeyOIDCConnectEnabled,
		SettingKeyOIDCConnectProviderName,
		SettingKeyOIDCConnectClientID,
		SettingKeyOIDCConnectClientSecret,
		SettingKeyOIDCConnectIssuerURL,
		SettingKeyOIDCConnectDiscoveryURL,
		SettingKeyOIDCConnectAuthorizeURL,
		SettingKeyOIDCConnectTokenURL,
		SettingKeyOIDCConnectUserInfoURL,
		SettingKeyOIDCConnectJWKSURL,
		SettingKeyOIDCConnectScopes,
		SettingKeyOIDCConnectRedirectURL,
		SettingKeyOIDCConnectFrontendRedirectURL,
		SettingKeyOIDCConnectTokenAuthMethod,
		SettingKeyOIDCConnectUsePKCE,
		SettingKeyOIDCConnectValidateIDToken,
		SettingKeyOIDCConnectAllowedSigningAlgs,
		SettingKeyOIDCConnectClockSkewSeconds,
		SettingKeyOIDCConnectRequireEmailVerified,
		SettingKeyOIDCConnectUserInfoEmailPath,
		SettingKeyOIDCConnectUserInfoIDPath,
		SettingKeyOIDCConnectUserInfoUsernamePath,
	}
	settings, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return config.OIDCConnectConfig{}, fmt.Errorf("get oidc connect settings: %w", err)
	}

	if raw, ok := settings[SettingKeyOIDCConnectEnabled]; ok {
		effective.Enabled = raw == "true"
	}
	if v, ok := settings[SettingKeyOIDCConnectProviderName]; ok && strings.TrimSpace(v) != "" {
		effective.ProviderName = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectClientID]; ok && strings.TrimSpace(v) != "" {
		effective.ClientID = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectClientSecret]; ok && strings.TrimSpace(v) != "" {
		effective.ClientSecret = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectIssuerURL]; ok && strings.TrimSpace(v) != "" {
		effective.IssuerURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectDiscoveryURL]; ok && strings.TrimSpace(v) != "" {
		effective.DiscoveryURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectAuthorizeURL]; ok && strings.TrimSpace(v) != "" {
		effective.AuthorizeURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectTokenURL]; ok && strings.TrimSpace(v) != "" {
		effective.TokenURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectUserInfoURL]; ok && strings.TrimSpace(v) != "" {
		effective.UserInfoURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectJWKSURL]; ok && strings.TrimSpace(v) != "" {
		effective.JWKSURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectScopes]; ok && strings.TrimSpace(v) != "" {
		effective.Scopes = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectRedirectURL]; ok && strings.TrimSpace(v) != "" {
		effective.RedirectURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectFrontendRedirectURL]; ok && strings.TrimSpace(v) != "" {
		effective.FrontendRedirectURL = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectTokenAuthMethod]; ok && strings.TrimSpace(v) != "" {
		effective.TokenAuthMethod = strings.ToLower(strings.TrimSpace(v))
	}
	if raw, ok := settings[SettingKeyOIDCConnectUsePKCE]; ok {
		effective.UsePKCE = raw == "true"
	} else {
		effective.UsePKCE = oidcUsePKCECompatibilityDefault(effective)
	}
	if raw, ok := settings[SettingKeyOIDCConnectValidateIDToken]; ok {
		effective.ValidateIDToken = raw == "true"
	} else {
		effective.ValidateIDToken = oidcValidateIDTokenCompatibilityDefault(effective)
	}
	if v, ok := settings[SettingKeyOIDCConnectAllowedSigningAlgs]; ok && strings.TrimSpace(v) != "" {
		effective.AllowedSigningAlgs = strings.TrimSpace(v)
	}
	if raw, ok := settings[SettingKeyOIDCConnectClockSkewSeconds]; ok && strings.TrimSpace(raw) != "" {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(raw)); parseErr == nil {
			effective.ClockSkewSeconds = parsed
		}
	}
	if raw, ok := settings[SettingKeyOIDCConnectRequireEmailVerified]; ok {
		effective.RequireEmailVerified = raw == "true"
	}
	if v, ok := settings[SettingKeyOIDCConnectUserInfoEmailPath]; ok {
		effective.UserInfoEmailPath = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectUserInfoIDPath]; ok {
		effective.UserInfoIDPath = strings.TrimSpace(v)
	}
	if v, ok := settings[SettingKeyOIDCConnectUserInfoUsernamePath]; ok {
		effective.UserInfoUsernamePath = strings.TrimSpace(v)
	}

	if !effective.Enabled {
		return config.OIDCConnectConfig{}, infraerrors.NotFound("OAUTH_DISABLED", "oauth login is disabled")
	}
	if strings.TrimSpace(effective.ProviderName) == "" {
		effective.ProviderName = "OIDC"
	}
	if strings.TrimSpace(effective.ClientID) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth client id not configured")
	}
	if strings.TrimSpace(effective.IssuerURL) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth issuer url not configured")
	}
	if strings.TrimSpace(effective.RedirectURL) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth redirect url not configured")
	}
	if strings.TrimSpace(effective.FrontendRedirectURL) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth frontend redirect url not configured")
	}
	if !scopesContainOpenID(effective.Scopes) {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth scopes must contain openid")
	}
	if effective.ClockSkewSeconds < 0 || effective.ClockSkewSeconds > 600 {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth clock skew must be between 0 and 600")
	}

	if err := config.ValidateAbsoluteHTTPURL(effective.IssuerURL); err != nil {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth issuer url invalid")
	}

	discoveryURL := strings.TrimSpace(effective.DiscoveryURL)
	if discoveryURL == "" {
		discoveryURL = oidcDefaultDiscoveryURL(effective.IssuerURL)
		effective.DiscoveryURL = discoveryURL
	}
	if discoveryURL != "" {
		if err := config.ValidateAbsoluteHTTPURL(discoveryURL); err != nil {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth discovery url invalid")
		}
	}

	needsDiscovery := strings.TrimSpace(effective.AuthorizeURL) == "" ||
		strings.TrimSpace(effective.TokenURL) == "" ||
		(effective.ValidateIDToken && strings.TrimSpace(effective.JWKSURL) == "")
	if needsDiscovery && discoveryURL != "" {
		metadata, resolveErr := oidcResolveProviderMetadata(ctx, discoveryURL)
		if resolveErr != nil {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth discovery resolve failed").WithCause(resolveErr)
		}
		if strings.TrimSpace(effective.AuthorizeURL) == "" {
			effective.AuthorizeURL = strings.TrimSpace(metadata.AuthorizationEndpoint)
		}
		if strings.TrimSpace(effective.TokenURL) == "" {
			effective.TokenURL = strings.TrimSpace(metadata.TokenEndpoint)
		}
		if strings.TrimSpace(effective.UserInfoURL) == "" {
			effective.UserInfoURL = strings.TrimSpace(metadata.UserInfoEndpoint)
		}
		if strings.TrimSpace(effective.JWKSURL) == "" {
			effective.JWKSURL = strings.TrimSpace(metadata.JWKSURI)
		}
	}

	if strings.TrimSpace(effective.AuthorizeURL) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth authorize url not configured")
	}
	if strings.TrimSpace(effective.TokenURL) == "" {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token url not configured")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.AuthorizeURL); err != nil {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth authorize url invalid")
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.TokenURL); err != nil {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token url invalid")
	}
	if v := strings.TrimSpace(effective.UserInfoURL); v != "" {
		if err := config.ValidateAbsoluteHTTPURL(v); err != nil {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth userinfo url invalid")
		}
	}
	if effective.ValidateIDToken {
		if strings.TrimSpace(effective.JWKSURL) == "" {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth jwks url not configured")
		}
		if strings.TrimSpace(effective.AllowedSigningAlgs) == "" {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth signing algs not configured")
		}
	}
	if v := strings.TrimSpace(effective.JWKSURL); v != "" {
		if err := config.ValidateAbsoluteHTTPURL(v); err != nil {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth jwks url invalid")
		}
	}
	if err := config.ValidateAbsoluteHTTPURL(effective.RedirectURL); err != nil {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth redirect url invalid")
	}
	if err := config.ValidateFrontendRedirectURL(effective.FrontendRedirectURL); err != nil {
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth frontend redirect url invalid")
	}

	method := strings.ToLower(strings.TrimSpace(effective.TokenAuthMethod))
	switch method {
	case "", "client_secret_post", "client_secret_basic":
		if strings.TrimSpace(effective.ClientSecret) == "" {
			return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth client secret not configured")
		}
	case "none":
	default:
		return config.OIDCConnectConfig{}, infraerrors.InternalServer("OAUTH_CONFIG_INVALID", "oauth token_auth_method invalid")
	}

	return effective, nil
}

func scopesContainOpenID(scopes string) bool {
	for _, scope := range strings.Fields(strings.ToLower(strings.TrimSpace(scopes))) {
		if scope == "openid" {
			return true
		}
	}
	return false
}

type oidcProviderMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func oidcDefaultDiscoveryURL(issuerURL string) string {
	issuerURL = strings.TrimSpace(issuerURL)
	if issuerURL == "" {
		return ""
	}
	return strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"
}

func oidcResolveProviderMetadata(ctx context.Context, discoveryURL string) (*oidcProviderMetadata, error) {
	discoveryURL = strings.TrimSpace(discoveryURL)
	if discoveryURL == "" {
		return nil, fmt.Errorf("discovery url is empty")
	}

	resp, err := req.C().
		SetTimeout(15*time.Second).
		R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("request discovery document: %w", err)
	}
	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("discovery request failed: status=%d", resp.StatusCode)
	}

	metadata := &oidcProviderMetadata{}
	if err := json.Unmarshal(resp.Bytes(), metadata); err != nil {
		return nil, fmt.Errorf("parse discovery document: %w", err)
	}
	return metadata, nil
}
