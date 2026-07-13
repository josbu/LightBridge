package config

import (
	"fmt"
	"sync/atomic"
	"time"
)

// TokenRefreshConfig OAuth token自动刷新配置
type TokenRefreshConfig struct {
	// 是否启用自动刷新
	Enabled bool `mapstructure:"enabled"`
	// 检查间隔（分钟）
	CheckIntervalMinutes int `mapstructure:"check_interval_minutes"`
	// 提前刷新时间（小时），在token过期前多久开始刷新
	RefreshBeforeExpiryHours float64 `mapstructure:"refresh_before_expiry_hours"`
	// 最大重试次数
	MaxRetries int `mapstructure:"max_retries"`
	// 重试退避基础时间（秒）
	RetryBackoffSeconds int `mapstructure:"retry_backoff_seconds"`
}

type PricingConfig struct {
	// 价格数据远程URL（默认使用LiteLLM镜像）
	RemoteURL string `mapstructure:"remote_url"`
	// 哈希校验文件URL
	HashURL string `mapstructure:"hash_url"`
	// 本地数据目录
	DataDir string `mapstructure:"data_dir"`
	// 回退文件路径
	FallbackFile string `mapstructure:"fallback_file"`
	// 更新间隔（小时）
	UpdateIntervalHours int `mapstructure:"update_interval_hours"`
	// 哈希校验间隔（分钟）
	HashCheckIntervalMinutes int `mapstructure:"hash_check_interval_minutes"`
}

// FeatureProfile controls which optional subsystems are eligible to start.
// The default profile is full to preserve existing deployments. Smaller
// profiles must be selected explicitly.
type FeatureProfile string

const (
	FeatureProfileMinimal  FeatureProfile = "minimal"
	FeatureProfileStandard FeatureProfile = "standard"
	FeatureProfileFull     FeatureProfile = "full"
)

// FeaturesConfig controls progressive backend registration. Overrides are keyed
// by the stable feature IDs declared in service/progressive_features.go. Core
// features cannot be disabled.
type FeaturesConfig struct {
	Profile   FeatureProfile  `mapstructure:"profile"`
	Overrides map[string]bool `mapstructure:"overrides"`
}

type ModuleConfig struct {
	DataDir                string `mapstructure:"data_dir"`
	SignaturePublicKeyPath string `mapstructure:"signature_public_key_path"`
	// MarketplaceRegistry* retain their historical names for configuration
	// compatibility. They point to the internal managed-provider registry and
	// are not exposed as a user-facing module marketplace.
	MarketplaceRegistryPath   string `mapstructure:"marketplace_registry_path"`
	MarketplaceRegistryURL    string `mapstructure:"marketplace_registry_url"`
	MarketplaceTimeoutSeconds int    `mapstructure:"marketplace_timeout_seconds"`
	Proxy                     struct {
		MihomoBinaryPath string `mapstructure:"mihomo_binary_path"`
		RuntimeDir       string `mapstructure:"runtime_dir"`
	} `mapstructure:"proxy"`
	// AistudioProxy configures the LB-managed aistudio-api reverse-proxy runtime.
	// RuntimeDir is the directory containing the aistudio-api main.py checkout
	// (default <data_dir>/aistudio-proxy). PythonBin is the interpreter (default python3).
	AistudioProxy struct {
		RuntimeDir string `mapstructure:"runtime_dir"`
		PythonBin  string `mapstructure:"python_bin"`
	} `mapstructure:"aistudio_proxy"`
}

type ServerConfig struct {
	Host               string    `mapstructure:"host"`
	Port               int       `mapstructure:"port"`
	Mode               string    `mapstructure:"mode"`                  // debug/release
	FrontendURL        string    `mapstructure:"frontend_url"`          // 前端基础 URL，用于生成邮件中的外部链接
	ReadHeaderTimeout  int       `mapstructure:"read_header_timeout"`   // 读取请求头超时（秒）
	IdleTimeout        int       `mapstructure:"idle_timeout"`          // 空闲连接超时（秒）
	TrustedProxies     []string  `mapstructure:"trusted_proxies"`       // 可信代理列表（CIDR/IP）
	MaxRequestBodySize int64     `mapstructure:"max_request_body_size"` // 全局最大请求体限制
	H2C                H2CConfig `mapstructure:"h2c"`                   // HTTP/2 Cleartext 配置
}

// H2CConfig HTTP/2 Cleartext 配置
type H2CConfig struct {
	Enabled                      bool   `mapstructure:"enabled"`                          // 是否启用 H2C
	MaxConcurrentStreams         uint32 `mapstructure:"max_concurrent_streams"`           // 最大并发流数量
	IdleTimeout                  int    `mapstructure:"idle_timeout"`                     // 空闲超时（秒）
	MaxReadFrameSize             int    `mapstructure:"max_read_frame_size"`              // 最大帧大小（字节）
	MaxUploadBufferPerConnection int    `mapstructure:"max_upload_buffer_per_connection"` // 每个连接的上传缓冲区（字节）
	MaxUploadBufferPerStream     int    `mapstructure:"max_upload_buffer_per_stream"`     // 每个流的上传缓冲区（字节）
}

type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

type SecurityConfig struct {
	URLAllowlist                     URLAllowlistConfig   `mapstructure:"url_allowlist"`
	ResponseHeaders                  ResponseHeaderConfig `mapstructure:"response_headers"`
	CSP                              CSPConfig            `mapstructure:"csp"`
	ProxyFallback                    ProxyFallbackConfig  `mapstructure:"proxy_fallback"`
	ProxyProbe                       ProxyProbeConfig     `mapstructure:"proxy_probe"`
	TrustForwardedIPForAPIKeyACL     bool                 `mapstructure:"trust_forwarded_ip_for_api_key_acl"`
	trustForwardedIPForAPIKeyACLLive *atomic.Bool         `mapstructure:"-"`
}

func (c *Config) TrustForwardedIPForAPIKeyACL() bool {
	if c == nil {
		return false
	}
	live := c.Security.trustForwardedIPForAPIKeyACLLive
	if live == nil {
		return c.Security.TrustForwardedIPForAPIKeyACL
	}
	return live.Load()
}

