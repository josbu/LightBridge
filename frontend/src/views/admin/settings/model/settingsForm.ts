import { normalizePlatformQuotasMap } from "@/api/admin/settings";
import type {
  DefaultPlatformQuotasMap,
  SystemSettings,
} from "@/api/admin/settings";
import type { NotifyEmailEntry } from "@/types";
import {
  defaultLoginAgreementDocuments,
  TABLE_PAGE_SIZE_DEFAULT,
} from "./settingsViewModel";

export type SettingsForm = Omit<
  SystemSettings,
  | "wechat_connect_open_enabled"
  | "wechat_connect_mp_enabled"
  | "wechat_connect_mobile_enabled"
> & {
  smtp_password: string;
  turnstile_secret_key: string;
  linuxdo_connect_client_secret: string;
  dingtalk_connect_client_secret: string;
  wechat_connect_app_secret: string;
  wechat_connect_open_app_secret: string;
  wechat_connect_mp_app_secret: string;
  wechat_connect_mobile_app_secret: string;
  wechat_connect_open_enabled: boolean;
  wechat_connect_mp_enabled: boolean;
  wechat_connect_mobile_enabled: boolean;
  oidc_connect_client_secret: string;
  github_oauth_client_secret: string;
  google_oauth_client_secret: string;
  force_email_on_third_party_signup: boolean;
  openai_advanced_scheduler_enabled: boolean;
  // 系统全局平台限额 map；form 内始终归一化为完整平台对象（模板非空绑定依赖此不变量）
  default_platform_quotas: DefaultPlatformQuotasMap;
};

