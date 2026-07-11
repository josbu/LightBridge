package admin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/handler/dto"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

// semverPattern 预编译 semver 格式校验正则
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// menuItemIDPattern validates custom menu item IDs: alphanumeric, hyphens, underscores only.
var menuItemIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// generateMenuItemID generates a short random hex ID for a custom menu item.
func generateMenuItemID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate menu item ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func scopesContainOpenID(scopes string) bool {
	for _, scope := range strings.Fields(strings.ToLower(strings.TrimSpace(scopes))) {
		if scope == "openid" {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// SettingHandler 系统设置处理器
type SettingHandler struct {
	settingService           *service.SettingService
	emailService             *service.EmailService
	turnstileService         *service.TurnstileService
	opsService               *service.OpsService
	paymentConfigService     *service.PaymentConfigService
	paymentService           *service.PaymentService
	userAttributeService     *service.UserAttributeService
	notificationEmailService *service.NotificationEmailService
}

// NewSettingHandler 创建系统设置处理器
func NewSettingHandler(settingService *service.SettingService, emailService *service.EmailService, turnstileService *service.TurnstileService, opsService *service.OpsService, paymentConfigService *service.PaymentConfigService, paymentService *service.PaymentService, userAttributeService *service.UserAttributeService) *SettingHandler {
	return &SettingHandler{
		settingService:       settingService,
		emailService:         emailService,
		turnstileService:     turnstileService,
		opsService:           opsService,
		paymentConfigService: paymentConfigService,
		paymentService:       paymentService,
		userAttributeService: userAttributeService,
	}
}

// SetNotificationEmailService attaches the notification template service without changing
// the constructor signature used by existing unit tests.
func (h *SettingHandler) SetNotificationEmailService(notificationEmailService *service.NotificationEmailService) {
	h.notificationEmailService = notificationEmailService
}

// GetSettings 获取所有系统设置
// GET /api/v1/admin/settings
func (h *SettingHandler) GetSettings(c *gin.Context) {
	settings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	authSourceDefaults, err := h.settingService.GetAuthSourceDefaultSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Check if ops monitoring is enabled (respects config.ops.enabled)
	opsEnabled := h.opsService != nil && h.opsService.IsMonitoringEnabled(c.Request.Context())
	defaultSubscriptions := make([]dto.DefaultSubscriptionSetting, 0, len(settings.DefaultSubscriptions))
	for _, sub := range settings.DefaultSubscriptions {
		defaultSubscriptions = append(defaultSubscriptions, dto.DefaultSubscriptionSetting{
			GroupID:      sub.GroupID,
			ValidityDays: sub.ValidityDays,
		})
	}

	// Load payment config
	var paymentCfg *service.PaymentConfig
	if h.paymentConfigService != nil {
		paymentCfg, _ = h.paymentConfigService.GetPaymentConfig(c.Request.Context())
	}
	if paymentCfg == nil {
		paymentCfg = &service.PaymentConfig{}
	}

	payload := dto.SystemSettings{
		RegistrationEnabled:                    settings.RegistrationEnabled,
		EmailVerifyEnabled:                     settings.EmailVerifyEnabled,
		RegistrationEmailSuffixWhitelist:       settings.RegistrationEmailSuffixWhitelist,
		PromoCodeEnabled:                       settings.PromoCodeEnabled,
		PasswordResetEnabled:                   settings.PasswordResetEnabled,
		FrontendURL:                            settings.FrontendURL,
		InvitationCodeEnabled:                  settings.InvitationCodeEnabled,
		TotpEnabled:                            settings.TotpEnabled,
		TotpEncryptionKeyConfigured:            h.settingService.IsTotpEncryptionKeyConfigured(),
		LoginAgreementEnabled:                  settings.LoginAgreementEnabled,
		LoginAgreementMode:                     settings.LoginAgreementMode,
		LoginAgreementUpdatedAt:                settings.LoginAgreementUpdatedAt,
		LoginAgreementDocuments:                loginAgreementDocumentsToDTO(settings.LoginAgreementDocuments),
		SMTPHost:                               settings.SMTPHost,
		SMTPPort:                               settings.SMTPPort,
		SMTPUsername:                           settings.SMTPUsername,
		SMTPPasswordConfigured:                 settings.SMTPPasswordConfigured,
		SMTPFrom:                               settings.SMTPFrom,
		SMTPFromName:                           settings.SMTPFromName,
		SMTPUseTLS:                             settings.SMTPUseTLS,
		TurnstileEnabled:                       settings.TurnstileEnabled,
		TurnstileSiteKey:                       settings.TurnstileSiteKey,
		TurnstileSecretKeyConfigured:           settings.TurnstileSecretKeyConfigured,
		APIKeyACLTrustForwardedIP:              settings.APIKeyACLTrustForwardedIP,
		LinuxDoConnectEnabled:                  settings.LinuxDoConnectEnabled,
		LinuxDoConnectClientID:                 settings.LinuxDoConnectClientID,
		LinuxDoConnectClientSecretConfigured:   settings.LinuxDoConnectClientSecretConfigured,
		LinuxDoConnectRedirectURL:              settings.LinuxDoConnectRedirectURL,
		DingTalkConnectEnabled:                 settings.DingTalkConnectEnabled,
		DingTalkConnectClientID:                settings.DingTalkConnectClientID,
		DingTalkConnectClientSecretConfigured:  settings.DingTalkConnectClientSecretConfigured,
		DingTalkConnectRedirectURL:             settings.DingTalkConnectRedirectURL,
		DingTalkConnectCorpRestrictionPolicy:   settings.DingTalkConnectCorpRestrictionPolicy,
		DingTalkConnectInternalCorpID:          settings.DingTalkConnectInternalCorpID,
		DingTalkConnectBypassRegistration:      settings.DingTalkConnectBypassRegistration,
		DingTalkConnectSyncCorpEmail:           settings.DingTalkConnectSyncCorpEmail,
		DingTalkConnectSyncDisplayName:         settings.DingTalkConnectSyncDisplayName,
		DingTalkConnectSyncDept:                settings.DingTalkConnectSyncDept,
		DingTalkConnectSyncCorpEmailAttrKey:    settings.DingTalkConnectSyncCorpEmailAttrKey,
		DingTalkConnectSyncDisplayNameAttrKey:  settings.DingTalkConnectSyncDisplayNameAttrKey,
		DingTalkConnectSyncDeptAttrKey:         settings.DingTalkConnectSyncDeptAttrKey,
		DingTalkConnectSyncCorpEmailAttrName:   settings.DingTalkConnectSyncCorpEmailAttrName,
		DingTalkConnectSyncDisplayNameAttrName: settings.DingTalkConnectSyncDisplayNameAttrName,
		DingTalkConnectSyncDeptAttrName:        settings.DingTalkConnectSyncDeptAttrName,
		WeChatConnectEnabled:                   settings.WeChatConnectEnabled,
		WeChatConnectAppID:                     settings.WeChatConnectAppID,
		WeChatConnectAppSecretConfigured:       settings.WeChatConnectAppSecretConfigured,
		WeChatConnectOpenAppID:                 settings.WeChatConnectOpenAppID,
		WeChatConnectOpenAppSecretConfigured:   settings.WeChatConnectOpenAppSecretConfigured,
		WeChatConnectMPAppID:                   settings.WeChatConnectMPAppID,
		WeChatConnectMPAppSecretConfigured:     settings.WeChatConnectMPAppSecretConfigured,
		WeChatConnectMobileAppID:               settings.WeChatConnectMobileAppID,
		WeChatConnectMobileAppSecretConfigured: settings.WeChatConnectMobileAppSecretConfigured,
		WeChatConnectOpenEnabled:               settings.WeChatConnectOpenEnabled,
		WeChatConnectMPEnabled:                 settings.WeChatConnectMPEnabled,
		WeChatConnectMobileEnabled:             settings.WeChatConnectMobileEnabled,
		WeChatConnectMode:                      settings.WeChatConnectMode,
		WeChatConnectScopes:                    settings.WeChatConnectScopes,
		WeChatConnectRedirectURL:               settings.WeChatConnectRedirectURL,
		WeChatConnectFrontendRedirectURL:       settings.WeChatConnectFrontendRedirectURL,
		OIDCConnectEnabled:                     settings.OIDCConnectEnabled,
		OIDCConnectProviderName:                settings.OIDCConnectProviderName,
		OIDCConnectClientID:                    settings.OIDCConnectClientID,
		OIDCConnectClientSecretConfigured:      settings.OIDCConnectClientSecretConfigured,
		OIDCConnectIssuerURL:                   settings.OIDCConnectIssuerURL,
		OIDCConnectDiscoveryURL:                settings.OIDCConnectDiscoveryURL,
		OIDCConnectAuthorizeURL:                settings.OIDCConnectAuthorizeURL,
		OIDCConnectTokenURL:                    settings.OIDCConnectTokenURL,
		OIDCConnectUserInfoURL:                 settings.OIDCConnectUserInfoURL,
		OIDCConnectJWKSURL:                     settings.OIDCConnectJWKSURL,
		OIDCConnectScopes:                      settings.OIDCConnectScopes,
		OIDCConnectRedirectURL:                 settings.OIDCConnectRedirectURL,
		OIDCConnectFrontendRedirectURL:         settings.OIDCConnectFrontendRedirectURL,
		OIDCConnectTokenAuthMethod:             settings.OIDCConnectTokenAuthMethod,
		OIDCConnectUsePKCE:                     settings.OIDCConnectUsePKCE,
		OIDCConnectValidateIDToken:             settings.OIDCConnectValidateIDToken,
		OIDCConnectAllowedSigningAlgs:          settings.OIDCConnectAllowedSigningAlgs,
		OIDCConnectClockSkewSeconds:            settings.OIDCConnectClockSkewSeconds,
		OIDCConnectRequireEmailVerified:        settings.OIDCConnectRequireEmailVerified,
		OIDCConnectUserInfoEmailPath:           settings.OIDCConnectUserInfoEmailPath,
		OIDCConnectUserInfoIDPath:              settings.OIDCConnectUserInfoIDPath,
		OIDCConnectUserInfoUsernamePath:        settings.OIDCConnectUserInfoUsernamePath,
		GitHubOAuthEnabled:                     settings.GitHubOAuthEnabled,
		GitHubOAuthClientID:                    settings.GitHubOAuthClientID,
		GitHubOAuthClientSecretConfigured:      settings.GitHubOAuthClientSecretConfigured,
		GitHubOAuthRedirectURL:                 settings.GitHubOAuthRedirectURL,
		GitHubOAuthFrontendRedirectURL:         settings.GitHubOAuthFrontendRedirectURL,
		GoogleOAuthEnabled:                     settings.GoogleOAuthEnabled,
		GoogleOAuthClientID:                    settings.GoogleOAuthClientID,
		GoogleOAuthClientSecretConfigured:      settings.GoogleOAuthClientSecretConfigured,
		GoogleOAuthRedirectURL:                 settings.GoogleOAuthRedirectURL,
		GoogleOAuthFrontendRedirectURL:         settings.GoogleOAuthFrontendRedirectURL,
		SiteName:                               settings.SiteName,
		SiteLogo:                               settings.SiteLogo,
		SiteSubtitle:                           settings.SiteSubtitle,
		APIBaseURL:                             settings.APIBaseURL,
		ContactInfo:                            settings.ContactInfo,
		DocURL:                                 settings.DocURL,
		HomeContent:                            settings.HomeContent,
		HideCcsImportButton:                    settings.HideCcsImportButton,
		PurchaseSubscriptionEnabled:            settings.PurchaseSubscriptionEnabled,
		PurchaseSubscriptionURL:                settings.PurchaseSubscriptionURL,
		TableDefaultPageSize:                   settings.TableDefaultPageSize,
		TablePageSizeOptions:                   settings.TablePageSizeOptions,
		CustomMenuItems:                        dto.ParseCustomMenuItems(settings.CustomMenuItems),
		CustomEndpoints:                        dto.ParseCustomEndpoints(settings.CustomEndpoints),
		DefaultConcurrency:                     settings.DefaultConcurrency,
		DefaultBalance:                         settings.DefaultBalance,
		RiskControlEnabled:                     settings.RiskControlEnabled,
		PrivacyFilterEnabled:                   settings.PrivacyFilterEnabled,
		DeploymentMode:                         settings.DeploymentMode,
		AffiliateRebateRate:                    settings.AffiliateRebateRate,
		AffiliateRebateFreezeHours:             settings.AffiliateRebateFreezeHours,
		AffiliateRebateDurationDays:            settings.AffiliateRebateDurationDays,
		AffiliateRebatePerInviteeCap:           settings.AffiliateRebatePerInviteeCap,
		DefaultUserRPMLimit:                    settings.DefaultUserRPMLimit,
		DefaultSubscriptions:                   defaultSubscriptions,
		EnableModelFallback:                    settings.EnableModelFallback,
		FallbackModelAnthropic:                 settings.FallbackModelAnthropic,
		FallbackModelOpenAI:                    settings.FallbackModelOpenAI,
		FallbackModelGemini:                    settings.FallbackModelGemini,
		FallbackModelAntigravity:               settings.FallbackModelAntigravity,
		EnableIdentityPatch:                    settings.EnableIdentityPatch,
		IdentityPatchPrompt:                    settings.IdentityPatchPrompt,
		OpsMonitoringEnabled:                   opsEnabled && settings.OpsMonitoringEnabled,
		OpsRealtimeMonitoringEnabled:           settings.OpsRealtimeMonitoringEnabled,
		OpsQueryModeDefault:                    settings.OpsQueryModeDefault,
		OpsMetricsIntervalSeconds:              settings.OpsMetricsIntervalSeconds,
		MinClaudeCodeVersion:                   settings.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:                   settings.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling:            settings.AllowUngroupedKeyScheduling,
		BackendModeEnabled:                     settings.BackendModeEnabled,
		EnableFingerprintUnification:           settings.EnableFingerprintUnification,
		EnableMetadataPassthrough:              settings.EnableMetadataPassthrough,
		EnableCCHSigning:                       settings.EnableCCHSigning,
		EnableAnthropicCacheTTL1hInjection:     settings.EnableAnthropicCacheTTL1hInjection,
		RewriteMessageCacheControl:             settings.RewriteMessageCacheControl,
		AntigravityUserAgentVersion:            settings.AntigravityUserAgentVersion,
		OpenAICodexUserAgent:                   settings.OpenAICodexUserAgent,
		OpenAIAllowClaudeCodeCodexPlugin:       settings.OpenAIAllowClaudeCodeCodexPlugin,
		WebSearchEmulationEnabled:              settings.WebSearchEmulationEnabled,
		PaymentVisibleMethodAlipaySource:       settings.PaymentVisibleMethodAlipaySource,
		PaymentVisibleMethodWxpaySource:        settings.PaymentVisibleMethodWxpaySource,
		PaymentVisibleMethodAlipayEnabled:      settings.PaymentVisibleMethodAlipayEnabled,
		PaymentVisibleMethodWxpayEnabled:       settings.PaymentVisibleMethodWxpayEnabled,
		OpenAIAdvancedSchedulerEnabled:         settings.OpenAIAdvancedSchedulerEnabled,
		BalanceLowNotifyEnabled:                settings.BalanceLowNotifyEnabled,
		BalanceLowNotifyThreshold:              settings.BalanceLowNotifyThreshold,
		BalanceLowNotifyRechargeURL:            settings.BalanceLowNotifyRechargeURL,
		SubscriptionExpiryNotifyEnabled:        settings.SubscriptionExpiryNotifyEnabled,
		AccountQuotaNotifyEnabled:              settings.AccountQuotaNotifyEnabled,
		AccountQuotaNotifyEmails:               dto.NotifyEmailEntriesFromService(settings.AccountQuotaNotifyEmails),
		PaymentEnabled:                         paymentCfg.Enabled,
		PaymentMinAmount:                       paymentCfg.MinAmount,
		PaymentMaxAmount:                       paymentCfg.MaxAmount,
		PaymentDailyLimit:                      paymentCfg.DailyLimit,
		PaymentOrderTimeoutMin:                 paymentCfg.OrderTimeoutMin,
		PaymentMaxPendingOrders:                paymentCfg.MaxPendingOrders,
		PaymentEnabledTypes:                    paymentCfg.EnabledTypes,
		PaymentBalanceDisabled:                 paymentCfg.BalanceDisabled,
		PaymentBalanceRechargeMultiplier:       paymentCfg.BalanceRechargeMultiplier,
		PaymentRechargeFeeRate:                 paymentCfg.RechargeFeeRate,
		PaymentLoadBalanceStrat:                paymentCfg.LoadBalanceStrategy,
		PaymentProductNamePrefix:               paymentCfg.ProductNamePrefix,
		PaymentProductNameSuffix:               paymentCfg.ProductNameSuffix,
		PaymentHelpImageURL:                    paymentCfg.HelpImageURL,
		PaymentHelpText:                        paymentCfg.HelpText,
		PaymentCancelRateLimitEnabled:          paymentCfg.CancelRateLimitEnabled,
		PaymentCancelRateLimitMax:              paymentCfg.CancelRateLimitMax,
		PaymentCancelRateLimitWindow:           paymentCfg.CancelRateLimitWindow,
		PaymentCancelRateLimitUnit:             paymentCfg.CancelRateLimitUnit,
		PaymentCancelRateLimitMode:             paymentCfg.CancelRateLimitMode,
		PaymentAlipayForceQRCode:               paymentCfg.AlipayForceQRCode,

		ChannelMonitorEnabled:                settings.ChannelMonitorEnabled,
		ChannelMonitorDefaultIntervalSeconds: settings.ChannelMonitorDefaultIntervalSeconds,

		AvailableChannelsEnabled: settings.AvailableChannelsEnabled,

		AffiliateEnabled: settings.AffiliateEnabled,
	}

	// OpenAI fast policy (stored under a dedicated setting key)
	if fastPolicy, err := h.settingService.GetOpenAIFastPolicySettings(c.Request.Context()); err != nil {
		slog.Error("openai_fast_policy_settings_get_failed", "error", err)
	} else if fastPolicy != nil {
		payload.OpenAIFastPolicySettings = openaiFastPolicySettingsToDTO(fastPolicy)
	}

	// Default platform quotas（JSON map）
	if platformQuotas, err := h.settingService.GetDefaultPlatformQuotas(c.Request.Context()); err != nil {
		slog.Error("default_platform_quotas_get_failed", "error", err)
	} else {
		payload.DefaultPlatformQuotas = platformQuotas
	}

	response.Success(c, systemSettingsResponseData(payload, authSourceDefaults))
}

// openaiFastPolicySettingsToDTO converts service -> dto for OpenAI fast policy.
func openaiFastPolicySettingsToDTO(s *service.OpenAIFastPolicySettings) *dto.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]dto.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = dto.OpenAIFastPolicyRule(r)
	}
	return &dto.OpenAIFastPolicySettings{Rules: rules}
}

