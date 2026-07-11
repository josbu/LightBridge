package config

const (
	RunModeStandard = "standard"
	RunModeSimple   = "simple"
)

const (
	LegacyModuleMarketplaceRegistryURL  = "https://github.com/WilliamWang1721/LightBridge/releases/download/module-migration-20260606/registry.json"
	DefaultModuleMarketplaceRegistryURL = "https://github.com/WilliamWang1721/LightBridge/releases/download/module-anthropic-oauth-provider-v0.1.0/registry.json"
)

// 使用量记录队列溢出策略（drop/sample 仅用于读取旧配置；运行时统一安全降级为 sync）
const (
	UsageRecordOverflowPolicyDrop   = "drop"
	UsageRecordOverflowPolicySample = "sample"
	UsageRecordOverflowPolicySync   = "sync"
)

// DefaultCSPPolicy is the default Content-Security-Policy with nonce support
// __CSP_NONCE__ will be replaced with actual nonce at request time by the SecurityHeaders middleware
const DefaultCSPPolicy = "default-src 'self'; script-src 'self' __CSP_NONCE__ https://challenges.cloudflare.com https://static.cloudflareinsights.com https://*.stripe.com https://static.airwallex.com https://checkout.airwallex.com https://static-demo.airwallex.com https://checkout-demo.airwallex.com; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://static.airwallex.com https://checkout.airwallex.com https://static-demo.airwallex.com https://checkout-demo.airwallex.com; img-src 'self' data: https:; font-src 'self' data: https://fonts.gstatic.com; connect-src 'self' https:; frame-src https://challenges.cloudflare.com https://*.stripe.com https://checkout.airwallex.com https://checkout-demo.airwallex.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// UMQ（用户消息队列）模式常量
const (
	// UMQModeSerialize: 账号级串行锁 + RPM 自适应延迟
	UMQModeSerialize = "serialize"
	// UMQModeThrottle: 仅 RPM 自适应前置延迟，不阻塞并发
	UMQModeThrottle = "throttle"
)

// 连接池隔离策略常量
// 用于控制上游 HTTP 连接池的隔离粒度，影响连接复用和资源消耗
const (
	// ConnectionPoolIsolationProxy: 按代理隔离
	// 同一代理地址共享连接池，适合代理数量少、账户数量多的场景
	ConnectionPoolIsolationProxy = "proxy"
	// ConnectionPoolIsolationAccount: 按账户隔离
	// 每个账户独立连接池，适合账户数量少、需要严格隔离的场景
	ConnectionPoolIsolationAccount = "account"
	// ConnectionPoolIsolationAccountProxy: 按账户+代理组合隔离（默认）
	// 同一账户+代理组合共享连接池，提供最细粒度的隔离
	ConnectionPoolIsolationAccountProxy = "account_proxy"
)

// DefaultUpstreamResponseReadMaxBytes 上游非流式响应体的默认读取上限。
// 128 MB 可容纳 2-3 张 4K PNG（base64 膨胀 33%，单张 4K PNG 最坏约 67MB base64）。
// 可通过 gateway.upstream_response_read_max_bytes 配置项覆盖。
const DefaultUpstreamResponseReadMaxBytes int64 = 128 * 1024 * 1024