func (c *Config) SetTrustForwardedIPForAPIKeyACL(enabled bool) {
	if c == nil {
		return
	}
	c.Security.TrustForwardedIPForAPIKeyACL = enabled
	if c.Security.trustForwardedIPForAPIKeyACLLive == nil {
		c.Security.trustForwardedIPForAPIKeyACLLive = &atomic.Bool{}
	}
	c.Security.trustForwardedIPForAPIKeyACLLive.Store(enabled)
}

type URLAllowlistConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	UpstreamHosts     []string `mapstructure:"upstream_hosts"`
	PricingHosts      []string `mapstructure:"pricing_hosts"`
	CRSHosts          []string `mapstructure:"crs_hosts"`
	AllowPrivateHosts bool     `mapstructure:"allow_private_hosts"`
	// 关闭 URL 白名单校验时，是否允许 http URL（默认只允许 https）
	AllowInsecureHTTP bool `mapstructure:"allow_insecure_http"`
}

type ResponseHeaderConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	AdditionalAllowed []string `mapstructure:"additional_allowed"`
	ForceRemove       []string `mapstructure:"force_remove"`
}

type CSPConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Policy  string `mapstructure:"policy"`
}

type ProxyFallbackConfig struct {
	// AllowDirectOnError 当辅助服务的代理初始化失败时是否允许回退直连。
	// 仅影响以下非 AI 账号连接的辅助服务：
	//   - GitHub Release 更新检查
	//   - 定价数据拉取
	// 不影响 AI 账号网关连接（Claude/OpenAI/Gemini/Antigravity），
	// 这些关键路径的代理失败始终返回错误，不会回退直连。
	// 默认 false：避免因代理配置错误导致服务器真实 IP 泄露。
	AllowDirectOnError bool `mapstructure:"allow_direct_on_error"`
}

type ProxyProbeConfig struct {
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"` // 已禁用：禁止跳过 TLS 证书验证
}

type BillingConfig struct {
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	// UserPlatformQuotaCacheTTLSeconds 用户 × 平台 quota 缓存 TTL（秒），默认 86400=1天，覆盖典型 daily 窗口。
	// 消费点：
	//   - billing_cache_service.cacheWriteWorker 异步累加
	//   - billing_cache_service.checkUserPlatformQuotaEligibility 首次缓存装载
	// 读写两端必须共用同一 TTL，避免缓存生命周期不一致导致 quota 计数漂移。
	UserPlatformQuotaCacheTTLSeconds int `mapstructure:"user_platform_quota_cache_ttl_seconds"`
	// UserPlatformQuotaSentinelTTLSeconds sentinel(无 limit 占位)entry 的 TTL,
	// 显著短于 quota cache 默认 86400s 以控 Redis 内存;默认 3600=1h。
	UserPlatformQuotaSentinelTTLSeconds int `mapstructure:"user_platform_quota_sentinel_ttl_seconds"`
}

type CircuitBreakerConfig struct {
	Enabled             bool `mapstructure:"enabled"`
	FailureThreshold    int  `mapstructure:"failure_threshold"`
	ResetTimeoutSeconds int  `mapstructure:"reset_timeout_seconds"`
	HalfOpenRequests    int  `mapstructure:"half_open_requests"`
}

type ConcurrencyConfig struct {
	// PingInterval: 并发等待期间的 SSE ping 间隔（秒）
	PingInterval int `mapstructure:"ping_interval"`
}

type ImageConcurrencyConfig struct {
	// Enabled: 是否启用图片生成独立并发限制，默认关闭以保持现有行为
	Enabled bool `mapstructure:"enabled"`
	// MaxConcurrentRequests: 当前进程允许同时处理的图片生成请求数，0表示不限制
	MaxConcurrentRequests int `mapstructure:"max_concurrent_requests"`
	// OverflowMode: 图片并发达到上限后的处理方式：reject/wait
	OverflowMode string `mapstructure:"overflow_mode"`
	// WaitTimeoutSeconds: overflow_mode=wait 时等待图片并发槽位的超时时间（秒）
	WaitTimeoutSeconds int `mapstructure:"wait_timeout_seconds"`
	// MaxWaitingRequests: overflow_mode=wait 时当前进程允许排队等待的图片请求数
	MaxWaitingRequests int `mapstructure:"max_waiting_requests"`
}

const (
	ImageConcurrencyOverflowModeReject = "reject"
	ImageConcurrencyOverflowModeWait   = "wait"
)

