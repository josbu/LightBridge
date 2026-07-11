package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/handler/dto"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

// UpdateSettings 更新系统设置
// PUT /api/v1/admin/settings
func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	previousSettings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	previousAuthSourceDefaults, err := h.settingService.GetAuthSourceDefaultSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证参数
	if req.DefaultConcurrency < 1 {
		req.DefaultConcurrency = 1
	}
	if req.DefaultBalance < 0 {
		req.DefaultBalance = 0
	}
	affiliateRebateRate := previousSettings.AffiliateRebateRate
	if req.AffiliateRebateRate != nil {
		affiliateRebateRate = *req.AffiliateRebateRate
	}
	if affiliateRebateRate < service.AffiliateRebateRateMin {
		affiliateRebateRate = service.AffiliateRebateRateMin
	}
	if affiliateRebateRate > service.AffiliateRebateRateMax {
		affiliateRebateRate = service.AffiliateRebateRateMax
	}
	affiliateRebateFreezeHours := previousSettings.AffiliateRebateFreezeHours
	if req.AffiliateRebateFreezeHours != nil {
		affiliateRebateFreezeHours = *req.AffiliateRebateFreezeHours
	}
	if affiliateRebateFreezeHours < 0 {
		affiliateRebateFreezeHours = service.AffiliateRebateFreezeHoursDefault
	}
	if affiliateRebateFreezeHours > service.AffiliateRebateFreezeHoursMax {
		affiliateRebateFreezeHours = service.AffiliateRebateFreezeHoursMax
	}
	affiliateRebateDurationDays := previousSettings.AffiliateRebateDurationDays
	if req.AffiliateRebateDurationDays != nil {
		affiliateRebateDurationDays = *req.AffiliateRebateDurationDays
	}
	if affiliateRebateDurationDays < 0 {
		affiliateRebateDurationDays = service.AffiliateRebateDurationDaysDefault
	}
	if affiliateRebateDurationDays > service.AffiliateRebateDurationDaysMax {
		affiliateRebateDurationDays = service.AffiliateRebateDurationDaysMax
	}
	affiliateRebatePerInviteeCap := previousSettings.AffiliateRebatePerInviteeCap
	if req.AffiliateRebatePerInviteeCap != nil {
		affiliateRebatePerInviteeCap = *req.AffiliateRebatePerInviteeCap
	}
	if affiliateRebatePerInviteeCap < 0 {
		affiliateRebatePerInviteeCap = service.AffiliateRebatePerInviteeCapDefault
	}
	// 通用表格配置：兼容旧客户端未传字段时保留当前值。
	if req.TableDefaultPageSize <= 0 {
		req.TableDefaultPageSize = previousSettings.TableDefaultPageSize
	}
	if req.TablePageSizeOptions == nil {
		req.TablePageSizeOptions = previousSettings.TablePageSizeOptions
	}
	req.SMTPHost = strings.TrimSpace(req.SMTPHost)
	req.SMTPUsername = strings.TrimSpace(req.SMTPUsername)
	req.SMTPPassword = strings.TrimSpace(req.SMTPPassword)
	req.SMTPFrom = strings.TrimSpace(req.SMTPFrom)
	req.SMTPFromName = strings.TrimSpace(req.SMTPFromName)
	if req.SMTPPort <= 0 {
		req.SMTPPort = 587
	}
	req.DefaultSubscriptions = normalizeDefaultSubscriptions(req.DefaultSubscriptions)
	req.AuthSourceDefaultEmailSubscriptions = normalizeOptionalDefaultSubscriptions(req.AuthSourceDefaultEmailSubscriptions)
	req.AuthSourceDefaultLinuxDoSubscriptions = normalizeOptionalDefaultSubscriptions(req.AuthSourceDefaultLinuxDoSubscriptions)
	req.AuthSourceDefaultOIDCSubscriptions = normalizeOptionalDefaultSubscriptions(req.AuthSourceDefaultOIDCSubscriptions)
	req.AuthSourceDefaultWeChatSubscriptions = normalizeOptionalDefaultSubscriptions(req.AuthSourceDefaultWeChatSubscriptions)
	req.AuthSourceDefaultDingTalkSubscriptions = normalizeOptionalDefaultSubscriptions(req.AuthSourceDefaultDingTalkSubscriptions)

	// SMTP 配置保护：如果请求中 smtp_host 为空但数据库中已有配置，则保留已有 SMTP 配置
	// 防止前端加载设置失败时空表单覆盖已保存的 SMTP 配置
	if req.SMTPHost == "" && previousSettings.SMTPHost != "" {
		req.SMTPHost = previousSettings.SMTPHost
		req.SMTPPort = previousSettings.SMTPPort
		req.SMTPUsername = previousSettings.SMTPUsername
		req.SMTPFrom = previousSettings.SMTPFrom
		req.SMTPFromName = previousSettings.SMTPFromName
		req.SMTPUseTLS = previousSettings.SMTPUseTLS
	}

	// Turnstile 参数验证
	if req.TurnstileEnabled {
		// 检查必填字段
		if req.TurnstileSiteKey == "" {
			response.BadRequest(c, "Turnstile Site Key is required when enabled")
			return
		}
		// 如果未提供 secret key，使用已保存的值（留空保留当前值）
		if req.TurnstileSecretKey == "" {
			if previousSettings.TurnstileSecretKey == "" {
				response.BadRequest(c, "Turnstile Secret Key is required when enabled")
				return
			}
			req.TurnstileSecretKey = previousSettings.TurnstileSecretKey
		}

		// 当 site_key 或 secret_key 任一变化时验证（避免配置错误导致无法登录）
		siteKeyChanged := previousSettings.TurnstileSiteKey != req.TurnstileSiteKey
		secretKeyChanged := previousSettings.TurnstileSecretKey != req.TurnstileSecretKey
		if siteKeyChanged || secretKeyChanged {
			if err := h.turnstileService.ValidateSecretKey(c.Request.Context(), req.TurnstileSecretKey); err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
	}

	// TOTP 双因素认证参数验证
	// 只有手动配置了加密密钥才允许启用 TOTP 功能
	if req.TotpEnabled != nil && *req.TotpEnabled && !previousSettings.TotpEnabled {
		// 尝试启用 TOTP，检查加密密钥是否已手动配置
		if !h.settingService.IsTotpEncryptionKeyConfigured() {
			response.BadRequest(c, "Cannot enable TOTP: TOTP_ENCRYPTION_KEY environment variable must be configured first. Generate a key with 'openssl rand -hex 32' and set it in your environment.")
			return
		}
	}
	loginAgreementMode := strings.ToLower(strings.TrimSpace(req.LoginAgreementMode))
	if loginAgreementMode == "" {
		loginAgreementMode = strings.ToLower(strings.TrimSpace(previousSettings.LoginAgreementMode))
	}
	switch loginAgreementMode {
	case "", "modal":
		loginAgreementMode = "modal"
	case "checkbox":
	default:
		response.BadRequest(c, "Login agreement mode must be modal or checkbox")
		return
	}
	loginAgreementUpdatedAt := strings.TrimSpace(req.LoginAgreementUpdatedAt)
	if loginAgreementUpdatedAt == "" {
		loginAgreementUpdatedAt = strings.TrimSpace(previousSettings.LoginAgreementUpdatedAt)
	}
	loginAgreementDocuments := loginAgreementDocumentsToService(req.LoginAgreementDocuments)
	if len(loginAgreementDocuments) == 0 {
		loginAgreementDocuments = previousSettings.LoginAgreementDocuments
	}
	for _, doc := range loginAgreementDocuments {
		if strings.TrimSpace(doc.Title) == "" {
			response.BadRequest(c, "Login agreement document title is required")
			return
		}
		if len(doc.Title) > 80 {
			response.BadRequest(c, "Login agreement document title is too long (max 80 characters)")
			return
		}
		if len(doc.ContentMD) > 200*1024 {
			response.BadRequest(c, "Login agreement document content is too large (max 200KB)")
			return
		}
	}
	if req.LoginAgreementEnabled != nil && *req.LoginAgreementEnabled && len(loginAgreementDocuments) == 0 {
		response.BadRequest(c, "Login agreement documents are required when enabled")
		return
	}

	// LinuxDo Connect 参数验证
	if req.LinuxDoConnectEnabled {
		req.LinuxDoConnectClientID = strings.TrimSpace(req.LinuxDoConnectClientID)
		req.LinuxDoConnectClientSecret = strings.TrimSpace(req.LinuxDoConnectClientSecret)
		req.LinuxDoConnectRedirectURL = strings.TrimSpace(req.LinuxDoConnectRedirectURL)

		if req.LinuxDoConnectClientID == "" {
			response.BadRequest(c, "LinuxDo Client ID is required when enabled")
			return
		}
		if req.LinuxDoConnectRedirectURL == "" {
			response.BadRequest(c, "LinuxDo Redirect URL is required when enabled")
			return
		}
		if err := config.ValidateAbsoluteHTTPURL(req.LinuxDoConnectRedirectURL); err != nil {
			response.BadRequest(c, "LinuxDo Redirect URL must be an absolute http(s) URL")
			return
		}

		// 如果未提供 client_secret，则保留现有值（如有）。
		if req.LinuxDoConnectClientSecret == "" {
			if previousSettings.LinuxDoConnectClientSecret == "" {
				response.BadRequest(c, "LinuxDo Client Secret is required when enabled")
				return
			}
			req.LinuxDoConnectClientSecret = previousSettings.LinuxDoConnectClientSecret
		}
	}

	// DingTalk Connect 参数验证
	// 防御性：任何写入路径上把已废弃的 corp_restriction_policy=whitelist 入参 coerce 为 none，
	// 避免任何直连 admin API 的客户端把死值写回 DB（前端 UI 已无此选项）。
	req.DingTalkConnectCorpRestrictionPolicy = service.CoerceDingTalkCorpPolicyForWrite(req.DingTalkConnectCorpRestrictionPolicy)

	if req.DingTalkConnectEnabled {
		req.DingTalkConnectClientID = strings.TrimSpace(req.DingTalkConnectClientID)
		req.DingTalkConnectClientSecret = strings.TrimSpace(req.DingTalkConnectClientSecret)
		req.DingTalkConnectRedirectURL = strings.TrimSpace(req.DingTalkConnectRedirectURL)
		req.DingTalkConnectCorpRestrictionPolicy = strings.TrimSpace(req.DingTalkConnectCorpRestrictionPolicy)
		req.DingTalkConnectInternalCorpID = strings.TrimSpace(req.DingTalkConnectInternalCorpID)

		if req.DingTalkConnectClientID == "" {
			response.BadRequest(c, "DingTalk Client ID is required when enabled")
			return
		}
		if req.DingTalkConnectRedirectURL == "" {
			response.BadRequest(c, "DingTalk Redirect URL is required when enabled")
			return
		}
		if err := config.ValidateAbsoluteHTTPURL(req.DingTalkConnectRedirectURL); err != nil {
			response.BadRequest(c, "DingTalk Redirect URL must be an absolute http(s) URL")
			return
		}

		// 如果未提供 client_secret，则保留现有值（如有）。
		if req.DingTalkConnectClientSecret == "" {
			if previousSettings.DingTalkConnectClientSecret == "" {
				response.BadRequest(c, "DingTalk Client Secret is required when enabled")
				return
			}
			req.DingTalkConnectClientSecret = previousSettings.DingTalkConnectClientSecret
		}

		// Corp 策略校验（V1/V4 fail-closed）
		dingTalkCfg := config.DingTalkConnectConfig{
			Enabled:               true,
			DingTalkAppKind:       "internal_app", // 硬编码：settings 层仅支持 internal_app
			AppType:               "internal",     // 对于 internal_only 策略的默认值
			CorpRestrictionPolicy: req.DingTalkConnectCorpRestrictionPolicy,
			InternalCorpID:        req.DingTalkConnectInternalCorpID,
		}
		// 若未填 corp_restriction_policy，保留已有配置
		if dingTalkCfg.CorpRestrictionPolicy == "" {
			dingTalkCfg.CorpRestrictionPolicy = previousSettings.DingTalkConnectCorpRestrictionPolicy
		}
		// 对于 internal_only 策略，app_type 必须为 internal（V1 校验）
		if dingTalkCfg.CorpRestrictionPolicy == "internal_only" {
			dingTalkCfg.AppType = "internal"
		} else {
			dingTalkCfg.AppType = "public"
		}
		if err := config.ValidateDingTalkConfig(dingTalkCfg); err != nil {
			response.ErrorWithDetails(c, http.StatusBadRequest, err.Error(), mapDingTalkValidateError(err), nil)
			return
		}

		// bypass_registration 仅在 internal_only 模式下有意义；其它策略下强制为 false，
		// 防止 admin 在切换 policy 时把 bypass 残留在 DB 中（前端 UI 也已隐藏该开关）。
		if dingTalkCfg.CorpRestrictionPolicy != "internal_only" {
			req.DingTalkConnectBypassRegistration = false
			// 身份同步三开关同理：仅 internal_only 模式下有意义，其它策略强制 false。
			req.DingTalkConnectSyncCorpEmail = false
			req.DingTalkConnectSyncDisplayName = false
			req.DingTalkConnectSyncDept = false
		}
		// 身份同步目标 attr key：trimSpace + 空值 fallback 到默认值
		req.DingTalkConnectSyncCorpEmailAttrKey = strings.TrimSpace(req.DingTalkConnectSyncCorpEmailAttrKey)
		if req.DingTalkConnectSyncCorpEmailAttrKey == "" {
			req.DingTalkConnectSyncCorpEmailAttrKey = "dingtalk_email"
		}
		req.DingTalkConnectSyncDisplayNameAttrKey = strings.TrimSpace(req.DingTalkConnectSyncDisplayNameAttrKey)
		if req.DingTalkConnectSyncDisplayNameAttrKey == "" {
			req.DingTalkConnectSyncDisplayNameAttrKey = "dingtalk_name"
		}
		req.DingTalkConnectSyncDeptAttrKey = strings.TrimSpace(req.DingTalkConnectSyncDeptAttrKey)
		if req.DingTalkConnectSyncDeptAttrKey == "" {
			req.DingTalkConnectSyncDeptAttrKey = "dingtalk_department"
		}
		// 身份同步目标 attr 显示名称：trim + 空值 fallback 到默认中文名
		req.DingTalkConnectSyncCorpEmailAttrName = strings.TrimSpace(req.DingTalkConnectSyncCorpEmailAttrName)
		if req.DingTalkConnectSyncCorpEmailAttrName == "" {
			req.DingTalkConnectSyncCorpEmailAttrName = "钉钉企业邮箱"
		}
		req.DingTalkConnectSyncDisplayNameAttrName = strings.TrimSpace(req.DingTalkConnectSyncDisplayNameAttrName)
		if req.DingTalkConnectSyncDisplayNameAttrName == "" {
			req.DingTalkConnectSyncDisplayNameAttrName = "钉钉姓名"
		}
		req.DingTalkConnectSyncDeptAttrName = strings.TrimSpace(req.DingTalkConnectSyncDeptAttrName)
		if req.DingTalkConnectSyncDeptAttrName == "" {
			req.DingTalkConnectSyncDeptAttrName = "钉钉部门"
		}
	}

	if req.WeChatConnectEnabled {
		req.WeChatConnectAppID = strings.TrimSpace(req.WeChatConnectAppID)
		req.WeChatConnectAppSecret = strings.TrimSpace(req.WeChatConnectAppSecret)
		req.WeChatConnectOpenAppID = strings.TrimSpace(req.WeChatConnectOpenAppID)
		req.WeChatConnectOpenAppSecret = strings.TrimSpace(req.WeChatConnectOpenAppSecret)
		req.WeChatConnectMPAppID = strings.TrimSpace(req.WeChatConnectMPAppID)
		req.WeChatConnectMPAppSecret = strings.TrimSpace(req.WeChatConnectMPAppSecret)
		req.WeChatConnectMobileAppID = strings.TrimSpace(req.WeChatConnectMobileAppID)
		req.WeChatConnectMobileAppSecret = strings.TrimSpace(req.WeChatConnectMobileAppSecret)
		req.WeChatConnectMode = strings.ToLower(strings.TrimSpace(req.WeChatConnectMode))
		req.WeChatConnectScopes = strings.TrimSpace(req.WeChatConnectScopes)
		req.WeChatConnectRedirectURL = strings.TrimSpace(req.WeChatConnectRedirectURL)
		req.WeChatConnectFrontendRedirectURL = strings.TrimSpace(req.WeChatConnectFrontendRedirectURL)
		req.WeChatConnectAppID = strings.TrimSpace(firstNonEmpty(req.WeChatConnectAppID, previousSettings.WeChatConnectAppID))
		req.WeChatConnectRedirectURL = strings.TrimSpace(firstNonEmpty(req.WeChatConnectRedirectURL, previousSettings.WeChatConnectRedirectURL))
		req.WeChatConnectFrontendRedirectURL = strings.TrimSpace(firstNonEmpty(req.WeChatConnectFrontendRedirectURL, previousSettings.WeChatConnectFrontendRedirectURL))
		if req.WeChatConnectMode == "" {
			req.WeChatConnectMode = strings.ToLower(strings.TrimSpace(previousSettings.WeChatConnectMode))
		}
		if req.WeChatConnectScopes == "" {
			req.WeChatConnectScopes = strings.TrimSpace(previousSettings.WeChatConnectScopes)
		}

		if req.WeChatConnectMPEnabled && req.WeChatConnectMobileEnabled {
			response.BadRequest(c, "WeChat Official Account and Mobile App cannot be enabled at the same time")
			return
		}
		if req.WeChatConnectMode != "" {
			switch req.WeChatConnectMode {
			case "open", "mp", "mobile":
			default:
				response.BadRequest(c, "WeChat mode must be open, mp, or mobile")
				return
			}
		}
		if !req.WeChatConnectOpenEnabled && !req.WeChatConnectMPEnabled && !req.WeChatConnectMobileEnabled {
			switch req.WeChatConnectMode {
			case "mp":
				req.WeChatConnectMPEnabled = true
			case "mobile":
				req.WeChatConnectMobileEnabled = true
			default:
				req.WeChatConnectOpenEnabled = true
			}
		}
		if req.WeChatConnectMode == "" {
			if req.WeChatConnectMPEnabled {
				req.WeChatConnectMode = "mp"
			} else if req.WeChatConnectMobileEnabled {
				req.WeChatConnectMode = "mobile"
			} else {
				req.WeChatConnectMode = "open"
			}
		}

		req.WeChatConnectOpenAppID = strings.TrimSpace(firstNonEmpty(req.WeChatConnectOpenAppID, req.WeChatConnectAppID, previousSettings.WeChatConnectOpenAppID, previousSettings.WeChatConnectAppID))
		req.WeChatConnectMPAppID = strings.TrimSpace(firstNonEmpty(req.WeChatConnectMPAppID, req.WeChatConnectAppID, previousSettings.WeChatConnectMPAppID, previousSettings.WeChatConnectAppID))
		req.WeChatConnectMobileAppID = strings.TrimSpace(firstNonEmpty(req.WeChatConnectMobileAppID, req.WeChatConnectAppID, previousSettings.WeChatConnectMobileAppID, previousSettings.WeChatConnectAppID))

		if req.WeChatConnectOpenAppSecret == "" {
			req.WeChatConnectOpenAppSecret = strings.TrimSpace(firstNonEmpty(previousSettings.WeChatConnectOpenAppSecret, previousSettings.WeChatConnectAppSecret, req.WeChatConnectAppSecret))
		}
		if req.WeChatConnectMPAppSecret == "" {
			req.WeChatConnectMPAppSecret = strings.TrimSpace(firstNonEmpty(previousSettings.WeChatConnectMPAppSecret, previousSettings.WeChatConnectAppSecret, req.WeChatConnectAppSecret))
		}
		if req.WeChatConnectMobileAppSecret == "" {
			req.WeChatConnectMobileAppSecret = strings.TrimSpace(firstNonEmpty(previousSettings.WeChatConnectMobileAppSecret, previousSettings.WeChatConnectAppSecret, req.WeChatConnectAppSecret))
		}
		if req.WeChatConnectAppSecret == "" {
			req.WeChatConnectAppSecret = strings.TrimSpace(firstNonEmpty(req.WeChatConnectOpenAppSecret, req.WeChatConnectMPAppSecret, req.WeChatConnectMobileAppSecret, previousSettings.WeChatConnectAppSecret))
		}

		if req.WeChatConnectOpenEnabled {
			if req.WeChatConnectOpenAppID == "" {
				response.BadRequest(c, "WeChat PC App ID is required when enabled")
				return
			}
			if req.WeChatConnectOpenAppSecret == "" {
				response.BadRequest(c, "WeChat PC App Secret is required when enabled")
				return
			}
		}
		if req.WeChatConnectMPEnabled {
			if req.WeChatConnectMPAppID == "" {
				response.BadRequest(c, "WeChat Official Account App ID is required when enabled")
				return
			}
			if req.WeChatConnectMPAppSecret == "" {
				response.BadRequest(c, "WeChat Official Account App Secret is required when enabled")
				return
			}
		}
		if req.WeChatConnectMobileEnabled {
			if req.WeChatConnectMobileAppID == "" {
				response.BadRequest(c, "WeChat Mobile App ID is required when enabled")
				return
			}
			if req.WeChatConnectMobileAppSecret == "" {
				response.BadRequest(c, "WeChat Mobile App Secret is required when enabled")
				return
			}
		}

		if req.WeChatConnectScopes == "" {
			if req.WeChatConnectMPEnabled {
				req.WeChatConnectScopes = service.DefaultWeChatConnectScopesForMode("mp")
			} else {
				req.WeChatConnectScopes = service.DefaultWeChatConnectScopesForMode(req.WeChatConnectMode)
			}
		}
		if req.WeChatConnectOpenEnabled || req.WeChatConnectMPEnabled {
			if req.WeChatConnectRedirectURL == "" {
				response.BadRequest(c, "WeChat Redirect URL is required when web oauth is enabled")
				return
			}
			if err := config.ValidateAbsoluteHTTPURL(req.WeChatConnectRedirectURL); err != nil {
				response.BadRequest(c, "WeChat Redirect URL must be an absolute http(s) URL")
				return
			}
			if req.WeChatConnectFrontendRedirectURL == "" {
				req.WeChatConnectFrontendRedirectURL = "/auth/wechat/callback"
			}
			if err := config.ValidateFrontendRedirectURL(req.WeChatConnectFrontendRedirectURL); err != nil {
				response.BadRequest(c, "WeChat Frontend Redirect URL is invalid")
				return
			}
		}
	}

	// Generic OIDC 参数验证
	oidcUsePKCE, oidcValidateIDToken, err := h.settingService.OIDCSecurityWriteDefaults(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.OIDCConnectEnabled {
		req.OIDCConnectProviderName = strings.TrimSpace(req.OIDCConnectProviderName)
		req.OIDCConnectClientID = strings.TrimSpace(req.OIDCConnectClientID)
		req.OIDCConnectClientSecret = strings.TrimSpace(req.OIDCConnectClientSecret)
		req.OIDCConnectIssuerURL = strings.TrimSpace(req.OIDCConnectIssuerURL)
		req.OIDCConnectDiscoveryURL = strings.TrimSpace(req.OIDCConnectDiscoveryURL)
		req.OIDCConnectAuthorizeURL = strings.TrimSpace(req.OIDCConnectAuthorizeURL)
		req.OIDCConnectTokenURL = strings.TrimSpace(req.OIDCConnectTokenURL)
		req.OIDCConnectUserInfoURL = strings.TrimSpace(req.OIDCConnectUserInfoURL)
		req.OIDCConnectJWKSURL = strings.TrimSpace(req.OIDCConnectJWKSURL)
		req.OIDCConnectScopes = strings.TrimSpace(req.OIDCConnectScopes)
		req.OIDCConnectRedirectURL = strings.TrimSpace(req.OIDCConnectRedirectURL)
		req.OIDCConnectFrontendRedirectURL = strings.TrimSpace(req.OIDCConnectFrontendRedirectURL)
		req.OIDCConnectTokenAuthMethod = strings.ToLower(strings.TrimSpace(req.OIDCConnectTokenAuthMethod))
		req.OIDCConnectAllowedSigningAlgs = strings.TrimSpace(req.OIDCConnectAllowedSigningAlgs)
		req.OIDCConnectUserInfoEmailPath = strings.TrimSpace(req.OIDCConnectUserInfoEmailPath)
		req.OIDCConnectUserInfoIDPath = strings.TrimSpace(req.OIDCConnectUserInfoIDPath)
		req.OIDCConnectUserInfoUsernamePath = strings.TrimSpace(req.OIDCConnectUserInfoUsernamePath)
		req.OIDCConnectProviderName = strings.TrimSpace(firstNonEmpty(req.OIDCConnectProviderName, previousSettings.OIDCConnectProviderName, "OIDC"))
		req.OIDCConnectClientID = strings.TrimSpace(firstNonEmpty(req.OIDCConnectClientID, previousSettings.OIDCConnectClientID))
		req.OIDCConnectIssuerURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectIssuerURL, previousSettings.OIDCConnectIssuerURL))
		req.OIDCConnectDiscoveryURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectDiscoveryURL, previousSettings.OIDCConnectDiscoveryURL))
		req.OIDCConnectAuthorizeURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectAuthorizeURL, previousSettings.OIDCConnectAuthorizeURL))
		req.OIDCConnectTokenURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectTokenURL, previousSettings.OIDCConnectTokenURL))
		req.OIDCConnectUserInfoURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectUserInfoURL, previousSettings.OIDCConnectUserInfoURL))
		req.OIDCConnectJWKSURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectJWKSURL, previousSettings.OIDCConnectJWKSURL))
		req.OIDCConnectScopes = strings.TrimSpace(firstNonEmpty(req.OIDCConnectScopes, previousSettings.OIDCConnectScopes, "openid email profile"))
		req.OIDCConnectRedirectURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectRedirectURL, previousSettings.OIDCConnectRedirectURL))
		req.OIDCConnectFrontendRedirectURL = strings.TrimSpace(firstNonEmpty(req.OIDCConnectFrontendRedirectURL, previousSettings.OIDCConnectFrontendRedirectURL, "/auth/oidc/callback"))
		req.OIDCConnectTokenAuthMethod = strings.ToLower(strings.TrimSpace(firstNonEmpty(req.OIDCConnectTokenAuthMethod, previousSettings.OIDCConnectTokenAuthMethod, "client_secret_post")))
		req.OIDCConnectAllowedSigningAlgs = strings.TrimSpace(firstNonEmpty(req.OIDCConnectAllowedSigningAlgs, previousSettings.OIDCConnectAllowedSigningAlgs, "RS256,ES256,PS256"))
		req.OIDCConnectUserInfoEmailPath = strings.TrimSpace(firstNonEmpty(req.OIDCConnectUserInfoEmailPath, previousSettings.OIDCConnectUserInfoEmailPath))
		req.OIDCConnectUserInfoIDPath = strings.TrimSpace(firstNonEmpty(req.OIDCConnectUserInfoIDPath, previousSettings.OIDCConnectUserInfoIDPath))
		req.OIDCConnectUserInfoUsernamePath = strings.TrimSpace(firstNonEmpty(req.OIDCConnectUserInfoUsernamePath, previousSettings.OIDCConnectUserInfoUsernamePath))
		if req.OIDCConnectUsePKCE != nil {
			oidcUsePKCE = *req.OIDCConnectUsePKCE
		}
		if req.OIDCConnectValidateIDToken != nil {
			oidcValidateIDToken = *req.OIDCConnectValidateIDToken
		}
		if req.OIDCConnectClockSkewSeconds == 0 {
			req.OIDCConnectClockSkewSeconds = previousSettings.OIDCConnectClockSkewSeconds
			if req.OIDCConnectClockSkewSeconds == 0 {
				req.OIDCConnectClockSkewSeconds = 120
			}
		}

		if req.OIDCConnectClientID == "" {
			response.BadRequest(c, "OIDC Client ID is required when enabled")
			return
		}
		if req.OIDCConnectIssuerURL == "" {
			response.BadRequest(c, "OIDC Issuer URL is required when enabled")
			return
		}
		if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectIssuerURL); err != nil {
			response.BadRequest(c, "OIDC Issuer URL must be an absolute http(s) URL")
			return
		}
		if req.OIDCConnectDiscoveryURL != "" {
			if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectDiscoveryURL); err != nil {
				response.BadRequest(c, "OIDC Discovery URL must be an absolute http(s) URL")
				return
			}
		}
		if req.OIDCConnectAuthorizeURL != "" {
			if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectAuthorizeURL); err != nil {
				response.BadRequest(c, "OIDC Authorize URL must be an absolute http(s) URL")
				return
			}
		}
		if req.OIDCConnectTokenURL != "" {
			if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectTokenURL); err != nil {
				response.BadRequest(c, "OIDC Token URL must be an absolute http(s) URL")
				return
			}
		}
		if req.OIDCConnectUserInfoURL != "" {
			if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectUserInfoURL); err != nil {
				response.BadRequest(c, "OIDC UserInfo URL must be an absolute http(s) URL")
				return
			}
		}
		if req.OIDCConnectRedirectURL == "" {
			response.BadRequest(c, "OIDC Redirect URL is required when enabled")
			return
		}
		if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectRedirectURL); err != nil {
			response.BadRequest(c, "OIDC Redirect URL must be an absolute http(s) URL")
			return
		}
		if req.OIDCConnectFrontendRedirectURL == "" {
			response.BadRequest(c, "OIDC Frontend Redirect URL is required when enabled")
			return
		}
		if err := config.ValidateFrontendRedirectURL(req.OIDCConnectFrontendRedirectURL); err != nil {
			response.BadRequest(c, "OIDC Frontend Redirect URL is invalid")
			return
		}
		if !scopesContainOpenID(req.OIDCConnectScopes) {
			response.BadRequest(c, "OIDC scopes must contain openid")
			return
		}
		switch req.OIDCConnectTokenAuthMethod {
		case "", "client_secret_post", "client_secret_basic", "none":
		default:
			response.BadRequest(c, "OIDC Token Auth Method must be one of client_secret_post/client_secret_basic/none")
			return
		}
		if req.OIDCConnectClockSkewSeconds < 0 || req.OIDCConnectClockSkewSeconds > 600 {
			response.BadRequest(c, "OIDC clock skew seconds must be between 0 and 600")
			return
		}
		if oidcValidateIDToken && req.OIDCConnectAllowedSigningAlgs == "" {
			response.BadRequest(c, "OIDC Allowed Signing Algs is required when validate_id_token=true")
			return
		}
		if req.OIDCConnectJWKSURL != "" {
			if err := config.ValidateAbsoluteHTTPURL(req.OIDCConnectJWKSURL); err != nil {
				response.BadRequest(c, "OIDC JWKS URL must be an absolute http(s) URL")
				return
			}
		}
		if req.OIDCConnectTokenAuthMethod == "" || req.OIDCConnectTokenAuthMethod == "client_secret_post" || req.OIDCConnectTokenAuthMethod == "client_secret_basic" {
			if req.OIDCConnectClientSecret == "" {
				if previousSettings.OIDCConnectClientSecret == "" {
					response.BadRequest(c, "OIDC Client Secret is required when enabled")
					return
				}
				req.OIDCConnectClientSecret = previousSettings.OIDCConnectClientSecret
			}
		}
	}

	// “购买订阅”页面配置验证
	purchaseEnabled := previousSettings.PurchaseSubscriptionEnabled
	if req.PurchaseSubscriptionEnabled != nil {
		purchaseEnabled = *req.PurchaseSubscriptionEnabled
	}
	purchaseURL := previousSettings.PurchaseSubscriptionURL
	if req.PurchaseSubscriptionURL != nil {
		purchaseURL = strings.TrimSpace(*req.PurchaseSubscriptionURL)
	}

	// - 启用时要求 URL 合法且非空
	// - 禁用时允许为空；若提供了 URL 也做基本校验，避免误配置
	if purchaseEnabled {
		if purchaseURL == "" {
			response.BadRequest(c, "Purchase Subscription URL is required when enabled")
			return
		}
		if err := config.ValidateAbsoluteHTTPURL(purchaseURL); err != nil {
			response.BadRequest(c, "Purchase Subscription URL must be an absolute http(s) URL")
			return
		}
	} else if purchaseURL != "" {
		if err := config.ValidateAbsoluteHTTPURL(purchaseURL); err != nil {
			response.BadRequest(c, "Purchase Subscription URL must be an absolute http(s) URL")
			return
		}
	}

	// Frontend URL 验证
	req.FrontendURL = strings.TrimSpace(req.FrontendURL)
	if req.FrontendURL != "" {
		if err := config.ValidateAbsoluteHTTPURL(req.FrontendURL); err != nil {
			response.BadRequest(c, "Frontend URL must be an absolute http(s) URL")
			return
		}
	}

	// 自定义菜单项验证
	const (
		maxCustomMenuItems    = 20
		maxMenuItemLabelLen   = 50
		maxMenuItemURLLen     = 2048
		maxMenuItemIconSVGLen = 10 * 1024 // 10KB
		maxMenuItemIDLen      = 32
	)

	customMenuJSON := previousSettings.CustomMenuItems
	if req.CustomMenuItems != nil {
		items := *req.CustomMenuItems
		if len(items) > maxCustomMenuItems {
			response.BadRequest(c, "Too many custom menu items (max 20)")
			return
		}
		for i, item := range items {
			if strings.TrimSpace(item.Label) == "" {
				response.BadRequest(c, "Custom menu item label is required")
				return
			}
			if len(item.Label) > maxMenuItemLabelLen {
				response.BadRequest(c, "Custom menu item label is too long (max 50 characters)")
				return
			}
			urlTrimmed := strings.TrimSpace(item.URL)
			if strings.HasPrefix(urlTrimmed, "md:") {
				// Markdown page mode: URL = "md:<slug>"
				slug := strings.TrimPrefix(urlTrimmed, "md:")
				if slug == "" {
					response.BadRequest(c, "Custom menu item markdown slug cannot be empty (use md:slug format)")
					return
				}
			} else {
				if urlTrimmed == "" {
					response.BadRequest(c, "Custom menu item URL is required (use md:slug for markdown pages)")
					return
				}
				if len(item.URL) > maxMenuItemURLLen {
					response.BadRequest(c, "Custom menu item URL is too long (max 2048 characters)")
					return
				}
				if err := config.ValidateAbsoluteHTTPURL(urlTrimmed); err != nil {
					response.BadRequest(c, "Custom menu item URL must be an absolute http(s) URL or md:<slug>")
					return
				}
			}
			if item.Visibility != "user" && item.Visibility != "admin" {
				response.BadRequest(c, "Custom menu item visibility must be 'user' or 'admin'")
				return
			}
			if len(item.IconSVG) > maxMenuItemIconSVGLen {
				response.BadRequest(c, "Custom menu item icon SVG is too large (max 10KB)")
				return
			}
			// Auto-generate ID if missing
			if strings.TrimSpace(item.ID) == "" {
				id, err := generateMenuItemID()
				if err != nil {
					response.Error(c, http.StatusInternalServerError, "Failed to generate menu item ID")
					return
				}
				items[i].ID = id
			} else if len(item.ID) > maxMenuItemIDLen {
				response.BadRequest(c, "Custom menu item ID is too long (max 32 characters)")
				return
			} else if !menuItemIDPattern.MatchString(item.ID) {
				response.BadRequest(c, "Custom menu item ID contains invalid characters (only a-z, A-Z, 0-9, - and _ are allowed)")
				return
			}
		}
		// ID uniqueness check
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			if _, exists := seen[item.ID]; exists {
				response.BadRequest(c, "Duplicate custom menu item ID: "+item.ID)
				return
			}
			seen[item.ID] = struct{}{}
		}
		menuBytes, err := json.Marshal(items)
		if err != nil {
			response.BadRequest(c, "Failed to serialize custom menu items")
			return
		}
		customMenuJSON = string(menuBytes)
	}

	// 自定义端点验证
	const (
		maxCustomEndpoints        = 10
		maxEndpointNameLen        = 50
		maxEndpointURLLen         = 2048
		maxEndpointDescriptionLen = 200
	)

	customEndpointsJSON := previousSettings.CustomEndpoints
	if req.CustomEndpoints != nil {
		endpoints := *req.CustomEndpoints
		if len(endpoints) > maxCustomEndpoints {
			response.BadRequest(c, "Too many custom endpoints (max 10)")
			return
		}
		for _, ep := range endpoints {
			if strings.TrimSpace(ep.Name) == "" {
				response.BadRequest(c, "Custom endpoint name is required")
				return
			}
			if len(ep.Name) > maxEndpointNameLen {
				response.BadRequest(c, "Custom endpoint name is too long (max 50 characters)")
				return
			}
			if strings.TrimSpace(ep.Endpoint) == "" {
				response.BadRequest(c, "Custom endpoint URL is required")
				return
			}
			if len(ep.Endpoint) > maxEndpointURLLen {
				response.BadRequest(c, "Custom endpoint URL is too long (max 2048 characters)")
				return
			}
			if err := config.ValidateAbsoluteHTTPURL(strings.TrimSpace(ep.Endpoint)); err != nil {
				response.BadRequest(c, "Custom endpoint URL must be an absolute http(s) URL")
				return
			}
			if len(ep.Description) > maxEndpointDescriptionLen {
				response.BadRequest(c, "Custom endpoint description is too long (max 200 characters)")
				return
			}
		}
		endpointBytes, err := json.Marshal(endpoints)
		if err != nil {
			response.BadRequest(c, "Failed to serialize custom endpoints")
			return
		}
		customEndpointsJSON = string(endpointBytes)
	}

	// Ops metrics collector interval validation (seconds).
	if req.OpsMetricsIntervalSeconds != nil {
		v := *req.OpsMetricsIntervalSeconds
		if v < 60 {
			v = 60
		}
		if v > 3600 {
			v = 3600
		}
		req.OpsMetricsIntervalSeconds = &v
	}
	defaultSubscriptions := make([]service.DefaultSubscriptionSetting, 0, len(req.DefaultSubscriptions))
	for _, sub := range req.DefaultSubscriptions {
		defaultSubscriptions = append(defaultSubscriptions, service.DefaultSubscriptionSetting{
			GroupID:      sub.GroupID,
			ValidityDays: sub.ValidityDays,
		})
	}

	// 验证最低版本号格式（空字符串=禁用，或合法 semver）
	if req.MinClaudeCodeVersion != "" {
		if !semverPattern.MatchString(req.MinClaudeCodeVersion) {
			response.Error(c, http.StatusBadRequest, "min_claude_code_version must be empty or a valid semver (e.g. 2.1.63)")
			return
		}
	}

	// 验证最高版本号格式（空字符串=禁用，或合法 semver）
	if req.MaxClaudeCodeVersion != "" {
		if !semverPattern.MatchString(req.MaxClaudeCodeVersion) {
			response.Error(c, http.StatusBadRequest, "max_claude_code_version must be empty or a valid semver (e.g. 3.0.0)")
			return
		}
	}
	if req.AntigravityUserAgentVersion != nil {
		normalized := strings.TrimSpace(*req.AntigravityUserAgentVersion)
		req.AntigravityUserAgentVersion = &normalized
		if normalized != "" && !semverPattern.MatchString(normalized) {
			response.Error(c, http.StatusBadRequest, "antigravity_user_agent_version must be empty or a valid semver (e.g. 1.23.2)")
			return
		}
	}
	if req.OpenAICodexUserAgent != nil {
		normalized := strings.TrimSpace(*req.OpenAICodexUserAgent)
		req.OpenAICodexUserAgent = &normalized
		// 仅做长度上限保护，不限制具体格式（运维需要可自由调整 codex 版本号）
		if len(normalized) > 512 {
			response.Error(c, http.StatusBadRequest, "openai_codex_user_agent must be at most 512 characters")
			return
		}
	}

	// 交叉验证：如果同时设置了最低和最高版本号，最高版本号必须 >= 最低版本号
	if req.MinClaudeCodeVersion != "" && req.MaxClaudeCodeVersion != "" {
		if service.CompareVersions(req.MaxClaudeCodeVersion, req.MinClaudeCodeVersion) < 0 {
			response.Error(c, http.StatusBadRequest, "max_claude_code_version must be greater than or equal to min_claude_code_version")
			return
		}
	}

	settings := &service.SystemSettings{
		// 系统全局 platform quota 默认值（整体替换语义）
		DefaultPlatformQuotas: req.DefaultPlatformQuotas,

		RegistrationEnabled: func() bool {
			if req.RegistrationEnabled != nil {
				return *req.RegistrationEnabled
			}
			return previousSettings.RegistrationEnabled
		}(),
		EmailVerifyEnabled: func() bool {
			if req.EmailVerifyEnabled != nil {
				return *req.EmailVerifyEnabled
			}
			return previousSettings.EmailVerifyEnabled
		}(),
		RegistrationEmailSuffixWhitelist: req.RegistrationEmailSuffixWhitelist,
		PromoCodeEnabled: func() bool {
			if req.PromoCodeEnabled != nil {
				return *req.PromoCodeEnabled
			}
			return previousSettings.PromoCodeEnabled
		}(),
		PasswordResetEnabled: func() bool {
			if req.PasswordResetEnabled != nil {
				return *req.PasswordResetEnabled
			}
			return previousSettings.PasswordResetEnabled
		}(),
		FrontendURL: req.FrontendURL,
		InvitationCodeEnabled: func() bool {
			if req.InvitationCodeEnabled != nil {
				return *req.InvitationCodeEnabled
			}
			return previousSettings.InvitationCodeEnabled
		}(),
		TotpEnabled: func() bool {
			if req.TotpEnabled != nil {
				return *req.TotpEnabled
			}
			return previousSettings.TotpEnabled
		}(),
		LoginAgreementEnabled: func() bool {
			if req.LoginAgreementEnabled != nil {
				return *req.LoginAgreementEnabled
			}
			return previousSettings.LoginAgreementEnabled
		}(),
		LoginAgreementMode:      loginAgreementMode,
		LoginAgreementUpdatedAt: loginAgreementUpdatedAt,
		LoginAgreementDocuments: loginAgreementDocuments,
		SMTPHost:                req.SMTPHost,
		SMTPPort:                req.SMTPPort,
		SMTPUsername:            req.SMTPUsername,
		SMTPPassword:            req.SMTPPassword,
		SMTPFrom:                req.SMTPFrom,
		SMTPFromName:            req.SMTPFromName,
		SMTPUseTLS:              req.SMTPUseTLS,
		TurnstileEnabled:        req.TurnstileEnabled,
		TurnstileSiteKey:        req.TurnstileSiteKey,
		TurnstileSecretKey:      req.TurnstileSecretKey,
		APIKeyACLTrustForwardedIP: func() bool {
			if req.APIKeyACLTrustForwardedIP != nil {
				return *req.APIKeyACLTrustForwardedIP
			}
			return previousSettings.APIKeyACLTrustForwardedIP
		}(),
		LinuxDoConnectEnabled:                  req.LinuxDoConnectEnabled,
		LinuxDoConnectClientID:                 req.LinuxDoConnectClientID,
		LinuxDoConnectClientSecret:             req.LinuxDoConnectClientSecret,
		LinuxDoConnectRedirectURL:              req.LinuxDoConnectRedirectURL,
		DingTalkConnectEnabled:                 req.DingTalkConnectEnabled,
		DingTalkConnectClientID:                req.DingTalkConnectClientID,
		DingTalkConnectClientSecret:            req.DingTalkConnectClientSecret,
		DingTalkConnectRedirectURL:             req.DingTalkConnectRedirectURL,
		DingTalkConnectCorpRestrictionPolicy:   req.DingTalkConnectCorpRestrictionPolicy,
		DingTalkConnectInternalCorpID:          req.DingTalkConnectInternalCorpID,
		DingTalkConnectBypassRegistration:      req.DingTalkConnectBypassRegistration,
		DingTalkConnectSyncCorpEmail:           req.DingTalkConnectSyncCorpEmail,
		DingTalkConnectSyncDisplayName:         req.DingTalkConnectSyncDisplayName,
		DingTalkConnectSyncDept:                req.DingTalkConnectSyncDept,
		DingTalkConnectSyncCorpEmailAttrKey:    req.DingTalkConnectSyncCorpEmailAttrKey,
		DingTalkConnectSyncDisplayNameAttrKey:  req.DingTalkConnectSyncDisplayNameAttrKey,
		DingTalkConnectSyncDeptAttrKey:         req.DingTalkConnectSyncDeptAttrKey,
		DingTalkConnectSyncCorpEmailAttrName:   req.DingTalkConnectSyncCorpEmailAttrName,
		DingTalkConnectSyncDisplayNameAttrName: req.DingTalkConnectSyncDisplayNameAttrName,
		DingTalkConnectSyncDeptAttrName:        req.DingTalkConnectSyncDeptAttrName,
		WeChatConnectEnabled:                   req.WeChatConnectEnabled,
		WeChatConnectAppID:                     req.WeChatConnectAppID,
		WeChatConnectAppSecret:                 req.WeChatConnectAppSecret,
		WeChatConnectOpenAppID:                 req.WeChatConnectOpenAppID,
		WeChatConnectOpenAppSecret:             req.WeChatConnectOpenAppSecret,
		WeChatConnectMPAppID:                   req.WeChatConnectMPAppID,
		WeChatConnectMPAppSecret:               req.WeChatConnectMPAppSecret,
		WeChatConnectMobileAppID:               req.WeChatConnectMobileAppID,
		WeChatConnectMobileAppSecret:           req.WeChatConnectMobileAppSecret,
		WeChatConnectOpenEnabled:               req.WeChatConnectOpenEnabled,
		WeChatConnectMPEnabled:                 req.WeChatConnectMPEnabled,
		WeChatConnectMobileEnabled:             req.WeChatConnectMobileEnabled,
		WeChatConnectMode:                      req.WeChatConnectMode,
		WeChatConnectScopes:                    req.WeChatConnectScopes,
		WeChatConnectRedirectURL:               req.WeChatConnectRedirectURL,
		WeChatConnectFrontendRedirectURL:       req.WeChatConnectFrontendRedirectURL,
		OIDCConnectEnabled:                     req.OIDCConnectEnabled,
		OIDCConnectProviderName:                req.OIDCConnectProviderName,
		OIDCConnectClientID:                    req.OIDCConnectClientID,
		OIDCConnectClientSecret:                req.OIDCConnectClientSecret,
		OIDCConnectIssuerURL:                   req.OIDCConnectIssuerURL,
		OIDCConnectDiscoveryURL:                req.OIDCConnectDiscoveryURL,
		OIDCConnectAuthorizeURL:                req.OIDCConnectAuthorizeURL,
		OIDCConnectTokenURL:                    req.OIDCConnectTokenURL,
		OIDCConnectUserInfoURL:                 req.OIDCConnectUserInfoURL,
		OIDCConnectJWKSURL:                     req.OIDCConnectJWKSURL,
		OIDCConnectScopes:                      req.OIDCConnectScopes,
		OIDCConnectRedirectURL:                 req.OIDCConnectRedirectURL,
		OIDCConnectFrontendRedirectURL:         req.OIDCConnectFrontendRedirectURL,
		OIDCConnectTokenAuthMethod:             req.OIDCConnectTokenAuthMethod,
		OIDCConnectUsePKCE:                     oidcUsePKCE,
		OIDCConnectValidateIDToken:             oidcValidateIDToken,
		OIDCConnectAllowedSigningAlgs:          req.OIDCConnectAllowedSigningAlgs,
		OIDCConnectClockSkewSeconds:            req.OIDCConnectClockSkewSeconds,
		OIDCConnectRequireEmailVerified:        req.OIDCConnectRequireEmailVerified,
		OIDCConnectUserInfoEmailPath:           req.OIDCConnectUserInfoEmailPath,
		OIDCConnectUserInfoIDPath:              req.OIDCConnectUserInfoIDPath,
		OIDCConnectUserInfoUsernamePath:        req.OIDCConnectUserInfoUsernamePath,
		GitHubOAuthEnabled:                     req.GitHubOAuthEnabled,
		GitHubOAuthClientID:                    req.GitHubOAuthClientID,
		GitHubOAuthClientSecret:                req.GitHubOAuthClientSecret,
		GitHubOAuthRedirectURL:                 req.GitHubOAuthRedirectURL,
		GitHubOAuthFrontendRedirectURL:         req.GitHubOAuthFrontendRedirectURL,
		GoogleOAuthEnabled:                     req.GoogleOAuthEnabled,
		GoogleOAuthClientID:                    req.GoogleOAuthClientID,
		GoogleOAuthClientSecret:                req.GoogleOAuthClientSecret,
		GoogleOAuthRedirectURL:                 req.GoogleOAuthRedirectURL,
		GoogleOAuthFrontendRedirectURL:         req.GoogleOAuthFrontendRedirectURL,
		SiteName:                               req.SiteName,
		SiteLogo:                               req.SiteLogo,
		SiteSubtitle:                           req.SiteSubtitle,
		APIBaseURL:                             req.APIBaseURL,
		ContactInfo:                            req.ContactInfo,
		DocURL:                                 req.DocURL,
		HomeContent:                            req.HomeContent,
		HideCcsImportButton:                    req.HideCcsImportButton,
		PurchaseSubscriptionEnabled:            purchaseEnabled,
		PurchaseSubscriptionURL:                purchaseURL,
		TableDefaultPageSize:                   req.TableDefaultPageSize,
		TablePageSizeOptions:                   req.TablePageSizeOptions,
		CustomMenuItems:                        customMenuJSON,
		CustomEndpoints:                        customEndpointsJSON,
		DefaultConcurrency:                     req.DefaultConcurrency,
		DefaultBalance:                         req.DefaultBalance,
		AffiliateRebateRate:                    affiliateRebateRate,
		AffiliateRebateFreezeHours:             affiliateRebateFreezeHours,
		AffiliateRebateDurationDays:            affiliateRebateDurationDays,
		AffiliateRebatePerInviteeCap:           affiliateRebatePerInviteeCap,
		DefaultUserRPMLimit:                    req.DefaultUserRPMLimit,
		DefaultSubscriptions:                   defaultSubscriptions,
		EnableModelFallback:                    req.EnableModelFallback,
		FallbackModelAnthropic:                 req.FallbackModelAnthropic,
		FallbackModelOpenAI:                    req.FallbackModelOpenAI,
		FallbackModelGemini:                    req.FallbackModelGemini,
		FallbackModelAntigravity:               req.FallbackModelAntigravity,
		EnableIdentityPatch:                    req.EnableIdentityPatch,
		IdentityPatchPrompt:                    req.IdentityPatchPrompt,
		MinClaudeCodeVersion:                   req.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:                   req.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling:            req.AllowUngroupedKeyScheduling,
		BackendModeEnabled:                     req.BackendModeEnabled,
		OpsMonitoringEnabled: func() bool {
			if req.OpsMonitoringEnabled != nil {
				return *req.OpsMonitoringEnabled
			}
			return previousSettings.OpsMonitoringEnabled
		}(),
		OpsRealtimeMonitoringEnabled: func() bool {
			if req.OpsRealtimeMonitoringEnabled != nil {
				return *req.OpsRealtimeMonitoringEnabled
			}
			return previousSettings.OpsRealtimeMonitoringEnabled
		}(),
		OpsQueryModeDefault: func() string {
			if req.OpsQueryModeDefault != nil {
				return *req.OpsQueryModeDefault
			}
			return previousSettings.OpsQueryModeDefault
		}(),
		OpsMetricsIntervalSeconds: func() int {
			if req.OpsMetricsIntervalSeconds != nil {
				return *req.OpsMetricsIntervalSeconds
			}
			return previousSettings.OpsMetricsIntervalSeconds
		}(),
		EnableFingerprintUnification: func() bool {
			if req.EnableFingerprintUnification != nil {
				return *req.EnableFingerprintUnification
			}
			return previousSettings.EnableFingerprintUnification
		}(),
		EnableMetadataPassthrough: func() bool {
			if req.EnableMetadataPassthrough != nil {
				return *req.EnableMetadataPassthrough
			}
			return previousSettings.EnableMetadataPassthrough
		}(),
		EnableCCHSigning: func() bool {
			if req.EnableCCHSigning != nil {
				return *req.EnableCCHSigning
			}
			return previousSettings.EnableCCHSigning
		}(),
		EnableAnthropicCacheTTL1hInjection: func() bool {
			if req.EnableAnthropicCacheTTL1hInjection != nil {
				return *req.EnableAnthropicCacheTTL1hInjection
			}
			return previousSettings.EnableAnthropicCacheTTL1hInjection
		}(),
		RewriteMessageCacheControl: func() bool {
			if req.RewriteMessageCacheControl != nil {
				return *req.RewriteMessageCacheControl
			}
			return previousSettings.RewriteMessageCacheControl
		}(),
		AntigravityUserAgentVersion: func() string {
			if req.AntigravityUserAgentVersion != nil {
				return *req.AntigravityUserAgentVersion
			}
			return previousSettings.AntigravityUserAgentVersion
		}(),
		OpenAICodexUserAgent: func() string {
			if req.OpenAICodexUserAgent != nil {
				return *req.OpenAICodexUserAgent
			}
			return previousSettings.OpenAICodexUserAgent
		}(),
		OpenAIAllowClaudeCodeCodexPlugin: func() bool {
			if req.OpenAIAllowClaudeCodeCodexPlugin != nil {
				return *req.OpenAIAllowClaudeCodeCodexPlugin
			}
			return previousSettings.OpenAIAllowClaudeCodeCodexPlugin
		}(),
		PaymentVisibleMethodAlipaySource: func() string {
			if req.PaymentVisibleMethodAlipaySource != nil {
				return strings.TrimSpace(*req.PaymentVisibleMethodAlipaySource)
			}
			return previousSettings.PaymentVisibleMethodAlipaySource
		}(),
		PaymentVisibleMethodWxpaySource: func() string {
			if req.PaymentVisibleMethodWxpaySource != nil {
				return strings.TrimSpace(*req.PaymentVisibleMethodWxpaySource)
			}
			return previousSettings.PaymentVisibleMethodWxpaySource
		}(),
		PaymentVisibleMethodAlipayEnabled: func() bool {
			if req.PaymentVisibleMethodAlipayEnabled != nil {
				return *req.PaymentVisibleMethodAlipayEnabled
			}
			return previousSettings.PaymentVisibleMethodAlipayEnabled
		}(),
		PaymentVisibleMethodWxpayEnabled: func() bool {
			if req.PaymentVisibleMethodWxpayEnabled != nil {
				return *req.PaymentVisibleMethodWxpayEnabled
			}
			return previousSettings.PaymentVisibleMethodWxpayEnabled
		}(),
		OpenAIAdvancedSchedulerEnabled: func() bool {
			if req.OpenAIAdvancedSchedulerEnabled != nil {
				return *req.OpenAIAdvancedSchedulerEnabled
			}
			return previousSettings.OpenAIAdvancedSchedulerEnabled
		}(),
		BalanceLowNotifyEnabled: func() bool {
			if req.BalanceLowNotifyEnabled != nil {
				return *req.BalanceLowNotifyEnabled
			}
			return previousSettings.BalanceLowNotifyEnabled
		}(),
		BalanceLowNotifyThreshold: func() float64 {
			if req.BalanceLowNotifyThreshold != nil {
				return *req.BalanceLowNotifyThreshold
			}
			return previousSettings.BalanceLowNotifyThreshold
		}(),
		BalanceLowNotifyRechargeURL: func() string {
			if req.BalanceLowNotifyRechargeURL != nil {
				return *req.BalanceLowNotifyRechargeURL
			}
			return previousSettings.BalanceLowNotifyRechargeURL
		}(),
		SubscriptionExpiryNotifyEnabled: func() bool {
			if req.SubscriptionExpiryNotifyEnabled != nil {
				return *req.SubscriptionExpiryNotifyEnabled
			}
			return previousSettings.SubscriptionExpiryNotifyEnabled
		}(),
		AccountQuotaNotifyEnabled: func() bool {
			if req.AccountQuotaNotifyEnabled != nil {
				return *req.AccountQuotaNotifyEnabled
			}
			return previousSettings.AccountQuotaNotifyEnabled
		}(),
		AccountQuotaNotifyEmails: func() []service.NotifyEmailEntry {
			if req.AccountQuotaNotifyEmails != nil {
				return dto.NotifyEmailEntriesToService(*req.AccountQuotaNotifyEmails)
			}
			return previousSettings.AccountQuotaNotifyEmails
		}(),
		ChannelMonitorEnabled: func() bool {
			if req.ChannelMonitorEnabled != nil {
				return *req.ChannelMonitorEnabled
			}
			return previousSettings.ChannelMonitorEnabled
		}(),
		ChannelMonitorDefaultIntervalSeconds: func() int {
			if req.ChannelMonitorDefaultIntervalSeconds != nil {
				return *req.ChannelMonitorDefaultIntervalSeconds
			}
			return previousSettings.ChannelMonitorDefaultIntervalSeconds
		}(),
		AvailableChannelsEnabled: func() bool {
			if req.AvailableChannelsEnabled != nil {
				return *req.AvailableChannelsEnabled
			}
			return previousSettings.AvailableChannelsEnabled
		}(),
		AffiliateEnabled: func() bool {
			if req.AffiliateEnabled != nil {
				return *req.AffiliateEnabled
			}
			return previousSettings.AffiliateEnabled
		}(),
		RiskControlEnabled: func() bool {
			if req.RiskControlEnabled != nil {
				return *req.RiskControlEnabled
			}
			return previousSettings.RiskControlEnabled
		}(),
		PrivacyFilterEnabled: func() bool {
			if req.PrivacyFilterEnabled != nil {
				return *req.PrivacyFilterEnabled
			}
			return previousSettings.PrivacyFilterEnabled
		}(),
		DeploymentMode: func() string {
			if req.DeploymentMode != nil {
				return service.NormalizeDeploymentMode(*req.DeploymentMode)
			}
			return previousSettings.DeploymentMode
		}(),
		AnnouncementsEnabled: func() bool {
			if req.AnnouncementsEnabled != nil {
				return *req.AnnouncementsEnabled
			}
			return previousSettings.AnnouncementsEnabled
		}(),
		RedeemEnabled: func() bool {
			if req.RedeemEnabled != nil {
				return *req.RedeemEnabled
			}
			return previousSettings.RedeemEnabled
		}(),
		PromoEnabled: func() bool {
			if req.PromoEnabled != nil {
				return *req.PromoEnabled
			}
			return previousSettings.PromoEnabled
		}(),
	}

	// req.AuthSourceXxxPlatformQuotas 为 nil 表示本次请求未包含该 source 的 quota 配置（保留 previousAuthSourceDefaults 中的值）；
	// non-nil（含 empty map）表示整体覆盖：empty map = 清空该 source 的所有 quota 配置。
	authSourceDefaults := &service.AuthSourceDefaultSettings{
		Email: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultEmailBalance, previousAuthSourceDefaults.Email.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultEmailConcurrency, previousAuthSourceDefaults.Email.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultEmailSubscriptions, previousAuthSourceDefaults.Email.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultEmailGrantOnSignup, previousAuthSourceDefaults.Email.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultEmailGrantOnFirstBind, previousAuthSourceDefaults.Email.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceEmailPlatformQuotas, previousAuthSourceDefaults.Email.PlatformQuotas),
		},
		LinuxDo: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultLinuxDoBalance, previousAuthSourceDefaults.LinuxDo.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultLinuxDoConcurrency, previousAuthSourceDefaults.LinuxDo.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultLinuxDoSubscriptions, previousAuthSourceDefaults.LinuxDo.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultLinuxDoGrantOnSignup, previousAuthSourceDefaults.LinuxDo.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultLinuxDoGrantOnFirstBind, previousAuthSourceDefaults.LinuxDo.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceLinuxDoPlatformQuotas, previousAuthSourceDefaults.LinuxDo.PlatformQuotas),
		},
		OIDC: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultOIDCBalance, previousAuthSourceDefaults.OIDC.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultOIDCConcurrency, previousAuthSourceDefaults.OIDC.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultOIDCSubscriptions, previousAuthSourceDefaults.OIDC.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultOIDCGrantOnSignup, previousAuthSourceDefaults.OIDC.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultOIDCGrantOnFirstBind, previousAuthSourceDefaults.OIDC.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceOIDCPlatformQuotas, previousAuthSourceDefaults.OIDC.PlatformQuotas),
		},
		WeChat: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultWeChatBalance, previousAuthSourceDefaults.WeChat.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultWeChatConcurrency, previousAuthSourceDefaults.WeChat.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultWeChatSubscriptions, previousAuthSourceDefaults.WeChat.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultWeChatGrantOnSignup, previousAuthSourceDefaults.WeChat.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultWeChatGrantOnFirstBind, previousAuthSourceDefaults.WeChat.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceWeChatPlatformQuotas, previousAuthSourceDefaults.WeChat.PlatformQuotas),
		},
		GitHub: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultGitHubBalance, previousAuthSourceDefaults.GitHub.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultGitHubConcurrency, previousAuthSourceDefaults.GitHub.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultGitHubSubscriptions, previousAuthSourceDefaults.GitHub.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultGitHubGrantOnSignup, previousAuthSourceDefaults.GitHub.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultGitHubGrantOnFirstBind, previousAuthSourceDefaults.GitHub.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceGitHubPlatformQuotas, previousAuthSourceDefaults.GitHub.PlatformQuotas),
		},
		Google: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultGoogleBalance, previousAuthSourceDefaults.Google.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultGoogleConcurrency, previousAuthSourceDefaults.Google.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultGoogleSubscriptions, previousAuthSourceDefaults.Google.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultGoogleGrantOnSignup, previousAuthSourceDefaults.Google.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultGoogleGrantOnFirstBind, previousAuthSourceDefaults.Google.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceGooglePlatformQuotas, previousAuthSourceDefaults.Google.PlatformQuotas),
		},
		DingTalk: service.ProviderDefaultGrantSettings{
			Balance:          float64ValueOrDefault(req.AuthSourceDefaultDingTalkBalance, previousAuthSourceDefaults.DingTalk.Balance),
			Concurrency:      intValueOrDefault(req.AuthSourceDefaultDingTalkConcurrency, previousAuthSourceDefaults.DingTalk.Concurrency),
			Subscriptions:    defaultSubscriptionsValueOrDefault(req.AuthSourceDefaultDingTalkSubscriptions, previousAuthSourceDefaults.DingTalk.Subscriptions),
			GrantOnSignup:    boolValueOrDefault(req.AuthSourceDefaultDingTalkGrantOnSignup, previousAuthSourceDefaults.DingTalk.GrantOnSignup),
			GrantOnFirstBind: boolValueOrDefault(req.AuthSourceDefaultDingTalkGrantOnFirstBind, previousAuthSourceDefaults.DingTalk.GrantOnFirstBind),
			PlatformQuotas:   platformQuotasValueOrDefault(req.AuthSourceDingTalkPlatformQuotas, previousAuthSourceDefaults.DingTalk.PlatformQuotas),
		},
		ForceEmailOnThirdPartySignup: boolValueOrDefault(req.ForceEmailOnThirdPartySignup, previousAuthSourceDefaults.ForceEmailOnThirdPartySignup),
	}
	if err := h.settingService.UpdateSettingsWithAuthSourceDefaults(c.Request.Context(), settings, authSourceDefaults); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Update OpenAI fast policy (stored under dedicated key, only when provided).
	if req.OpenAIFastPolicySettings != nil {
		if err := h.settingService.SetOpenAIFastPolicySettings(c.Request.Context(), openaiFastPolicySettingsFromDTO(req.OpenAIFastPolicySettings)); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	// Update payment configuration (integrated into system settings).
	// Skip if no payment fields were provided (prevents accidental wipe).
	if h.paymentConfigService != nil && hasPaymentFields(req) {
		paymentReq := service.UpdatePaymentConfigRequest{
			Enabled:                   req.PaymentEnabled,
			MinAmount:                 req.PaymentMinAmount,
			MaxAmount:                 req.PaymentMaxAmount,
			DailyLimit:                req.PaymentDailyLimit,
			OrderTimeoutMin:           req.PaymentOrderTimeoutMin,
			MaxPendingOrders:          req.PaymentMaxPendingOrders,
			EnabledTypes:              req.PaymentEnabledTypes,
			BalanceDisabled:           req.PaymentBalanceDisabled,
			BalanceRechargeMultiplier: req.PaymentBalanceRechargeMultiplier,
			RechargeFeeRate:           req.PaymentRechargeFeeRate,
			LoadBalanceStrategy:       req.PaymentLoadBalanceStrat,
			ProductNamePrefix:         req.PaymentProductNamePrefix,
			ProductNameSuffix:         req.PaymentProductNameSuffix,
			HelpImageURL:              req.PaymentHelpImageURL,
			HelpText:                  req.PaymentHelpText,
			CancelRateLimitEnabled:    req.PaymentCancelRateLimitEnabled,
			CancelRateLimitMax:        req.PaymentCancelRateLimitMax,
			CancelRateLimitWindow:     req.PaymentCancelRateLimitWindow,
			CancelRateLimitUnit:       req.PaymentCancelRateLimitUnit,
			CancelRateLimitMode:       req.PaymentCancelRateLimitMode,
			AlipayForceQRCode:         req.PaymentAlipayForceQRCode,
		}
		if err := h.paymentConfigService.UpdatePaymentConfig(c.Request.Context(), paymentReq); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		// Refresh in-memory provider registry so config changes take effect immediately
		if h.paymentService != nil {
			h.paymentService.RefreshProviders(c.Request.Context())
		}
	}

	h.auditSettingsUpdate(c, previousSettings, settings, previousAuthSourceDefaults, authSourceDefaults, req)

	// 重新获取设置返回
	updatedSettings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.ensureDingTalkSyncAttributes(c.Request.Context(), updatedSettings)
	updatedAuthSourceDefaults, err := h.settingService.GetAuthSourceDefaultSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	updatedDefaultSubscriptions := make([]dto.DefaultSubscriptionSetting, 0, len(updatedSettings.DefaultSubscriptions))
	for _, sub := range updatedSettings.DefaultSubscriptions {
		updatedDefaultSubscriptions = append(updatedDefaultSubscriptions, dto.DefaultSubscriptionSetting{
			GroupID:      sub.GroupID,
			ValidityDays: sub.ValidityDays,
		})
	}

	// Reload payment config for response
	var updatedPaymentCfg *service.PaymentConfig
	if h.paymentConfigService != nil {
		updatedPaymentCfg, _ = h.paymentConfigService.GetPaymentConfig(c.Request.Context())
	}
	if updatedPaymentCfg == nil {
		updatedPaymentCfg = &service.PaymentConfig{}
	}

	payload := dto.SystemSettings{
		RegistrationEnabled:                    updatedSettings.RegistrationEnabled,
		EmailVerifyEnabled:                     updatedSettings.EmailVerifyEnabled,
		RegistrationEmailSuffixWhitelist:       updatedSettings.RegistrationEmailSuffixWhitelist,
		PromoCodeEnabled:                       updatedSettings.PromoCodeEnabled,
		PasswordResetEnabled:                   updatedSettings.PasswordResetEnabled,
		FrontendURL:                            updatedSettings.FrontendURL,
		InvitationCodeEnabled:                  updatedSettings.InvitationCodeEnabled,
		TotpEnabled:                            updatedSettings.TotpEnabled,
		TotpEncryptionKeyConfigured:            h.settingService.IsTotpEncryptionKeyConfigured(),
		LoginAgreementEnabled:                  updatedSettings.LoginAgreementEnabled,
		LoginAgreementMode:                     updatedSettings.LoginAgreementMode,
		LoginAgreementUpdatedAt:                updatedSettings.LoginAgreementUpdatedAt,
		LoginAgreementDocuments:                loginAgreementDocumentsToDTO(updatedSettings.LoginAgreementDocuments),
		SMTPHost:                               updatedSettings.SMTPHost,
		SMTPPort:                               updatedSettings.SMTPPort,
		SMTPUsername:                           updatedSettings.SMTPUsername,
		SMTPPasswordConfigured:                 updatedSettings.SMTPPasswordConfigured,
		SMTPFrom:                               updatedSettings.SMTPFrom,
		SMTPFromName:                           updatedSettings.SMTPFromName,
		SMTPUseTLS:                             updatedSettings.SMTPUseTLS,
		TurnstileEnabled:                       updatedSettings.TurnstileEnabled,
		TurnstileSiteKey:                       updatedSettings.TurnstileSiteKey,
		TurnstileSecretKeyConfigured:           updatedSettings.TurnstileSecretKeyConfigured,
		APIKeyACLTrustForwardedIP:              updatedSettings.APIKeyACLTrustForwardedIP,
		LinuxDoConnectEnabled:                  updatedSettings.LinuxDoConnectEnabled,
		LinuxDoConnectClientID:                 updatedSettings.LinuxDoConnectClientID,
		LinuxDoConnectClientSecretConfigured:   updatedSettings.LinuxDoConnectClientSecretConfigured,
		LinuxDoConnectRedirectURL:              updatedSettings.LinuxDoConnectRedirectURL,
		DingTalkConnectEnabled:                 updatedSettings.DingTalkConnectEnabled,
		DingTalkConnectClientID:                updatedSettings.DingTalkConnectClientID,
		DingTalkConnectClientSecretConfigured:  updatedSettings.DingTalkConnectClientSecretConfigured,
		DingTalkConnectRedirectURL:             updatedSettings.DingTalkConnectRedirectURL,
		DingTalkConnectCorpRestrictionPolicy:   updatedSettings.DingTalkConnectCorpRestrictionPolicy,
		DingTalkConnectInternalCorpID:          updatedSettings.DingTalkConnectInternalCorpID,
		DingTalkConnectBypassRegistration:      updatedSettings.DingTalkConnectBypassRegistration,
		DingTalkConnectSyncCorpEmail:           updatedSettings.DingTalkConnectSyncCorpEmail,
		DingTalkConnectSyncDisplayName:         updatedSettings.DingTalkConnectSyncDisplayName,
		DingTalkConnectSyncDept:                updatedSettings.DingTalkConnectSyncDept,
		DingTalkConnectSyncCorpEmailAttrKey:    updatedSettings.DingTalkConnectSyncCorpEmailAttrKey,
		DingTalkConnectSyncDisplayNameAttrKey:  updatedSettings.DingTalkConnectSyncDisplayNameAttrKey,
		DingTalkConnectSyncDeptAttrKey:         updatedSettings.DingTalkConnectSyncDeptAttrKey,
		DingTalkConnectSyncCorpEmailAttrName:   updatedSettings.DingTalkConnectSyncCorpEmailAttrName,
		DingTalkConnectSyncDisplayNameAttrName: updatedSettings.DingTalkConnectSyncDisplayNameAttrName,
		DingTalkConnectSyncDeptAttrName:        updatedSettings.DingTalkConnectSyncDeptAttrName,
		WeChatConnectEnabled:                   updatedSettings.WeChatConnectEnabled,
		WeChatConnectAppID:                     updatedSettings.WeChatConnectAppID,
		WeChatConnectAppSecretConfigured:       updatedSettings.WeChatConnectAppSecretConfigured,
		WeChatConnectOpenAppID:                 updatedSettings.WeChatConnectOpenAppID,
		WeChatConnectOpenAppSecretConfigured:   updatedSettings.WeChatConnectOpenAppSecretConfigured,
		WeChatConnectMPAppID:                   updatedSettings.WeChatConnectMPAppID,
		WeChatConnectMPAppSecretConfigured:     updatedSettings.WeChatConnectMPAppSecretConfigured,
		WeChatConnectMobileAppID:               updatedSettings.WeChatConnectMobileAppID,
		WeChatConnectMobileAppSecretConfigured: updatedSettings.WeChatConnectMobileAppSecretConfigured,
		WeChatConnectOpenEnabled:               updatedSettings.WeChatConnectOpenEnabled,
		WeChatConnectMPEnabled:                 updatedSettings.WeChatConnectMPEnabled,
		WeChatConnectMobileEnabled:             updatedSettings.WeChatConnectMobileEnabled,
		WeChatConnectMode:                      updatedSettings.WeChatConnectMode,
		WeChatConnectScopes:                    updatedSettings.WeChatConnectScopes,
		WeChatConnectRedirectURL:               updatedSettings.WeChatConnectRedirectURL,
		WeChatConnectFrontendRedirectURL:       updatedSettings.WeChatConnectFrontendRedirectURL,
		OIDCConnectEnabled:                     updatedSettings.OIDCConnectEnabled,
		OIDCConnectProviderName:                updatedSettings.OIDCConnectProviderName,
		OIDCConnectClientID:                    updatedSettings.OIDCConnectClientID,
		OIDCConnectClientSecretConfigured:      updatedSettings.OIDCConnectClientSecretConfigured,
		OIDCConnectIssuerURL:                   updatedSettings.OIDCConnectIssuerURL,
		OIDCConnectDiscoveryURL:                updatedSettings.OIDCConnectDiscoveryURL,
		OIDCConnectAuthorizeURL:                updatedSettings.OIDCConnectAuthorizeURL,
		OIDCConnectTokenURL:                    updatedSettings.OIDCConnectTokenURL,
		OIDCConnectUserInfoURL:                 updatedSettings.OIDCConnectUserInfoURL,
		OIDCConnectJWKSURL:                     updatedSettings.OIDCConnectJWKSURL,
		OIDCConnectScopes:                      updatedSettings.OIDCConnectScopes,
		OIDCConnectRedirectURL:                 updatedSettings.OIDCConnectRedirectURL,
		OIDCConnectFrontendRedirectURL:         updatedSettings.OIDCConnectFrontendRedirectURL,
		OIDCConnectTokenAuthMethod:             updatedSettings.OIDCConnectTokenAuthMethod,
		OIDCConnectUsePKCE:                     updatedSettings.OIDCConnectUsePKCE,
		OIDCConnectValidateIDToken:             updatedSettings.OIDCConnectValidateIDToken,
		OIDCConnectAllowedSigningAlgs:          updatedSettings.OIDCConnectAllowedSigningAlgs,
		OIDCConnectClockSkewSeconds:            updatedSettings.OIDCConnectClockSkewSeconds,
		OIDCConnectRequireEmailVerified:        updatedSettings.OIDCConnectRequireEmailVerified,
		OIDCConnectUserInfoEmailPath:           updatedSettings.OIDCConnectUserInfoEmailPath,
		OIDCConnectUserInfoIDPath:              updatedSettings.OIDCConnectUserInfoIDPath,
		OIDCConnectUserInfoUsernamePath:        updatedSettings.OIDCConnectUserInfoUsernamePath,
		GitHubOAuthEnabled:                     updatedSettings.GitHubOAuthEnabled,
		GitHubOAuthClientID:                    updatedSettings.GitHubOAuthClientID,
		GitHubOAuthClientSecretConfigured:      updatedSettings.GitHubOAuthClientSecretConfigured,
		GitHubOAuthRedirectURL:                 updatedSettings.GitHubOAuthRedirectURL,
		GitHubOAuthFrontendRedirectURL:         updatedSettings.GitHubOAuthFrontendRedirectURL,
		GoogleOAuthEnabled:                     updatedSettings.GoogleOAuthEnabled,
		GoogleOAuthClientID:                    updatedSettings.GoogleOAuthClientID,
		GoogleOAuthClientSecretConfigured:      updatedSettings.GoogleOAuthClientSecretConfigured,
		GoogleOAuthRedirectURL:                 updatedSettings.GoogleOAuthRedirectURL,
		GoogleOAuthFrontendRedirectURL:         updatedSettings.GoogleOAuthFrontendRedirectURL,
		SiteName:                               updatedSettings.SiteName,
		SiteLogo:                               updatedSettings.SiteLogo,
		SiteSubtitle:                           updatedSettings.SiteSubtitle,
		APIBaseURL:                             updatedSettings.APIBaseURL,
		ContactInfo:                            updatedSettings.ContactInfo,
		DocURL:                                 updatedSettings.DocURL,
		HomeContent:                            updatedSettings.HomeContent,
		HideCcsImportButton:                    updatedSettings.HideCcsImportButton,
		PurchaseSubscriptionEnabled:            updatedSettings.PurchaseSubscriptionEnabled,
		PurchaseSubscriptionURL:                updatedSettings.PurchaseSubscriptionURL,
		TableDefaultPageSize:                   updatedSettings.TableDefaultPageSize,
		TablePageSizeOptions:                   updatedSettings.TablePageSizeOptions,
		CustomMenuItems:                        dto.ParseCustomMenuItems(updatedSettings.CustomMenuItems),
		CustomEndpoints:                        dto.ParseCustomEndpoints(updatedSettings.CustomEndpoints),
		DefaultConcurrency:                     updatedSettings.DefaultConcurrency,
		DefaultBalance:                         updatedSettings.DefaultBalance,
		AffiliateRebateRate:                    updatedSettings.AffiliateRebateRate,
		AffiliateRebateFreezeHours:             updatedSettings.AffiliateRebateFreezeHours,
		AffiliateRebateDurationDays:            updatedSettings.AffiliateRebateDurationDays,
		AffiliateRebatePerInviteeCap:           updatedSettings.AffiliateRebatePerInviteeCap,
		DefaultUserRPMLimit:                    updatedSettings.DefaultUserRPMLimit,
		DefaultSubscriptions:                   updatedDefaultSubscriptions,
		EnableModelFallback:                    updatedSettings.EnableModelFallback,
		FallbackModelAnthropic:                 updatedSettings.FallbackModelAnthropic,
		FallbackModelOpenAI:                    updatedSettings.FallbackModelOpenAI,
		FallbackModelGemini:                    updatedSettings.FallbackModelGemini,
		FallbackModelAntigravity:               updatedSettings.FallbackModelAntigravity,
		EnableIdentityPatch:                    updatedSettings.EnableIdentityPatch,
		IdentityPatchPrompt:                    updatedSettings.IdentityPatchPrompt,
		OpsMonitoringEnabled:                   updatedSettings.OpsMonitoringEnabled,
		OpsRealtimeMonitoringEnabled:           updatedSettings.OpsRealtimeMonitoringEnabled,
		OpsQueryModeDefault:                    updatedSettings.OpsQueryModeDefault,
		OpsMetricsIntervalSeconds:              updatedSettings.OpsMetricsIntervalSeconds,
		MinClaudeCodeVersion:                   updatedSettings.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:                   updatedSettings.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling:            updatedSettings.AllowUngroupedKeyScheduling,
		BackendModeEnabled:                     updatedSettings.BackendModeEnabled,
		EnableFingerprintUnification:           updatedSettings.EnableFingerprintUnification,
		EnableMetadataPassthrough:              updatedSettings.EnableMetadataPassthrough,
		EnableCCHSigning:                       updatedSettings.EnableCCHSigning,
		EnableAnthropicCacheTTL1hInjection:     updatedSettings.EnableAnthropicCacheTTL1hInjection,
		RewriteMessageCacheControl:             updatedSettings.RewriteMessageCacheControl,
		AntigravityUserAgentVersion:            updatedSettings.AntigravityUserAgentVersion,
		OpenAICodexUserAgent:                   updatedSettings.OpenAICodexUserAgent,
		OpenAIAllowClaudeCodeCodexPlugin:       updatedSettings.OpenAIAllowClaudeCodeCodexPlugin,
		PaymentVisibleMethodAlipaySource:       updatedSettings.PaymentVisibleMethodAlipaySource,
		PaymentVisibleMethodWxpaySource:        updatedSettings.PaymentVisibleMethodWxpaySource,
		PaymentVisibleMethodAlipayEnabled:      updatedSettings.PaymentVisibleMethodAlipayEnabled,
		PaymentVisibleMethodWxpayEnabled:       updatedSettings.PaymentVisibleMethodWxpayEnabled,
		OpenAIAdvancedSchedulerEnabled:         updatedSettings.OpenAIAdvancedSchedulerEnabled,
		BalanceLowNotifyEnabled:                updatedSettings.BalanceLowNotifyEnabled,
		BalanceLowNotifyThreshold:              updatedSettings.BalanceLowNotifyThreshold,
		BalanceLowNotifyRechargeURL:            updatedSettings.BalanceLowNotifyRechargeURL,
		SubscriptionExpiryNotifyEnabled:        updatedSettings.SubscriptionExpiryNotifyEnabled,
		AccountQuotaNotifyEnabled:              updatedSettings.AccountQuotaNotifyEnabled,
		AccountQuotaNotifyEmails:               dto.NotifyEmailEntriesFromService(updatedSettings.AccountQuotaNotifyEmails),
		PaymentEnabled:                         updatedPaymentCfg.Enabled,
		PaymentMinAmount:                       updatedPaymentCfg.MinAmount,
		PaymentMaxAmount:                       updatedPaymentCfg.MaxAmount,
		PaymentDailyLimit:                      updatedPaymentCfg.DailyLimit,
		PaymentOrderTimeoutMin:                 updatedPaymentCfg.OrderTimeoutMin,
		PaymentMaxPendingOrders:                updatedPaymentCfg.MaxPendingOrders,
		PaymentEnabledTypes:                    updatedPaymentCfg.EnabledTypes,
		PaymentBalanceDisabled:                 updatedPaymentCfg.BalanceDisabled,
		PaymentBalanceRechargeMultiplier:       updatedPaymentCfg.BalanceRechargeMultiplier,
		PaymentRechargeFeeRate:                 updatedPaymentCfg.RechargeFeeRate,
		PaymentLoadBalanceStrat:                updatedPaymentCfg.LoadBalanceStrategy,
		PaymentProductNamePrefix:               updatedPaymentCfg.ProductNamePrefix,
		PaymentProductNameSuffix:               updatedPaymentCfg.ProductNameSuffix,
		PaymentHelpImageURL:                    updatedPaymentCfg.HelpImageURL,
		PaymentHelpText:                        updatedPaymentCfg.HelpText,
		PaymentCancelRateLimitEnabled:          updatedPaymentCfg.CancelRateLimitEnabled,
		PaymentCancelRateLimitMax:              updatedPaymentCfg.CancelRateLimitMax,
		PaymentCancelRateLimitWindow:           updatedPaymentCfg.CancelRateLimitWindow,
		PaymentCancelRateLimitUnit:             updatedPaymentCfg.CancelRateLimitUnit,
		PaymentCancelRateLimitMode:             updatedPaymentCfg.CancelRateLimitMode,
		PaymentAlipayForceQRCode:               updatedPaymentCfg.AlipayForceQRCode,

		ChannelMonitorEnabled:                updatedSettings.ChannelMonitorEnabled,
		ChannelMonitorDefaultIntervalSeconds: updatedSettings.ChannelMonitorDefaultIntervalSeconds,

		AvailableChannelsEnabled: updatedSettings.AvailableChannelsEnabled,

		AffiliateEnabled: updatedSettings.AffiliateEnabled,

		RiskControlEnabled:   updatedSettings.RiskControlEnabled,
		PrivacyFilterEnabled: updatedSettings.PrivacyFilterEnabled,
		DeploymentMode:       updatedSettings.DeploymentMode,
	}
	if fastPolicy, err := h.settingService.GetOpenAIFastPolicySettings(c.Request.Context()); err != nil {
		slog.Error("openai_fast_policy_settings_get_failed", "error", err)
	} else if fastPolicy != nil {
		payload.OpenAIFastPolicySettings = openaiFastPolicySettingsToDTO(fastPolicy)
	}

	// Default platform quotas（JSON map）—— 与 GetSettings 一致，避免保存后响应缺失该字段
	if platformQuotas, err := h.settingService.GetDefaultPlatformQuotas(c.Request.Context()); err != nil {
		slog.Error("default_platform_quotas_get_failed", "error", err)
	} else {
		payload.DefaultPlatformQuotas = platformQuotas
	}
	response.Success(c, systemSettingsResponseData(payload, updatedAuthSourceDefaults))
}