// openaiFastPolicySettingsFromDTO converts dto -> service for OpenAI fast policy.
//
// 规范化 ServiceTier：在 DTO 进入 service 层之前统一把空字符串归一为
// service.OpenAIFastTierAny ("all")，避免管理员保存时空串与 "all" 同时
// 表达"匹配任意 tier"造成数据库取值的二义性。其它非空值原样透传，由
// service.SetOpenAIFastPolicySettings 负责合法值校验。
func openaiFastPolicySettingsFromDTO(s *dto.OpenAIFastPolicySettings) *service.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]service.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = service.OpenAIFastPolicyRule(r)
		tier := strings.ToLower(strings.TrimSpace(rules[i].ServiceTier))
		if tier == "" {
			tier = service.OpenAIFastTierAny
		}
		rules[i].ServiceTier = tier
	}
	return &service.OpenAIFastPolicySettings{Rules: rules}
}

func loginAgreementDocumentsToDTO(items []service.LoginAgreementDocument) []dto.LoginAgreementDocument {
	result := make([]dto.LoginAgreementDocument, 0, len(items))
	for _, item := range items {
		result = append(result, dto.LoginAgreementDocument{
			ID:        item.ID,
			Title:     item.Title,
			ContentMD: item.ContentMD,
		})
	}
	return result
}