type Config struct {
	Server                  ServerConfig                  `mapstructure:"server"`
	Log                     LogConfig                     `mapstructure:"log"`
	CORS                    CORSConfig                    `mapstructure:"cors"`
	Security                SecurityConfig                `mapstructure:"security"`
	Billing                 BillingConfig                 `mapstructure:"billing"`
	Turnstile               TurnstileConfig               `mapstructure:"turnstile"`
	Database                DatabaseConfig                `mapstructure:"database"`
	Redis                   RedisConfig                   `mapstructure:"redis"`
	Ops                     OpsConfig                     `mapstructure:"ops"`
	JWT                     JWTConfig                     `mapstructure:"jwt"`
	Totp                    TotpConfig                    `mapstructure:"totp"`
	LinuxDo                 LinuxDoConnectConfig          `mapstructure:"linuxdo_connect"`
	WeChat                  WeChatConnectConfig           `mapstructure:"wechat_connect"`
	OIDC                    OIDCConnectConfig             `mapstructure:"oidc_connect"`
	DingTalk                DingTalkConnectConfig         `mapstructure:"dingtalk_connect"`
	GitHubOAuth             EmailOAuthProviderConfig      `mapstructure:"github_oauth"`
	GoogleOAuth             EmailOAuthProviderConfig      `mapstructure:"google_oauth"`
	Default                 DefaultConfig                 `mapstructure:"default"`
	RateLimit               RateLimitConfig               `mapstructure:"rate_limit"`
	Pricing                 PricingConfig                 `mapstructure:"pricing"`
	Modules                 ModuleConfig                  `mapstructure:"modules"`
	Features                FeaturesConfig                `mapstructure:"features"`
	Gateway                 GatewayConfig                 `mapstructure:"gateway"`
	APIKeyAuth              APIKeyAuthCacheConfig         `mapstructure:"api_key_auth_cache"`
	SubscriptionCache       SubscriptionCacheConfig       `mapstructure:"subscription_cache"`
	SubscriptionMaintenance SubscriptionMaintenanceConfig `mapstructure:"subscription_maintenance"`
	Dashboard               DashboardCacheConfig          `mapstructure:"dashboard_cache"`
	DashboardAgg            DashboardAggregationConfig    `mapstructure:"dashboard_aggregation"`
	UsageCleanup            UsageCleanupConfig            `mapstructure:"usage_cleanup"`
	Concurrency             ConcurrencyConfig             `mapstructure:"concurrency"`
	TokenRefresh            TokenRefreshConfig            `mapstructure:"token_refresh"`
	RunMode                 string                        `mapstructure:"run_mode" yaml:"run_mode"`
	Timezone                string                        `mapstructure:"timezone"` // e.g. "Asia/Shanghai", "UTC"
	Language                string                        `mapstructure:"language"` // default language for API error messages: "en" or "zh"
	Gemini                  GeminiConfig                  `mapstructure:"gemini"`
	Update                  UpdateConfig                  `mapstructure:"update"`
	Idempotency             IdempotencyConfig             `mapstructure:"idempotency"`
}

type LogConfig struct {
	Level           string            `mapstructure:"level"`
	Format          string            `mapstructure:"format"`
	ServiceName     string            `mapstructure:"service_name"`
	Environment     string            `mapstructure:"env"`
	Caller          bool              `mapstructure:"caller"`
	StacktraceLevel string            `mapstructure:"stacktrace_level"`
	Output          LogOutputConfig   `mapstructure:"output"`
	Rotation        LogRotationConfig `mapstructure:"rotation"`
	Sampling        LogSamplingConfig `mapstructure:"sampling"`
}

type LogOutputConfig struct {
	ToStdout bool   `mapstructure:"to_stdout"`
	ToFile   bool   `mapstructure:"to_file"`
	FilePath string `mapstructure:"file_path"`
}

type LogRotationConfig struct {
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
	MaxBackups int  `mapstructure:"max_backups"`
	MaxAgeDays int  `mapstructure:"max_age_days"`
	Compress   bool `mapstructure:"compress"`
	LocalTime  bool `mapstructure:"local_time"`
}

type LogSamplingConfig struct {
	Enabled    bool `mapstructure:"enabled"`
	Initial    int  `mapstructure:"initial"`
	Thereafter int  `mapstructure:"thereafter"`
}

type GeminiConfig struct {
	OAuth GeminiOAuthConfig `mapstructure:"oauth"`
	Quota GeminiQuotaConfig `mapstructure:"quota"`
}

type GeminiOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	Scopes       string `mapstructure:"scopes"`
}

type GeminiQuotaConfig struct {
	Tiers  map[string]GeminiTierQuotaConfig `mapstructure:"tiers"`
	Policy string                           `mapstructure:"policy"`
}

type GeminiTierQuotaConfig struct {
	ProRPD          *int64 `mapstructure:"pro_rpd" json:"pro_rpd"`
	FlashRPD        *int64 `mapstructure:"flash_rpd" json:"flash_rpd"`
	CooldownMinutes *int   `mapstructure:"cooldown_minutes" json:"cooldown_minutes"`
}