export function createSettingsForm(): SettingsForm {
  return {
    registration_enabled: true,
    email_verify_enabled: false,
    registration_email_suffix_whitelist: [],
    promo_code_enabled: true,
    invitation_code_enabled: false,
    password_reset_enabled: false,
    totp_enabled: false,
    totp_encryption_key_configured: false,
    login_agreement_enabled: false,
    login_agreement_mode: "modal",
    login_agreement_updated_at: "2026-03-31",
    login_agreement_documents: defaultLoginAgreementDocuments(),
    default_balance: 0,
    default_platform_quotas:
      normalizePlatformQuotasMap() as DefaultPlatformQuotasMap,
    affiliate_rebate_rate: 20,
    affiliate_rebate_freeze_hours: 0,
    affiliate_rebate_duration_days: 0,
    affiliate_rebate_per_invitee_cap: 0,
    default_concurrency: 1,
    default_subscriptions: [],
    force_email_on_third_party_signup: false,
    default_user_rpm_limit: 0,
    site_name: "LightBridge",
    site_logo: "",
    site_subtitle: "AI Gateway Control Plane",
    api_base_url: "",
    contact_info: "",
    doc_url: "",
    home_content: "",
    backend_mode_enabled: false,
    hide_ccs_import_button: false,
    payment_enabled: false,
    risk_control_enabled: false,
    privacy_filter_enabled: false,
    deployment_mode: 'distribution',
    payment_min_amount: 1,
    payment_max_amount: 10000,
    payment_daily_limit: 50000,
    payment_max_pending_orders: 3,
    payment_order_timeout_minutes: 30,
    payment_balance_disabled: false,
    payment_balance_recharge_multiplier: 1,
    payment_recharge_fee_rate: 0,
    payment_enabled_types: [],
    payment_help_image_url: "",
    payment_help_text: "",
    payment_product_name_prefix: "",
    payment_product_name_suffix: "",
    payment_load_balance_strategy: "round-robin",
    payment_cancel_rate_limit_enabled: false,
    payment_cancel_rate_limit_max: 10,
    payment_cancel_rate_limit_window: 1,
    payment_cancel_rate_limit_unit: "day",
    payment_cancel_rate_limit_window_mode: "rolling",
    payment_alipay_force_qrcode: false,
    table_default_page_size: TABLE_PAGE_SIZE_DEFAULT,
    table_page_size_options: [10, 20, 50, 100],
    custom_menu_items: [] as Array<{
      id: string;
      label: string;
      icon_svg: string;
      url: string;
      visibility: "user" | "admin";
      sort_order: number;
    }>,
    custom_endpoints: [] as Array<{
      name: string;
      endpoint: string;
      description: string;
    }>,
    frontend_url: "",
    smtp_host: "",
    smtp_port: 587,
    smtp_username: "",
    smtp_password: "",
    smtp_password_configured: false,
    smtp_from_email: "",
    smtp_from_name: "",
    smtp_use_tls: true,
    // Cloudflare Turnstile
    turnstile_enabled: false,
    turnstile_site_key: "",
    turnstile_secret_key: "",
    turnstile_secret_key_configured: false,
    api_key_acl_trust_forwarded_ip: false,
    // LinuxDo Connect OAuth 登录
    linuxdo_connect_enabled: false,
    linuxdo_connect_client_id: "",
    linuxdo_connect_client_secret: "",
    linuxdo_connect_client_secret_configured: false,
    linuxdo_connect_redirect_url: "",
    // DingTalk Connect OAuth 登录
    dingtalk_connect_enabled: false,
    dingtalk_connect_client_id: "",
    dingtalk_connect_client_secret: "",
    dingtalk_connect_client_secret_configured: false,
    dingtalk_connect_redirect_url: "",
    dingtalk_connect_corp_restriction_policy: "none",
    dingtalk_connect_internal_corp_id: "",
    dingtalk_connect_bypass_registration: false,
    dingtalk_connect_sync_corp_email: false,
    dingtalk_connect_sync_display_name: false,
    dingtalk_connect_sync_dept: false,
    dingtalk_connect_sync_corp_email_attr_key: "dingtalk_email",
    dingtalk_connect_sync_display_name_attr_key: "dingtalk_name",
    dingtalk_connect_sync_dept_attr_key: "dingtalk_department",
    dingtalk_connect_sync_corp_email_attr_name: "钉钉企业邮箱",
    dingtalk_connect_sync_display_name_attr_name: "钉钉姓名",
    dingtalk_connect_sync_dept_attr_name: "钉钉部门",
    wechat_connect_enabled: false,
    wechat_connect_app_id: "",
    wechat_connect_app_secret: "",
    wechat_connect_app_secret_configured: false,
    wechat_connect_open_app_id: "",
    wechat_connect_open_app_secret: "",
    wechat_connect_open_app_secret_configured: false,
    wechat_connect_mp_app_id: "",
    wechat_connect_mp_app_secret: "",
    wechat_connect_mp_app_secret_configured: false,
    wechat_connect_mobile_app_id: "",
    wechat_connect_mobile_app_secret: "",
    wechat_connect_mobile_app_secret_configured: false,
    wechat_connect_open_enabled: false,
    wechat_connect_mp_enabled: false,
    wechat_connect_mobile_enabled: false,
    wechat_connect_mode: "open",
    wechat_connect_scopes: "snsapi_login",
    wechat_connect_redirect_url: "",
    wechat_connect_frontend_redirect_url: "/auth/wechat/callback",
    // Generic OIDC OAuth 登录
    oidc_connect_enabled: false,
    oidc_connect_provider_name: "OIDC",
    oidc_connect_client_id: "",
    oidc_connect_client_secret: "",
    oidc_connect_client_secret_configured: false,
    oidc_connect_issuer_url: "",
    oidc_connect_discovery_url: "",
    oidc_connect_authorize_url: "",
    oidc_connect_token_url: "",
    oidc_connect_userinfo_url: "",
    oidc_connect_jwks_url: "",
    oidc_connect_scopes: "openid email profile",
    oidc_connect_redirect_url: "",
    oidc_connect_frontend_redirect_url: "/auth/oidc/callback",
    oidc_connect_token_auth_method: "client_secret_post",
    oidc_connect_use_pkce: false,
    oidc_connect_validate_id_token: false,
    oidc_connect_allowed_signing_algs: "RS256,ES256,PS256",
    oidc_connect_clock_skew_seconds: 120,
    oidc_connect_require_email_verified: false,
    oidc_connect_userinfo_email_path: "",
    oidc_connect_userinfo_id_path: "",
    oidc_connect_userinfo_username_path: "",
    // GitHub / Google 邮箱快捷登录
    github_oauth_enabled: false,
    github_oauth_client_id: "",
    github_oauth_client_secret: "",
    github_oauth_client_secret_configured: false,
    github_oauth_redirect_url: "",
    github_oauth_frontend_redirect_url: "/auth/oauth/callback",
    google_oauth_enabled: false,
    google_oauth_client_id: "",
    google_oauth_client_secret: "",
    google_oauth_client_secret_configured: false,
    google_oauth_redirect_url: "",
    google_oauth_frontend_redirect_url: "/auth/oauth/callback",
    // Model fallback
    enable_model_fallback: false,
    fallback_model_anthropic: "claude-3-5-sonnet-20241022",
    fallback_model_openai: "gpt-4o",
    fallback_model_gemini: "gemini-2.5-pro",
    fallback_model_antigravity: "gemini-2.5-pro",
    // Identity patch (Claude -> Gemini)
    enable_identity_patch: true,
    identity_patch_prompt: "",
    // Ops monitoring (vNext)
    ops_monitoring_enabled: true,
    ops_realtime_monitoring_enabled: true,
    ops_query_mode_default: "auto",
    ops_metrics_interval_seconds: 60,
    // Claude Code version check
    min_claude_code_version: "",
    max_claude_code_version: "",
    // 分组隔离
    allow_ungrouped_key_scheduling: false,
    openai_advanced_scheduler_enabled: false,
    // Gateway forwarding behavior
    enable_fingerprint_unification: true,
    enable_metadata_passthrough: false,
    enable_cch_signing: false,
    enable_anthropic_cache_ttl_1h_injection: false,
    rewrite_message_cache_control: false,
    antigravity_user_agent_version: "",
    openai_codex_user_agent: "",
    openai_allow_claude_code_codex_plugin: false,
    // 余额、订阅到期与账号限额通知
    balance_low_notify_enabled: false,
    balance_low_notify_threshold: 0,
    balance_low_notify_recharge_url: "",
    subscription_expiry_notify_enabled: true,
    account_quota_notify_enabled: false,
    account_quota_notify_emails: [] as NotifyEmailEntry[],
    // Channel Monitor feature switch
    channel_monitor_enabled: true,
    channel_monitor_default_interval_seconds: 60,
    // Available Channels feature switch
    available_channels_enabled: false,
    // Affiliate (邀请返利) feature switch
    affiliate_enabled: false,
  };
}