func loginAgreementDocumentsToService(items []dto.LoginAgreementDocument) []service.LoginAgreementDocument {
	result := make([]service.LoginAgreementDocument, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.ContentMD)
		if title == "" && content == "" {
			continue
		}
		result = append(result, service.LoginAgreementDocument{
			ID:        strings.TrimSpace(item.ID),
			Title:     title,
			ContentMD: content,
		})
	}
	return result
}

// UpdateSettingsRequest 更新设置请求
type UpdateSettingsRequest struct {
	// 注册设置
	RegistrationEnabled              *bool                        `json:"registration_enabled"`
	EmailVerifyEnabled               *bool                        `json:"email_verify_enabled"`
	RegistrationEmailSuffixWhitelist []string                     `json:"registration_email_suffix_whitelist"`
	PromoCodeEnabled                 *bool                        `json:"promo_code_enabled"`
	PasswordResetEnabled             *bool                        `json:"password_reset_enabled"`
	FrontendURL                      string                       `json:"frontend_url"`
	InvitationCodeEnabled            *bool                        `json:"invitation_code_enabled"`
	TotpEnabled                      *bool                        `json:"totp_enabled"` // TOTP 双因素认证
	LoginAgreementEnabled            *bool                        `json:"login_agreement_enabled"`
	LoginAgreementMode               string                       `json:"login_agreement_mode"`
	LoginAgreementUpdatedAt          string                       `json:"login_agreement_updated_at"`
	LoginAgreementDocuments          []dto.LoginAgreementDocument `json:"login_agreement_documents"`

	// 邮件服务设置
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	SMTPFrom     string `json:"smtp_from_email"`
	SMTPFromName string `json:"smtp_from_name"`
	SMTPUseTLS   bool   `json:"smtp_use_tls"`

	// Cloudflare Turnstile 设置
	TurnstileEnabled   bool   `json:"turnstile_enabled"`
	TurnstileSiteKey   string `json:"turnstile_site_key"`
	TurnstileSecretKey string `json:"turnstile_secret_key"`

	// API Key IP 访问控制设置
	APIKeyACLTrustForwardedIP *bool `json:"api_key_acl_trust_forwarded_ip"`

	// LinuxDo Connect OAuth 登录
	LinuxDoConnectEnabled      bool   `json:"linuxdo_connect_enabled"`
	LinuxDoConnectClientID     string `json:"linuxdo_connect_client_id"`
	LinuxDoConnectClientSecret string `json:"linuxdo_connect_client_secret"`
	LinuxDoConnectRedirectURL  string `json:"linuxdo_connect_redirect_url"`

	// DingTalk Connect OAuth 登录
	DingTalkConnectEnabled                 bool   `json:"dingtalk_connect_enabled"`
	DingTalkConnectClientID                string `json:"dingtalk_connect_client_id"`
	DingTalkConnectClientSecret            string `json:"dingtalk_connect_client_secret"`
	DingTalkConnectRedirectURL             string `json:"dingtalk_connect_redirect_url"`
	DingTalkConnectCorpRestrictionPolicy   string `json:"dingtalk_connect_corp_restriction_policy"`
	DingTalkConnectInternalCorpID          string `json:"dingtalk_connect_internal_corp_id"`
	DingTalkConnectBypassRegistration      bool   `json:"dingtalk_connect_bypass_registration"`
	DingTalkConnectSyncCorpEmail           bool   `json:"dingtalk_connect_sync_corp_email"`
	DingTalkConnectSyncDisplayName         bool   `json:"dingtalk_connect_sync_display_name"`
	DingTalkConnectSyncDept                bool   `json:"dingtalk_connect_sync_dept"`
	DingTalkConnectSyncCorpEmailAttrKey    string `json:"dingtalk_connect_sync_corp_email_attr_key"`
	DingTalkConnectSyncDisplayNameAttrKey  string `json:"dingtalk_connect_sync_display_name_attr_key"`
	DingTalkConnectSyncDeptAttrKey         string `json:"dingtalk_connect_sync_dept_attr_key"`
	DingTalkConnectSyncCorpEmailAttrName   string `json:"dingtalk_connect_sync_corp_email_attr_name"`
	DingTalkConnectSyncDisplayNameAttrName string `json:"dingtalk_connect_sync_display_name_attr_name"`
	DingTalkConnectSyncDeptAttrName        string `json:"dingtalk_connect_sync_dept_attr_name"`

	// WeChat Connect OAuth 登录
	WeChatConnectEnabled             bool   `json:"wechat_connect_enabled"`
	WeChatConnectAppID               string `json:"wechat_connect_app_id"`
	WeChatConnectAppSecret           string `json:"wechat_connect_app_secret"`
	WeChatConnectOpenAppID           string `json:"wechat_connect_open_app_id"`
	WeChatConnectOpenAppSecret       string `json:"wechat_connect_open_app_secret"`
	WeChatConnectMPAppID             string `json:"wechat_connect_mp_app_id"`
	WeChatConnectMPAppSecret         string `json:"wechat_connect_mp_app_secret"`
	WeChatConnectMobileAppID         string `json:"wechat_connect_mobile_app_id"`
	WeChatConnectMobileAppSecret     string `json:"wechat_connect_mobile_app_secret"`
	WeChatConnectOpenEnabled         bool   `json:"wechat_connect_open_enabled"`
	WeChatConnectMPEnabled           bool   `json:"wechat_connect_mp_enabled"`
	WeChatConnectMobileEnabled       bool   `json:"wechat_connect_mobile_enabled"`
	WeChatConnectMode                string `json:"wechat_connect_mode"`
	WeChatConnectScopes              string `json:"wechat_connect_scopes"`
	WeChatConnectRedirectURL         string `json:"wechat_connect_redirect_url"`
	WeChatConnectFrontendRedirectURL string `json:"wechat_connect_frontend_redirect_url"`

	// Generic OIDC OAuth 登录
	OIDCConnectEnabled              bool   `json:"oidc_connect_enabled"`
	OIDCConnectProviderName         string `json:"oidc_connect_provider_name"`
	OIDCConnectClientID             string `json:"oidc_connect_client_id"`
	OIDCConnectClientSecret         string `json:"oidc_connect_client_secret"`
	OIDCConnectIssuerURL            string `json:"oidc_connect_issuer_url"`
	OIDCConnectDiscoveryURL         string `json:"oidc_connect_discovery_url"`
	OIDCConnectAuthorizeURL         string `json:"oidc_connect_authorize_url"`
	OIDCConnectTokenURL             string `json:"oidc_connect_token_url"`
	OIDCConnectUserInfoURL          string `json:"oidc_connect_userinfo_url"`
	OIDCConnectJWKSURL              string `json:"oidc_connect_jwks_url"`
	OIDCConnectScopes               string `json:"oidc_connect_scopes"`
	OIDCConnectRedirectURL          string `json:"oidc_connect_redirect_url"`
	OIDCConnectFrontendRedirectURL  string `json:"oidc_connect_frontend_redirect_url"`
	OIDCConnectTokenAuthMethod      string `json:"oidc_connect_token_auth_method"`
	OIDCConnectUsePKCE              *bool  `json:"oidc_connect_use_pkce"`
	OIDCConnectValidateIDToken      *bool  `json:"oidc_connect_validate_id_token"`
	OIDCConnectAllowedSigningAlgs   string `json:"oidc_connect_allowed_signing_algs"`
	OIDCConnectClockSkewSeconds     int    `json:"oidc_connect_clock_skew_seconds"`
	OIDCConnectRequireEmailVerified bool   `json:"oidc_connect_require_email_verified"`
	OIDCConnectUserInfoEmailPath    string `json:"oidc_connect_userinfo_email_path"`
	OIDCConnectUserInfoIDPath       string `json:"oidc_connect_userinfo_id_path"`
	OIDCConnectUserInfoUsernamePath string `json:"oidc_connect_userinfo_username_path"`

	GitHubOAuthEnabled             bool   `json:"github_oauth_enabled"`
	GitHubOAuthClientID            string `json:"github_oauth_client_id"`
	GitHubOAuthClientSecret        string `json:"github_oauth_client_secret"`
	GitHubOAuthRedirectURL         string `json:"github_oauth_redirect_url"`
	GitHubOAuthFrontendRedirectURL string `json:"github_oauth_frontend_redirect_url"`
	GoogleOAuthEnabled             bool   `json:"google_oauth_enabled"`
	GoogleOAuthClientID            string `json:"google_oauth_client_id"`
	GoogleOAuthClientSecret        string `json:"google_oauth_client_secret"`
	GoogleOAuthRedirectURL         string `json:"google_oauth_redirect_url"`
	GoogleOAuthFrontendRedirectURL string `json:"google_oauth_frontend_redirect_url"`

	// OEM设置
	SiteName                    string                `json:"site_name"`
	SiteLogo                    string                `json:"site_logo"`
	SiteSubtitle                string                `json:"site_subtitle"`
	APIBaseURL                  string                `json:"api_base_url"`
	ContactInfo                 string                `json:"contact_info"`
	DocURL                      string                `json:"doc_url"`
	HomeContent                 string                `json:"home_content"`
	HideCcsImportButton         bool                  `json:"hide_ccs_import_button"`
	PurchaseSubscriptionEnabled *bool                 `json:"purchase_subscription_enabled"`
	PurchaseSubscriptionURL     *string               `json:"purchase_subscription_url"`
	TableDefaultPageSize        int                   `json:"table_default_page_size"`
	TablePageSizeOptions        []int                 `json:"table_page_size_options"`
	CustomMenuItems             *[]dto.CustomMenuItem `json:"custom_menu_items"`
	CustomEndpoints             *[]dto.CustomEndpoint `json:"custom_endpoints"`

	// 默认配置
	DefaultConcurrency                        int                               `json:"default_concurrency"`
	DefaultBalance                            float64                           `json:"default_balance"`
	AffiliateRebateRate                       *float64                          `json:"affiliate_rebate_rate"`
	AffiliateRebateFreezeHours                *int                              `json:"affiliate_rebate_freeze_hours"`
	AffiliateRebateDurationDays               *int                              `json:"affiliate_rebate_duration_days"`
	AffiliateRebatePerInviteeCap              *float64                          `json:"affiliate_rebate_per_invitee_cap"`
	DefaultUserRPMLimit                       int                               `json:"default_user_rpm_limit"`
	DefaultSubscriptions                      []dto.DefaultSubscriptionSetting  `json:"default_subscriptions"`
	AuthSourceDefaultEmailBalance             *float64                          `json:"auth_source_default_email_balance"`
	AuthSourceDefaultEmailConcurrency         *int                              `json:"auth_source_default_email_concurrency"`
	AuthSourceDefaultEmailSubscriptions       *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_email_subscriptions"`
	AuthSourceDefaultEmailGrantOnSignup       *bool                             `json:"auth_source_default_email_grant_on_signup"`
	AuthSourceDefaultEmailGrantOnFirstBind    *bool                             `json:"auth_source_default_email_grant_on_first_bind"`
	AuthSourceDefaultLinuxDoBalance           *float64                          `json:"auth_source_default_linuxdo_balance"`
	AuthSourceDefaultLinuxDoConcurrency       *int                              `json:"auth_source_default_linuxdo_concurrency"`
	AuthSourceDefaultLinuxDoSubscriptions     *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_linuxdo_subscriptions"`
	AuthSourceDefaultLinuxDoGrantOnSignup     *bool                             `json:"auth_source_default_linuxdo_grant_on_signup"`
	AuthSourceDefaultLinuxDoGrantOnFirstBind  *bool                             `json:"auth_source_default_linuxdo_grant_on_first_bind"`
	AuthSourceDefaultOIDCBalance              *float64                          `json:"auth_source_default_oidc_balance"`
	AuthSourceDefaultOIDCConcurrency          *int                              `json:"auth_source_default_oidc_concurrency"`
	AuthSourceDefaultOIDCSubscriptions        *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_oidc_subscriptions"`
	AuthSourceDefaultOIDCGrantOnSignup        *bool                             `json:"auth_source_default_oidc_grant_on_signup"`
	AuthSourceDefaultOIDCGrantOnFirstBind     *bool                             `json:"auth_source_default_oidc_grant_on_first_bind"`
	AuthSourceDefaultWeChatBalance            *float64                          `json:"auth_source_default_wechat_balance"`
	AuthSourceDefaultWeChatConcurrency        *int                              `json:"auth_source_default_wechat_concurrency"`
	AuthSourceDefaultWeChatSubscriptions      *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_wechat_subscriptions"`
	AuthSourceDefaultWeChatGrantOnSignup      *bool                             `json:"auth_source_default_wechat_grant_on_signup"`
	AuthSourceDefaultWeChatGrantOnFirstBind   *bool                             `json:"auth_source_default_wechat_grant_on_first_bind"`
	AuthSourceDefaultGitHubBalance            *float64                          `json:"auth_source_default_github_balance"`
	AuthSourceDefaultGitHubConcurrency        *int                              `json:"auth_source_default_github_concurrency"`
	AuthSourceDefaultGitHubSubscriptions      *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_github_subscriptions"`
	AuthSourceDefaultGitHubGrantOnSignup      *bool                             `json:"auth_source_default_github_grant_on_signup"`
	AuthSourceDefaultGitHubGrantOnFirstBind   *bool                             `json:"auth_source_default_github_grant_on_first_bind"`
	AuthSourceDefaultGoogleBalance            *float64                          `json:"auth_source_default_google_balance"`
	AuthSourceDefaultGoogleConcurrency        *int                              `json:"auth_source_default_google_concurrency"`
	AuthSourceDefaultGoogleSubscriptions      *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_google_subscriptions"`
	AuthSourceDefaultGoogleGrantOnSignup      *bool                             `json:"auth_source_default_google_grant_on_signup"`
	AuthSourceDefaultGoogleGrantOnFirstBind   *bool                             `json:"auth_source_default_google_grant_on_first_bind"`
	AuthSourceDefaultDingTalkBalance          *float64                          `json:"auth_source_default_dingtalk_balance"`
	AuthSourceDefaultDingTalkConcurrency      *int                              `json:"auth_source_default_dingtalk_concurrency"`
	AuthSourceDefaultDingTalkSubscriptions    *[]dto.DefaultSubscriptionSetting `json:"auth_source_default_dingtalk_subscriptions"`
	AuthSourceDefaultDingTalkGrantOnSignup    *bool                             `json:"auth_source_default_dingtalk_grant_on_signup"`
	AuthSourceDefaultDingTalkGrantOnFirstBind *bool                             `json:"auth_source_default_dingtalk_grant_on_first_bind"`
	ForceEmailOnThirdPartySignup              *bool                             `json:"force_email_on_third_party_signup"`

	// Model fallback configuration
	EnableModelFallback      bool   `json:"enable_model_fallback"`
	FallbackModelAnthropic   string `json:"fallback_model_anthropic"`
	FallbackModelOpenAI      string `json:"fallback_model_openai"`
	FallbackModelGemini      string `json:"fallback_model_gemini"`
	FallbackModelAntigravity string `json:"fallback_model_antigravity"`

	// Identity patch configuration (Claude -> Gemini)
	EnableIdentityPatch bool   `json:"enable_identity_patch"`
	IdentityPatchPrompt string `json:"identity_patch_prompt"`

	// Ops monitoring (vNext)
	OpsMonitoringEnabled         *bool   `json:"ops_monitoring_enabled"`
	OpsRealtimeMonitoringEnabled *bool   `json:"ops_realtime_monitoring_enabled"`
	OpsQueryModeDefault          *string `json:"ops_query_mode_default"`
	OpsMetricsIntervalSeconds    *int    `json:"ops_metrics_interval_seconds"`

	MinClaudeCodeVersion string `json:"min_claude_code_version"`
	MaxClaudeCodeVersion string `json:"max_claude_code_version"`

	// 分组隔离
	AllowUngroupedKeyScheduling bool `json:"allow_ungrouped_key_scheduling"`

	// Backend Mode
	BackendModeEnabled bool `json:"backend_mode_enabled"`

	// Gateway forwarding behavior
	EnableFingerprintUnification       *bool   `json:"enable_fingerprint_unification"`
	EnableMetadataPassthrough          *bool   `json:"enable_metadata_passthrough"`
	EnableCCHSigning                   *bool   `json:"enable_cch_signing"`
	EnableAnthropicCacheTTL1hInjection *bool   `json:"enable_anthropic_cache_ttl_1h_injection"`
	RewriteMessageCacheControl         *bool   `json:"rewrite_message_cache_control"`
	AntigravityUserAgentVersion        *string `json:"antigravity_user_agent_version"`
	OpenAICodexUserAgent               *string `json:"openai_codex_user_agent"`
	OpenAIAllowClaudeCodeCodexPlugin   *bool   `json:"openai_allow_claude_code_codex_plugin"`

	// Payment visible method routing
	PaymentVisibleMethodAlipaySource  *string `json:"payment_visible_method_alipay_source"`
	PaymentVisibleMethodWxpaySource   *string `json:"payment_visible_method_wxpay_source"`
	PaymentVisibleMethodAlipayEnabled *bool   `json:"payment_visible_method_alipay_enabled"`
	PaymentVisibleMethodWxpayEnabled  *bool   `json:"payment_visible_method_wxpay_enabled"`

	// OpenAI account scheduling
	OpenAIAdvancedSchedulerEnabled *bool `json:"openai_advanced_scheduler_enabled"`

	// 余额不足提醒
	BalanceLowNotifyEnabled         *bool                   `json:"balance_low_notify_enabled"`
	BalanceLowNotifyThreshold       *float64                `json:"balance_low_notify_threshold"`
	BalanceLowNotifyRechargeURL     *string                 `json:"balance_low_notify_recharge_url"`
	SubscriptionExpiryNotifyEnabled *bool                   `json:"subscription_expiry_notify_enabled"`
	AccountQuotaNotifyEnabled       *bool                   `json:"account_quota_notify_enabled"`
	AccountQuotaNotifyEmails        *[]dto.NotifyEmailEntry `json:"account_quota_notify_emails"`

	// Payment configuration (integrated into settings, full replace)
	PaymentEnabled                   *bool    `json:"payment_enabled"`
	PaymentMinAmount                 *float64 `json:"payment_min_amount"`
	PaymentMaxAmount                 *float64 `json:"payment_max_amount"`
	PaymentDailyLimit                *float64 `json:"payment_daily_limit"`
	PaymentOrderTimeoutMin           *int     `json:"payment_order_timeout_minutes"`
	PaymentMaxPendingOrders          *int     `json:"payment_max_pending_orders"`
	PaymentEnabledTypes              []string `json:"payment_enabled_types"`
	PaymentBalanceDisabled           *bool    `json:"payment_balance_disabled"`
	PaymentBalanceRechargeMultiplier *float64 `json:"payment_balance_recharge_multiplier"`
	PaymentRechargeFeeRate           *float64 `json:"payment_recharge_fee_rate"`
	PaymentLoadBalanceStrat          *string  `json:"payment_load_balance_strategy"`
	PaymentProductNamePrefix         *string  `json:"payment_product_name_prefix"`
	PaymentProductNameSuffix         *string  `json:"payment_product_name_suffix"`
	PaymentHelpImageURL              *string  `json:"payment_help_image_url"`
	PaymentHelpText                  *string  `json:"payment_help_text"`

	// Cancel rate limit
	PaymentCancelRateLimitEnabled *bool   `json:"payment_cancel_rate_limit_enabled"`
	PaymentCancelRateLimitMax     *int    `json:"payment_cancel_rate_limit_max"`
	PaymentCancelRateLimitWindow  *int    `json:"payment_cancel_rate_limit_window"`
	PaymentCancelRateLimitUnit    *string `json:"payment_cancel_rate_limit_unit"`
	PaymentCancelRateLimitMode    *string `json:"payment_cancel_rate_limit_window_mode"`

	// Force Alipay mobile clients to use QR code payment instead of mobile redirect
	PaymentAlipayForceQRCode *bool `json:"payment_alipay_force_qrcode"`

	// Channel Monitor feature switch
	ChannelMonitorEnabled                *bool `json:"channel_monitor_enabled"`
	ChannelMonitorDefaultIntervalSeconds *int  `json:"channel_monitor_default_interval_seconds"`

	// Available Channels feature switch (user-facing)
	AvailableChannelsEnabled *bool `json:"available_channels_enabled"`

	// Affiliate (邀请返利) feature switch
	AffiliateEnabled *bool `json:"affiliate_enabled"`

	// 风控中心功能开关
	RiskControlEnabled *bool `json:"risk_control_enabled"`

	// 隐私过滤功能开关
	PrivacyFilterEnabled *bool `json:"privacy_filter_enabled"`

	// 部署模式：personal（个人）/ distribution（分发）
	DeploymentMode *string `json:"deployment_mode"`

	// 公告功能开关
	AnnouncementsEnabled *bool `json:"announcements_enabled"`

	// 兑换码功能开关
	RedeemEnabled *bool `json:"redeem_enabled"`

	// 优惠码功能开关
	PromoEnabled *bool `json:"promo_enabled"`

	// OpenAI fast/flex policy (optional, only updated when provided)
	OpenAIFastPolicySettings *dto.OpenAIFastPolicySettings `json:"openai_fast_policy_settings,omitempty"`

	// 系统全局 platform quota 默认值（整体替换语义：nil = 不修改，non-nil = 整体覆盖）。
	DefaultPlatformQuotas map[string]*service.DefaultPlatformQuotaSetting `json:"default_platform_quotas"`

	// auth-source 层 platform quota 覆盖（override 语义：nil = 不修改，non-nil = 整体覆盖该 source 的 quota 配置）。
	AuthSourceEmailPlatformQuotas    map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_email_platform_quotas"`
	AuthSourceLinuxDoPlatformQuotas  map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_linuxdo_platform_quotas"`
	AuthSourceOIDCPlatformQuotas     map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_oidc_platform_quotas"`
	AuthSourceWeChatPlatformQuotas   map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_wechat_platform_quotas"`
	AuthSourceGitHubPlatformQuotas   map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_github_platform_quotas"`
	AuthSourceGooglePlatformQuotas   map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_google_platform_quotas"`
	AuthSourceDingTalkPlatformQuotas map[string]*service.DefaultPlatformQuotaSetting `json:"auth_source_default_dingtalk_platform_quotas"`
}