type UpdateConfig struct {
	// ProxyURL 用于访问 GitHub 的代理地址
	// 支持 http/https/socks5/socks5h 协议
	// 例如: "http://127.0.0.1:7890", "socks5://127.0.0.1:1080"
	ProxyURL string `mapstructure:"proxy_url"`
}

type IdempotencyConfig struct {
	// ObserveOnly 为 true 时处于观察期：未携带 Idempotency-Key 的请求继续放行。
	ObserveOnly bool `mapstructure:"observe_only"`
	// DefaultTTLSeconds 关键写接口的幂等记录默认 TTL（秒）。
	DefaultTTLSeconds int `mapstructure:"default_ttl_seconds"`
	// SystemOperationTTLSeconds 系统操作接口的幂等记录 TTL（秒）。
	SystemOperationTTLSeconds int `mapstructure:"system_operation_ttl_seconds"`
	// ProcessingTimeoutSeconds processing 状态锁超时（秒）。
	ProcessingTimeoutSeconds int `mapstructure:"processing_timeout_seconds"`
	// FailedRetryBackoffSeconds 失败退避窗口（秒）。
	FailedRetryBackoffSeconds int `mapstructure:"failed_retry_backoff_seconds"`
	// MaxStoredResponseLen 持久化响应体最大长度（字节）。
	MaxStoredResponseLen int `mapstructure:"max_stored_response_len"`
	// CleanupIntervalSeconds 过期记录清理周期（秒）。
	CleanupIntervalSeconds int `mapstructure:"cleanup_interval_seconds"`
	// CleanupBatchSize 每次清理的最大记录数。
	CleanupBatchSize int `mapstructure:"cleanup_batch_size"`
}

type LinuxDoConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`          // 后端回调地址（需在提供方后台登记）
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"` // 前端接收 token 的路由（默认：/auth/linuxdo/callback）
	TokenAuthMethod     string `mapstructure:"token_auth_method"`     // client_secret_post / client_secret_basic / none
	UsePKCE             bool   `mapstructure:"use_pkce"`

	// 可选：用于从 userinfo JSON 中提取字段的 gjson 路径。
	// 为空时，服务端会尝试一组常见字段名。
	UserInfoEmailPath    string `mapstructure:"userinfo_email_path"`
	UserInfoIDPath       string `mapstructure:"userinfo_id_path"`
	UserInfoUsernamePath string `mapstructure:"userinfo_username_path"`
}

type WeChatConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	AppID               string `mapstructure:"app_id"`
	AppSecret           string `mapstructure:"app_secret"`
	OpenAppID           string `mapstructure:"open_app_id"`
	OpenAppSecret       string `mapstructure:"open_app_secret"`
	MPAppID             string `mapstructure:"mp_app_id"`
	MPAppSecret         string `mapstructure:"mp_app_secret"`
	MobileAppID         string `mapstructure:"mobile_app_id"`
	MobileAppSecret     string `mapstructure:"mobile_app_secret"`
	OpenEnabled         bool   `mapstructure:"open_enabled"`
	MPEnabled           bool   `mapstructure:"mp_enabled"`
	MobileEnabled       bool   `mapstructure:"mobile_enabled"`
	Mode                string `mapstructure:"mode"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`
}