// GatewayConfig API网关相关配置
type GatewayConfig struct {
	// 等待上游响应头的超时时间（秒），0表示无超时
	// 注意：这不影响流式数据传输，只控制等待响应头的时间
	ResponseHeaderTimeout int `mapstructure:"response_header_timeout"`
	// OpenAIResponseHeaderTimeout: OpenAI/Codex 上游等待响应头的超时时间（秒），0表示无超时
	// OpenAI/Codex 请求可能在上游排队较久；默认不使用通用响应头超时截断。
	OpenAIResponseHeaderTimeout int `mapstructure:"openai_response_header_timeout"`
	// 请求体最大字节数，用于网关请求体大小限制
	MaxBodySize int64 `mapstructure:"max_body_size"`
	// 非流式上游响应体读取上限（字节），用于防止无界读取导致内存放大
	UpstreamResponseReadMaxBytes int64 `mapstructure:"upstream_response_read_max_bytes"`
	// 代理探测响应体读取上限（字节）
	ProxyProbeResponseReadMaxBytes int64 `mapstructure:"proxy_probe_response_read_max_bytes"`
	// Gemini 上游响应头调试日志开关（默认关闭，避免高频日志开销）
	GeminiDebugResponseHeaders bool `mapstructure:"gemini_debug_response_headers"`
	// ConnectionPoolIsolation: 上游连接池隔离策略（proxy/account/account_proxy）
	ConnectionPoolIsolation string `mapstructure:"connection_pool_isolation"`
	// ForceCodexCLI: 强制将 OpenAI `/v1/responses` 请求按 Codex CLI 处理。
	// 用于网关未透传/改写 User-Agent 时的兼容兜底（默认关闭，避免影响其他客户端）。
	ForceCodexCLI bool `mapstructure:"force_codex_cli"`
	// CodexImageGenerationBridgeEnabled: 是否为 Codex `/v1/responses` 自动注入 image_generation 工具和桥接指令。
	// 默认关闭，避免纯文本 Codex 请求被意外改写；显式携带 image_generation 工具的请求仍按分组能力转发。
	CodexImageGenerationBridgeEnabled bool `mapstructure:"codex_image_generation_bridge_enabled"`
	// ForcedCodexInstructionsTemplateFile: 服务端强制附加到 Codex 顶层 instructions 的模板文件路径。
	// 模板渲染后会直接覆盖最终 instructions；若需要保留客户端 system 转换结果，请在模板中显式引用 {{ .ExistingInstructions }}。
	ForcedCodexInstructionsTemplateFile string `mapstructure:"forced_codex_instructions_template_file"`
	// ForcedCodexInstructionsTemplate: 启动时从模板文件读取并缓存的模板内容。
	// 该字段不直接参与配置反序列化，仅用于请求热路径避免重复读盘。
	ForcedCodexInstructionsTemplate string `mapstructure:"-"`
	// OpenAIPassthroughAllowTimeoutHeaders: OpenAI 透传模式是否放行客户端超时头
	// 关闭（默认）可避免 x-stainless-timeout 等头导致上游提前断流。
	OpenAIPassthroughAllowTimeoutHeaders bool `mapstructure:"openai_passthrough_allow_timeout_headers"`
	// OpenAIWS: OpenAI Responses WebSocket 配置（默认开启，可按需回滚到 HTTP）
	OpenAIWS GatewayOpenAIWSConfig `mapstructure:"openai_ws"`
	// OpenAIHTTP2: OpenAI HTTP 上游协议策略（默认启用 HTTP/2，可按代理能力回退 HTTP/1.1）
	OpenAIHTTP2 GatewayOpenAIHTTP2Config `mapstructure:"openai_http2"`
	// ImageConcurrency: 图片生成独立并发限制配置（默认关闭）
	ImageConcurrency ImageConcurrencyConfig `mapstructure:"image_concurrency"`

	// HTTP 上游连接池配置（性能优化：支持高并发场景调优）
	// MaxIdleConns: 所有主机的最大空闲连接总数
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// MaxIdleConnsPerHost: 每个主机的最大空闲连接数（关键参数，影响连接复用率）
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`
	// MaxConnsPerHost: 每个主机的最大连接数（包括活跃+空闲），0表示无限制
	MaxConnsPerHost int `mapstructure:"max_conns_per_host"`
	// IdleConnTimeoutSeconds: 空闲连接超时时间（秒）
	IdleConnTimeoutSeconds int `mapstructure:"idle_conn_timeout_seconds"`
	// MaxUpstreamClients: 上游连接池客户端最大缓存数量
	// 当使用连接池隔离策略时，系统会为不同的账户/代理组合创建独立的 HTTP 客户端
	// 此参数限制缓存的客户端数量，超出后会淘汰最久未使用的客户端
	// 建议值：预估的活跃账户数 * 1.2（留有余量）
	MaxUpstreamClients int `mapstructure:"max_upstream_clients"`
	// ClientIdleTTLSeconds: 上游连接池客户端空闲回收阈值（秒）
	// 超过此时间未使用的客户端会被标记为可回收
	// 建议值：根据用户访问频率设置，一般 10-30 分钟
	ClientIdleTTLSeconds int `mapstructure:"client_idle_ttl_seconds"`
	// ConcurrencySlotTTLMinutes: 并发槽位过期时间（分钟）
	// 应大于最长 LLM 请求时间，防止请求完成前槽位过期
	ConcurrencySlotTTLMinutes int `mapstructure:"concurrency_slot_ttl_minutes"`
	// SessionIdleTimeoutMinutes: 会话空闲超时时间（分钟），默认 5 分钟
	// 用于 Anthropic OAuth/SetupToken 账号的会话数量限制功能
	// 空闲超过此时间的会话将被自动释放
	SessionIdleTimeoutMinutes int `mapstructure:"session_idle_timeout_minutes"`

	// StreamDataIntervalTimeout: 流数据间隔超时（秒），0表示禁用
	StreamDataIntervalTimeout int `mapstructure:"stream_data_interval_timeout"`
	// StreamKeepaliveInterval: 流式 keepalive 间隔（秒），0表示禁用
	StreamKeepaliveInterval int `mapstructure:"stream_keepalive_interval"`
	// ImageStreamDataIntervalTimeout: 图片流数据间隔超时（秒），0表示禁用
	ImageStreamDataIntervalTimeout int `mapstructure:"image_stream_data_interval_timeout"`
	// ImageStreamKeepaliveInterval: 图片流式 keepalive 间隔（秒），0表示禁用
	ImageStreamKeepaliveInterval int `mapstructure:"image_stream_keepalive_interval"`
	// MaxLineSize: 上游 SSE 单行最大字节数（0使用默认值）
	MaxLineSize int `mapstructure:"max_line_size"`

	// 是否记录上游错误响应体摘要（避免输出请求内容）
	LogUpstreamErrorBody bool `mapstructure:"log_upstream_error_body"`
	// 上游错误响应体记录最大字节数（超过会截断）
	LogUpstreamErrorBodyMaxBytes int `mapstructure:"log_upstream_error_body_max_bytes"`

	// API-key 账号在客户端未提供 anthropic-beta 时，是否按需自动补齐（默认关闭以保持兼容）
	InjectBetaForAPIKey bool `mapstructure:"inject_beta_for_apikey"`

	// 是否允许对部分 400 错误触发 failover（默认关闭以避免改变语义）
	FailoverOn400 bool `mapstructure:"failover_on_400"`

	// 账户切换最大次数（遇到上游错误时切换到其他账户的次数上限）
	MaxAccountSwitches int `mapstructure:"max_account_switches"`
	// Gemini 账户切换最大次数（Gemini 平台单独配置，因 API 限制更严格）
	MaxAccountSwitchesGemini int `mapstructure:"max_account_switches_gemini"`

	// Antigravity 429 fallback 限流时间（分钟），解析重置时间失败时使用
	AntigravityFallbackCooldownMinutes int `mapstructure:"antigravity_fallback_cooldown_minutes"`

	// Scheduling: 账号调度相关配置
	Scheduling GatewaySchedulingConfig `mapstructure:"scheduling"`

	// TLSFingerprint: TLS指纹伪装配置
	TLSFingerprint TLSFingerprintConfig `mapstructure:"tls_fingerprint"`

	// UsageRecord: 使用量记录异步队列配置（有界队列 + 固定 worker）
	UsageRecord GatewayUsageRecordConfig `mapstructure:"usage_record"`

	// UserGroupRateCacheTTLSeconds: 用户分组倍率热路径缓存 TTL（秒）
	UserGroupRateCacheTTLSeconds int `mapstructure:"user_group_rate_cache_ttl_seconds"`
	// ModelsListCacheTTLSeconds: /v1/models 模型列表短缓存 TTL（秒）
	ModelsListCacheTTLSeconds int `mapstructure:"models_list_cache_ttl_seconds"`

	// UserMessageQueue: 用户消息串行队列配置
	// 对 role:"user" 的真实用户消息实施账号级串行化 + RPM 自适应延迟
	UserMessageQueue UserMessageQueueConfig `mapstructure:"user_message_queue"`
}

// GatewayOpenAIHTTP2Config OpenAI HTTP 上游协议配置。
// 默认启用 HTTP/2；在部分代理不兼容时按策略回退 HTTP/1.1。
type GatewayOpenAIHTTP2Config struct {
	// Enabled: 是否启用 OpenAI HTTP/2 优先策略
	Enabled bool `mapstructure:"enabled"`
	// AllowProxyFallbackToHTTP1: HTTP/HTTPS 代理出现明确 H2 兼容错误时，临时回退 HTTP/1.1
	AllowProxyFallbackToHTTP1 bool `mapstructure:"allow_proxy_fallback_to_http1"`
	// FallbackErrorThreshold: 回退窗口内累计多少次兼容错误后触发回退
	FallbackErrorThreshold int `mapstructure:"fallback_error_threshold"`
	// FallbackWindowSeconds: 统计兼容错误的时间窗口（秒）
	FallbackWindowSeconds int `mapstructure:"fallback_window_seconds"`
	// FallbackTTLSeconds: 触发后回退 HTTP/1.1 的持续时间（秒）
	FallbackTTLSeconds int `mapstructure:"fallback_ttl_seconds"`
}

// UserMessageQueueConfig 用户消息串行队列配置
// 用于 Anthropic OAuth/SetupToken 账号的用户消息串行化发送
type UserMessageQueueConfig struct {
	// Mode: 模式选择
	// "serialize" = 账号级串行锁 + RPM 自适应延迟
	// "throttle" = 仅 RPM 自适应前置延迟，不阻塞并发
	// "" = 禁用（默认）
	Mode string `mapstructure:"mode"`
	// Enabled: 已废弃，仅向后兼容（等同于 mode: "serialize"）
	Enabled bool `mapstructure:"enabled"`
	// LockTTLMs: 串行锁 TTL（毫秒），应大于最长请求时间
	LockTTLMs int `mapstructure:"lock_ttl_ms"`
	// WaitTimeoutMs: 等待获取锁的超时时间（毫秒）
	WaitTimeoutMs int `mapstructure:"wait_timeout_ms"`
	// MinDelayMs: RPM 自适应延迟下限（毫秒）
	MinDelayMs int `mapstructure:"min_delay_ms"`
	// MaxDelayMs: RPM 自适应延迟上限（毫秒）
	MaxDelayMs int `mapstructure:"max_delay_ms"`
	// CleanupIntervalSeconds: 孤儿锁清理间隔（秒），0 表示禁用
	CleanupIntervalSeconds int `mapstructure:"cleanup_interval_seconds"`
}

// WaitTimeout 返回等待超时的 time.Duration
func (c *UserMessageQueueConfig) WaitTimeout() time.Duration {
	if c.WaitTimeoutMs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.WaitTimeoutMs) * time.Millisecond
}

// GetEffectiveMode 返回生效的模式
// 注意：Mode 字段已在 load() 中做过白名单校验和规范化，此处无需重复验证
func (c *UserMessageQueueConfig) GetEffectiveMode() string {
	if c.Mode == UMQModeSerialize || c.Mode == UMQModeThrottle {
		return c.Mode
	}
	if c.Enabled {
		return UMQModeSerialize // 向后兼容
	}
	return ""
}

// GatewayOpenAIWSConfig OpenAI Responses WebSocket 配置。
// 注意：默认全局开启；如需回滚可使用 force_http 或关闭 enabled。
type GatewayOpenAIWSConfig struct {
	// ModeRouterV2Enabled: 新版 WS mode 路由开关（默认 false；关闭时保持 legacy 行为）
	ModeRouterV2Enabled bool `mapstructure:"mode_router_v2_enabled"`
	// IngressModeDefault: ingress 默认模式（off/ctx_pool/passthrough/http_bridge）
	IngressModeDefault string `mapstructure:"ingress_mode_default"`
	// Enabled: 全局总开关（默认 true）
	Enabled bool `mapstructure:"enabled"`
	// OAuthEnabled: 是否允许 OpenAI OAuth 账号使用 WS
	OAuthEnabled bool `mapstructure:"oauth_enabled"`
	// APIKeyEnabled: 是否允许 OpenAI API Key 账号使用 WS
	APIKeyEnabled bool `mapstructure:"apikey_enabled"`
	// ForceHTTP: 全局强制 HTTP（用于紧急回滚）
	ForceHTTP bool `mapstructure:"force_http"`
	// AllowStoreRecovery: 允许在 WSv2 下按策略恢复 store=true（默认 false）
	AllowStoreRecovery bool `mapstructure:"allow_store_recovery"`
	// IngressPreviousResponseRecoveryEnabled: ingress 模式收到 previous_response_not_found 时，是否允许自动去掉 previous_response_id 重试一次（默认 true）
	IngressPreviousResponseRecoveryEnabled bool `mapstructure:"ingress_previous_response_recovery_enabled"`
	// StoreDisabledConnMode: store=false 且无可复用会话连接时的建连策略（strict/adaptive/off）
	// - strict: 强制新建连接（隔离优先）
	// - adaptive: 仅在高风险失败后强制新建连接（性能与隔离折中）
	// - off: 不强制新建连接（复用优先）
	StoreDisabledConnMode string `mapstructure:"store_disabled_conn_mode"`
	// StoreDisabledForceNewConn: store=false 且无可复用粘连连接时是否强制新建连接（默认 true，保障会话隔离）
	// 兼容旧配置；当 StoreDisabledConnMode 为空时才生效。
	StoreDisabledForceNewConn bool `mapstructure:"store_disabled_force_new_conn"`
	// PrewarmGenerateEnabled: 是否启用 WSv2 generate=false 预热（默认 false）
	PrewarmGenerateEnabled bool `mapstructure:"prewarm_generate_enabled"`
	// ClientReadLimitBytes: 入站客户端 WS 单帧读取上限。
	ClientReadLimitBytes int64 `mapstructure:"client_read_limit_bytes"`
	// HTTPBridgeEnabled: 首包过大时，保持客户端 WS，改用 HTTP Responses 上游。
	HTTPBridgeEnabled bool `mapstructure:"http_bridge_enabled"`
	// HTTPBridgeThresholdBytes: 触发 HTTP bridge 的入站 WS payload 阈值。
	HTTPBridgeThresholdBytes int64 `mapstructure:"http_bridge_threshold_bytes"`

	// Feature 开关：v2 优先于 v1
	ResponsesWebsockets   bool `mapstructure:"responses_websockets"`
	ResponsesWebsocketsV2 bool `mapstructure:"responses_websockets_v2"`

	// 连接池参数
	MaxConnsPerAccount int `mapstructure:"max_conns_per_account"`
	MinIdlePerAccount  int `mapstructure:"min_idle_per_account"`
	MaxIdlePerAccount  int `mapstructure:"max_idle_per_account"`
	// DynamicMaxConnsByAccountConcurrencyEnabled: 是否按账号并发动态计算连接池上限
	DynamicMaxConnsByAccountConcurrencyEnabled bool `mapstructure:"dynamic_max_conns_by_account_concurrency_enabled"`
	// OAuthMaxConnsFactor: OAuth 账号连接池系数（effective=ceil(concurrency*factor)）
	OAuthMaxConnsFactor float64 `mapstructure:"oauth_max_conns_factor"`
	// APIKeyMaxConnsFactor: API Key 账号连接池系数（effective=ceil(concurrency*factor)）
	APIKeyMaxConnsFactor  float64 `mapstructure:"apikey_max_conns_factor"`
	DialTimeoutSeconds    int     `mapstructure:"dial_timeout_seconds"`
	ReadTimeoutSeconds    int     `mapstructure:"read_timeout_seconds"`
	WriteTimeoutSeconds   int     `mapstructure:"write_timeout_seconds"`
	PoolTargetUtilization float64 `mapstructure:"pool_target_utilization"`
	QueueLimitPerConn     int     `mapstructure:"queue_limit_per_conn"`
	// EventFlushBatchSize: WS 流式写出批量 flush 阈值（事件条数）
	EventFlushBatchSize int `mapstructure:"event_flush_batch_size"`
	// EventFlushIntervalMS: WS 流式写出最大等待时间（毫秒）；0 表示仅按 batch 触发
	EventFlushIntervalMS int `mapstructure:"event_flush_interval_ms"`
	// PrewarmCooldownMS: 连接池预热触发冷却时间（毫秒）
	PrewarmCooldownMS int `mapstructure:"prewarm_cooldown_ms"`
	// FallbackCooldownSeconds: WS 回退冷却窗口，避免 WS/HTTP 抖动；0 表示关闭冷却
	FallbackCooldownSeconds int `mapstructure:"fallback_cooldown_seconds"`
	// RetryBackoffInitialMS: WS 重试初始退避（毫秒）；<=0 表示关闭退避
	RetryBackoffInitialMS int `mapstructure:"retry_backoff_initial_ms"`
	// RetryBackoffMaxMS: WS 重试最大退避（毫秒）
	RetryBackoffMaxMS int `mapstructure:"retry_backoff_max_ms"`
	// RetryJitterRatio: WS 重试退避抖动比例（0-1）
	RetryJitterRatio float64 `mapstructure:"retry_jitter_ratio"`
	// RetryTotalBudgetMS: WS 单次请求重试总预算（毫秒）；0 表示关闭预算限制
	RetryTotalBudgetMS int `mapstructure:"retry_total_budget_ms"`
	// PayloadLogSampleRate: payload_schema 日志采样率（0-1）
	PayloadLogSampleRate float64 `mapstructure:"payload_log_sample_rate"`

	// 账号调度与粘连参数
	LBTopK int `mapstructure:"lb_top_k"`
	// StickySessionTTLSeconds: session_hash -> account_id 粘连 TTL
	StickySessionTTLSeconds int `mapstructure:"sticky_session_ttl_seconds"`
	// SessionHashReadOldFallback: 会话哈希迁移期是否允许“新 key 未命中时回退读旧 SHA-256 key”
	SessionHashReadOldFallback bool `mapstructure:"session_hash_read_old_fallback"`
	// SessionHashDualWriteOld: 会话哈希迁移期是否双写旧 SHA-256 key（短 TTL）
	SessionHashDualWriteOld bool `mapstructure:"session_hash_dual_write_old"`
	// MetadataBridgeEnabled: RequestMetadata 迁移期是否保留旧 ctxkey.* 兼容桥接
	MetadataBridgeEnabled bool `mapstructure:"metadata_bridge_enabled"`
	// StickyResponseIDTTLSeconds: response_id -> account_id 粘连 TTL
	StickyResponseIDTTLSeconds int `mapstructure:"sticky_response_id_ttl_seconds"`
	// StickyPreviousResponseTTLSeconds: 兼容旧键（当新键未设置时回退）
	StickyPreviousResponseTTLSeconds int `mapstructure:"sticky_previous_response_ttl_seconds"`

	SchedulerScoreWeights GatewayOpenAIWSSchedulerScoreWeights `mapstructure:"scheduler_score_weights"`
}

// GatewayOpenAIWSSchedulerScoreWeights 账号调度打分权重。
type GatewayOpenAIWSSchedulerScoreWeights struct {
	Priority  float64 `mapstructure:"priority"`
	Load      float64 `mapstructure:"load"`
	Queue     float64 `mapstructure:"queue"`
	ErrorRate float64 `mapstructure:"error_rate"`
	TTFT      float64 `mapstructure:"ttft"`
}

// GatewayUsageRecordConfig 使用量记录异步队列配置
type GatewayUsageRecordConfig struct {
	// WorkerCount: worker 初始数量（自动扩缩容开启时作为初始并发上限）
	WorkerCount int `mapstructure:"worker_count"`
	// QueueSize: 队列容量（有界）
	QueueSize int `mapstructure:"queue_size"`
	// TaskTimeoutSeconds: 单个使用量记录任务超时（秒）
	TaskTimeoutSeconds int `mapstructure:"task_timeout_seconds"`
	// OverflowPolicy: 队列满时策略；drop/sample 仅为旧配置兼容，运行时统一按 sync 执行
	OverflowPolicy string `mapstructure:"overflow_policy"`
	// OverflowSamplePercent: 旧 sample 配置兼容字段（1-100），不再用于丢弃计费任务
	OverflowSamplePercent int `mapstructure:"overflow_sample_percent"`

	// AutoScaleEnabled: 是否启用 worker 自动扩缩容
	AutoScaleEnabled bool `mapstructure:"auto_scale_enabled"`
	// AutoScaleMinWorkers: 自动扩缩容最小 worker 数
	AutoScaleMinWorkers int `mapstructure:"auto_scale_min_workers"`
	// AutoScaleMaxWorkers: 自动扩缩容最大 worker 数
	AutoScaleMaxWorkers int `mapstructure:"auto_scale_max_workers"`
	// AutoScaleUpQueuePercent: 队列占用率达到该阈值时触发扩容
	AutoScaleUpQueuePercent int `mapstructure:"auto_scale_up_queue_percent"`
	// AutoScaleDownQueuePercent: 队列占用率低于该阈值时触发缩容
	AutoScaleDownQueuePercent int `mapstructure:"auto_scale_down_queue_percent"`
	// AutoScaleUpStep: 每次扩容步长
	AutoScaleUpStep int `mapstructure:"auto_scale_up_step"`
	// AutoScaleDownStep: 每次缩容步长
	AutoScaleDownStep int `mapstructure:"auto_scale_down_step"`
	// AutoScaleCheckIntervalSeconds: 自动扩缩容检测间隔（秒）
	AutoScaleCheckIntervalSeconds int `mapstructure:"auto_scale_check_interval_seconds"`
	// AutoScaleCooldownSeconds: 自动扩缩容冷却时间（秒）
	AutoScaleCooldownSeconds int `mapstructure:"auto_scale_cooldown_seconds"`
}

// TLSFingerprintConfig TLS指纹伪装配置
// 用于模拟 Claude CLI (Node.js) 的 TLS 握手特征，避免被识别为非官方客户端
type TLSFingerprintConfig struct {
	// Enabled: 是否全局启用TLS指纹功能
	Enabled bool `mapstructure:"enabled"`
	// Profiles: 预定义的TLS指纹配置模板
	// key 为模板名称，如 "claude_cli_v2", "chrome_120" 等
	Profiles map[string]TLSProfileConfig `mapstructure:"profiles"`
}

// TLSProfileConfig 单个TLS指纹模板的配置
// 所有列表字段为空时使用内置默认值（Claude CLI 2.x / Node.js 20.x）
// 建议通过 TLS 指纹采集工具 (tests/tls-fingerprint-web) 获取完整配置
type TLSProfileConfig struct {
	// Name: 模板显示名称
	Name string `mapstructure:"name"`
	// EnableGREASE: 是否启用GREASE扩展（Chrome使用，Node.js不使用）
	EnableGREASE bool `mapstructure:"enable_grease"`
	// CipherSuites: TLS加密套件列表
	CipherSuites []uint16 `mapstructure:"cipher_suites"`
	// Curves: 椭圆曲线列表
	Curves []uint16 `mapstructure:"curves"`
	// PointFormats: 点格式列表
	PointFormats []uint16 `mapstructure:"point_formats"`
	// SignatureAlgorithms: 签名算法列表
	SignatureAlgorithms []uint16 `mapstructure:"signature_algorithms"`
	// ALPNProtocols: ALPN协议列表（如 ["h2", "http/1.1"]）
	ALPNProtocols []string `mapstructure:"alpn_protocols"`
	// SupportedVersions: 支持的TLS版本列表（如 [0x0304, 0x0303] 即 TLS1.3, TLS1.2）
	SupportedVersions []uint16 `mapstructure:"supported_versions"`
	// KeyShareGroups: Key Share中发送的曲线组（如 [29] 即 X25519）
	KeyShareGroups []uint16 `mapstructure:"key_share_groups"`
	// PSKModes: PSK密钥交换模式（如 [1] 即 psk_dhe_ke）
	PSKModes []uint16 `mapstructure:"psk_modes"`
	// Extensions: TLS扩展类型ID列表，按发送顺序排列
	// 空则使用内置默认顺序 [0,11,10,35,16,22,23,13,43,45,51]
	// GREASE值(如0x0a0a)会自动插入GREASE扩展
	Extensions []uint16 `mapstructure:"extensions"`
}

// GatewaySchedulingConfig accounts scheduling configuration.
type GatewaySchedulingConfig struct {
	// 粘性会话排队配置
	StickySessionMaxWaiting  int           `mapstructure:"sticky_session_max_waiting"`
	StickySessionWaitTimeout time.Duration `mapstructure:"sticky_session_wait_timeout"`

	// 兜底排队配置
	FallbackWaitTimeout time.Duration `mapstructure:"fallback_wait_timeout"`
	FallbackMaxWaiting  int           `mapstructure:"fallback_max_waiting"`

	// 兜底层账户选择策略: "last_used"(按最后使用时间排序，默认) 或 "random"(随机)
	FallbackSelectionMode string `mapstructure:"fallback_selection_mode"`

	// 负载计算
	LoadBatchEnabled    bool `mapstructure:"load_batch_enabled"`
	LoadBatchCacheTTLMS int  `mapstructure:"load_batch_cache_ttl_ms"`
	// 快照桶读取时的 MGET 分块大小
	SnapshotMGetChunkSize int `mapstructure:"snapshot_mget_chunk_size"`
	// 快照重建时的缓存写入分块大小
	SnapshotWriteChunkSize int `mapstructure:"snapshot_write_chunk_size"`

	// 过期槽位清理周期（0 表示禁用）
	SlotCleanupInterval time.Duration `mapstructure:"slot_cleanup_interval"`

	// 受控回源配置
	DbFallbackEnabled bool `mapstructure:"db_fallback_enabled"`
	// 受控回源超时（秒），0 表示不额外收紧超时
	DbFallbackTimeoutSeconds int `mapstructure:"db_fallback_timeout_seconds"`
	// 受控回源限流（实例级 QPS），0 表示不限制
	DbFallbackMaxQPS int `mapstructure:"db_fallback_max_qps"`

	// Outbox 轮询与滞后阈值配置
	// Outbox 轮询周期（秒）
	OutboxPollIntervalSeconds int `mapstructure:"outbox_poll_interval_seconds"`
	// Outbox 滞后告警阈值（秒）
	OutboxLagWarnSeconds int `mapstructure:"outbox_lag_warn_seconds"`
	// Outbox 触发强制重建阈值（秒）
	OutboxLagRebuildSeconds int `mapstructure:"outbox_lag_rebuild_seconds"`
	// Outbox 连续滞后触发次数
	OutboxLagRebuildFailures int `mapstructure:"outbox_lag_rebuild_failures"`
	// Outbox 积压触发重建阈值（行数）
	OutboxBacklogRebuildRows int `mapstructure:"outbox_backlog_rebuild_rows"`

	// 全量重建周期配置
	// 全量重建周期（秒），0 表示禁用
	FullRebuildIntervalSeconds int `mapstructure:"full_rebuild_interval_seconds"`
}

func (s *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// DatabaseConfig 数据库连接配置
// 性能优化：新增连接池参数，避免频繁创建/销毁连接
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
	// 连接池配置（性能优化：可配置化连接池参数）
	// MaxOpenConns: 最大打开连接数，控制数据库连接上限，防止资源耗尽
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// MaxIdleConns: 最大空闲连接数，保持热连接减少建连延迟
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// ConnMaxLifetimeMinutes: 连接最大存活时间，防止长连接导致的资源泄漏
	ConnMaxLifetimeMinutes int `mapstructure:"conn_max_lifetime_minutes"`
	// ConnMaxIdleTimeMinutes: 空闲连接最大存活时间，及时释放不活跃连接
	ConnMaxIdleTimeMinutes int `mapstructure:"conn_max_idle_time_minutes"`
	// UserPlatformQuotaFlusherEnabled: 是否启用 user×platform 配额写聚合 flusher
	UserPlatformQuotaFlusherEnabled bool `mapstructure:"user_platform_quota_flusher_enabled"`
	// UserPlatformQuotaFlushIntervalMs: flusher 刷写间隔（毫秒）
	UserPlatformQuotaFlushIntervalMs int `mapstructure:"user_platform_quota_flush_interval_ms"`
	// UserPlatformQuotaFlushBatchSize: flusher 单批最大条数
	// 建议 ≤ 6000（单条 UPSERT 原子上限）
	UserPlatformQuotaFlushBatchSize int `mapstructure:"user_platform_quota_flush_batch_size"`
}

func (d *DatabaseConfig) DSN() string {
	// 当密码为空时不包含 password 参数，避免 libpq 解析错误
	if d.Password == "" {
		return fmt.Sprintf(
			"host=%s port=%d user=%s dbname=%s sslmode=%s",
			d.Host, d.Port, d.User, d.DBName, d.SSLMode,
		)
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

// DSNWithTimezone returns DSN with timezone setting
func (d *DatabaseConfig) DSNWithTimezone(tz string) string {
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	// 当密码为空时不包含 password 参数，避免 libpq 解析错误
	if d.Password == "" {
		return fmt.Sprintf(
			"host=%s port=%d user=%s dbname=%s sslmode=%s TimeZone=%s",
			d.Host, d.Port, d.User, d.DBName, d.SSLMode, tz,
		)
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode, tz,
	)
}

// RedisConfig Redis 连接配置
// 性能优化：新增连接池和超时参数，提升高并发场景下的吞吐量
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	// 连接池与超时配置（性能优化：可配置化连接池参数）
	// DialTimeoutSeconds: 建立连接超时，防止慢连接阻塞
	DialTimeoutSeconds int `mapstructure:"dial_timeout_seconds"`
	// ReadTimeoutSeconds: 读取超时，避免慢查询阻塞连接池
	ReadTimeoutSeconds int `mapstructure:"read_timeout_seconds"`
	// WriteTimeoutSeconds: 写入超时，避免慢写入阻塞连接池
	WriteTimeoutSeconds int `mapstructure:"write_timeout_seconds"`
	// PoolSize: 连接池大小，控制最大并发连接数
	PoolSize int `mapstructure:"pool_size"`
	// MinIdleConns: 最小空闲连接数，保持热连接减少冷启动延迟
	MinIdleConns int `mapstructure:"min_idle_conns"`
	// EnableTLS: 是否启用 TLS/SSL 连接
	EnableTLS bool `mapstructure:"enable_tls"`
}

func (r *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type OpsConfig struct {
	// Enabled controls whether ops features should run.
	//
	// NOTE: vNext still has a DB-backed feature flag (ops_monitoring_enabled) for runtime on/off.
	// This config flag is the "hard switch" for deployments that want to disable ops completely.
	Enabled bool `mapstructure:"enabled"`

	// UsePreaggregatedTables prefers ops_metrics_hourly/daily for long-window dashboard queries.
	UsePreaggregatedTables bool `mapstructure:"use_preaggregated_tables"`

	// Cleanup controls periodic deletion of old ops data to prevent unbounded growth.
	Cleanup OpsCleanupConfig `mapstructure:"cleanup"`

	// MetricsCollectorCache controls Redis caching for expensive per-window collector queries.
	MetricsCollectorCache OpsMetricsCollectorCacheConfig `mapstructure:"metrics_collector_cache"`

	// Pre-aggregation configuration.
	Aggregation OpsAggregationConfig `mapstructure:"aggregation"`
}

type OpsCleanupConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Schedule string `mapstructure:"schedule"`

	// Retention days (0 disables that cleanup target).
	//
	// vNext requirement: default 30 days across ops datasets.
	ErrorLogRetentionDays      int `mapstructure:"error_log_retention_days"`
	MinuteMetricsRetentionDays int `mapstructure:"minute_metrics_retention_days"`
	HourlyMetricsRetentionDays int `mapstructure:"hourly_metrics_retention_days"`
}

type OpsAggregationConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type OpsMetricsCollectorCacheConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	TTL     time.Duration `mapstructure:"ttl"`
}

type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	ExpireHour int    `mapstructure:"expire_hour"`
	// AccessTokenExpireMinutes: Access Token有效期（分钟）
	// - >0: 使用分钟配置（优先级高于 ExpireHour）
	// - =0: 回退使用 ExpireHour（向后兼容旧配置）
	AccessTokenExpireMinutes int `mapstructure:"access_token_expire_minutes"`
	// RefreshTokenExpireDays: Refresh Token有效期（天），默认30天
	RefreshTokenExpireDays int `mapstructure:"refresh_token_expire_days"`
	// RefreshWindowMinutes: 刷新窗口（分钟），在Access Token过期前多久开始允许刷新
	RefreshWindowMinutes int `mapstructure:"refresh_window_minutes"`
}

// TotpConfig TOTP 双因素认证配置
type TotpConfig struct {
	// EncryptionKey 用于加密 TOTP 密钥的 AES-256 密钥（32 字节 hex 编码）
	// 如果为空，将自动生成一个随机密钥（仅适用于开发环境）
	EncryptionKey string `mapstructure:"encryption_key"`
	// EncryptionKeyConfigured 标记加密密钥是否为手动配置（非自动生成）
	// 只有手动配置了密钥才允许在管理后台启用 TOTP 功能
	EncryptionKeyConfigured bool `mapstructure:"-"`
}

type TurnstileConfig struct {
	Required bool `mapstructure:"required"`
}

type DefaultConfig struct {
	AdminEmail      string  `mapstructure:"admin_email"`
	AdminPassword   string  `mapstructure:"admin_password"`
	UserConcurrency int     `mapstructure:"user_concurrency"`
	UserBalance     float64 `mapstructure:"user_balance"`
	APIKeyPrefix    string  `mapstructure:"api_key_prefix"`
	RateMultiplier  float64 `mapstructure:"rate_multiplier"`
}

type RateLimitConfig struct {
	OverloadCooldownMinutes int `mapstructure:"overload_cooldown_minutes"`  // 529过载冷却时间(分钟)
	OAuth401CooldownMinutes int `mapstructure:"oauth_401_cooldown_minutes"` // OAuth 401临时不可调度冷却(分钟)
}

// APIKeyAuthCacheConfig API Key 认证缓存配置
type APIKeyAuthCacheConfig struct {
	L1Size             int  `mapstructure:"l1_size"`
	L1TTLSeconds       int  `mapstructure:"l1_ttl_seconds"`
	L2TTLSeconds       int  `mapstructure:"l2_ttl_seconds"`
	NegativeTTLSeconds int  `mapstructure:"negative_ttl_seconds"`
	JitterPercent      int  `mapstructure:"jitter_percent"`
	Singleflight       bool `mapstructure:"singleflight"`
}

// SubscriptionCacheConfig 订阅认证 L1 缓存配置
type SubscriptionCacheConfig struct {
	L1Size        int `mapstructure:"l1_size"`
	L1TTLSeconds  int `mapstructure:"l1_ttl_seconds"`
	JitterPercent int `mapstructure:"jitter_percent"`
}

// SubscriptionMaintenanceConfig 订阅窗口维护后台任务配置。
// 用于将“请求路径触发的维护动作”有界化，避免高并发下 goroutine 膨胀。
type SubscriptionMaintenanceConfig struct {
	WorkerCount int `mapstructure:"worker_count"`
	QueueSize   int `mapstructure:"queue_size"`
}

// DashboardCacheConfig 仪表盘统计缓存配置
type DashboardCacheConfig struct {
	// Enabled: 是否启用仪表盘缓存
	Enabled bool `mapstructure:"enabled"`
	// KeyPrefix: Redis key 前缀，用于多环境隔离
	KeyPrefix string `mapstructure:"key_prefix"`
	// StatsFreshTTLSeconds: 缓存命中认为“新鲜”的时间窗口（秒）
	StatsFreshTTLSeconds int `mapstructure:"stats_fresh_ttl_seconds"`
	// StatsTTLSeconds: Redis 缓存总 TTL（秒）
	StatsTTLSeconds int `mapstructure:"stats_ttl_seconds"`
	// StatsRefreshTimeoutSeconds: 异步刷新超时（秒）
	StatsRefreshTimeoutSeconds int `mapstructure:"stats_refresh_timeout_seconds"`
}

// DashboardAggregationConfig 仪表盘预聚合配置
type DashboardAggregationConfig struct {
	// Enabled: 是否启用预聚合作业
	Enabled bool `mapstructure:"enabled"`
	// IntervalSeconds: 聚合刷新间隔（秒）
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// LookbackSeconds: 回看窗口（秒）
	LookbackSeconds int `mapstructure:"lookback_seconds"`
	// BackfillEnabled: 是否允许全量回填
	BackfillEnabled bool `mapstructure:"backfill_enabled"`
	// BackfillMaxDays: 回填最大跨度（天）
	BackfillMaxDays int `mapstructure:"backfill_max_days"`
	// Retention: 各表保留窗口（天）
	Retention DashboardAggregationRetentionConfig `mapstructure:"retention"`
	// RecomputeDays: 启动时重算最近 N 天
	RecomputeDays int `mapstructure:"recompute_days"`
}

// DashboardAggregationRetentionConfig 预聚合保留窗口
type DashboardAggregationRetentionConfig struct {
	UsageLogsDays         int `mapstructure:"usage_logs_days"`
	UsageBillingDedupDays int `mapstructure:"usage_billing_dedup_days"`
	HourlyDays            int `mapstructure:"hourly_days"`
	DailyDays             int `mapstructure:"daily_days"`
}

// UsageCleanupConfig 使用记录清理任务配置
type UsageCleanupConfig struct {
	// Enabled: 是否启用清理任务执行器
	Enabled bool `mapstructure:"enabled"`
	// MaxRangeDays: 单次任务允许的最大时间跨度（天）
	MaxRangeDays int `mapstructure:"max_range_days"`
	// BatchSize: 单批删除数量
	BatchSize int `mapstructure:"batch_size"`
	// WorkerIntervalSeconds: 后台任务轮询间隔（秒）
	WorkerIntervalSeconds int `mapstructure:"worker_interval_seconds"`
	// TaskTimeoutSeconds: 单次任务最大执行时长（秒）
	TaskTimeoutSeconds int `mapstructure:"task_timeout_seconds"`
}