type OIDCConnectConfig struct {
	Enabled                 bool   `mapstructure:"enabled"`
	ProviderName            string `mapstructure:"provider_name"` // 显示名: "Keycloak" 等
	ClientID                string `mapstructure:"client_id"`
	ClientSecret            string `mapstructure:"client_secret"`
	IssuerURL               string `mapstructure:"issuer_url"`
	DiscoveryURL            string `mapstructure:"discovery_url"`
	AuthorizeURL            string `mapstructure:"authorize_url"`
	TokenURL                string `mapstructure:"token_url"`
	UserInfoURL             string `mapstructure:"userinfo_url"`
	JWKSURL                 string `mapstructure:"jwks_url"`
	Scopes                  string `mapstructure:"scopes"`                // 默认 "openid email profile"
	RedirectURL             string `mapstructure:"redirect_url"`          // 后端回调地址（需在提供方后台登记）
	FrontendRedirectURL     string `mapstructure:"frontend_redirect_url"` // 前端接收 token 的路由（默认：/auth/oidc/callback）
	TokenAuthMethod         string `mapstructure:"token_auth_method"`     // client_secret_post / client_secret_basic / none
	UsePKCE                 bool   `mapstructure:"use_pkce"`
	ValidateIDToken         bool   `mapstructure:"validate_id_token"`
	UsePKCEExplicit         bool   `mapstructure:"-" yaml:"-"`
	ValidateIDTokenExplicit bool   `mapstructure:"-" yaml:"-"`
	AllowedSigningAlgs      string `mapstructure:"allowed_signing_algs"`   // 默认 "RS256,ES256,PS256"
	ClockSkewSeconds        int    `mapstructure:"clock_skew_seconds"`     // 默认 120
	RequireEmailVerified    bool   `mapstructure:"require_email_verified"` // 默认 false

	// 可选：用于从 userinfo JSON 中提取字段的 gjson 路径。
	// 为空时，服务端会尝试一组常见字段名。
	UserInfoEmailPath    string `mapstructure:"userinfo_email_path"`
	UserInfoIDPath       string `mapstructure:"userinfo_id_path"`
	UserInfoUsernamePath string `mapstructure:"userinfo_username_path"`
}

type DingTalkConnectConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`

	// 平台底座 + 业务行为
	DingTalkAppKind string `mapstructure:"dingtalk_app_kind"` // 仅 "internal_app"（V4 fail-closed）
	AppType         string `mapstructure:"app_type"`          // "public" (default) | "internal"

	// Corp 限定（none | internal_only）
	CorpRestrictionPolicy   string `mapstructure:"corp_restriction_policy"`
	InternalCorpID          string `mapstructure:"internal_corp_id"`
	BypassRegistration      bool   `mapstructure:"bypass_registration"`
	SyncCorpEmail           bool   `mapstructure:"sync_corp_email"`
	SyncDisplayName         bool   `mapstructure:"sync_display_name"`
	SyncDept                bool   `mapstructure:"sync_dept"`
	SyncCorpEmailAttrKey    string `mapstructure:"sync_corp_email_attr_key"`
	SyncDisplayNameAttrKey  string `mapstructure:"sync_display_name_attr_key"`
	SyncDeptAttrKey         string `mapstructure:"sync_dept_attr_key"`
	SyncCorpEmailAttrName   string `mapstructure:"sync_corp_email_attr_name"`
	SyncDisplayNameAttrName string `mapstructure:"sync_display_name_attr_name"`
	SyncDeptAttrName        string `mapstructure:"sync_dept_attr_name"`

	// 邮箱 + Username
	RequireEmail            bool   `mapstructure:"require_email"`
	UsernameOverwritePolicy string `mapstructure:"username_overwrite_policy"`

	// Attribute（私有版扩展点；开源版仅声明）
	UsernameAttributeKey         string   `mapstructure:"username_attribute_key"`
	EnableAttributeMatching      bool     `mapstructure:"enable_attribute_matching"`
	EnableAttributeSync          bool     `mapstructure:"enable_attribute_sync"`
	AttributeSyncFields          []string `mapstructure:"attribute_sync_fields"`
	AttributeSyncOverwritePolicy string   `mapstructure:"attribute_sync_overwrite_policy"`
}

type EmailOAuthProviderConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	ClientID            string `mapstructure:"client_id"`
	ClientSecret        string `mapstructure:"client_secret"`
	AuthorizeURL        string `mapstructure:"authorize_url"`
	TokenURL            string `mapstructure:"token_url"`
	UserInfoURL         string `mapstructure:"userinfo_url"`
	EmailsURL           string `mapstructure:"emails_url"`
	Scopes              string `mapstructure:"scopes"`
	RedirectURL         string `mapstructure:"redirect_url"`
	FrontendRedirectURL string `mapstructure:"frontend_redirect_url"`
}
