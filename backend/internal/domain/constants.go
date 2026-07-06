package domain

// Status constants
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusError    = "error"
	StatusUnused   = "unused"
	StatusUsed     = "used"
	StatusExpired  = "expired"
)

// Role constants
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Platform constants
const (
	PlatformAnthropic = "anthropic"
	PlatformOpenAI    = "openai"
	PlatformGemini    = "gemini"
	// PlatformGrok 表示 xAI Grok 订阅账号。Grok 使用 OpenAI Responses 形态的入站/出站
	// 数据结构，但在调度、配额、渠道定价与模型目录中必须保持独立平台语义。
	PlatformGrok = "grok"
	// PlatformAntigravity 历史上是一个独立平台值。自 Gemini/Antigravity 合并后，
	// Antigravity 账号在数据库中以 platform="gemini" + sub_platform="antigravity" 存储，
	// 该常量不再作为 accounts.platform 的取值，而是继续承担两个角色：
	//   1. accounts.sub_platform 的取值（见 SubPlatformAntigravity，二者同值）；
	//   2. 向后兼容的“平台别名”——分组 platform、强制平台中间件、API 入参、
	//      配额归因等仍可使用 "antigravity"，由归一化逻辑映射到 gemini+sub_platform。
	PlatformAntigravity = "antigravity"

	// PlatformCustom 是「自定义 Provider」平台。Custom 账号通过自定义 Base URL +
	// API Key 连接任意上游，并显式选择上游协议（见 CustomProtocol* 常量）。
	// Custom 账号不受分组类型限制：可加入任意分组。router 模式下 protocol 表示上游
	// 目标协议，由 ProtocolRouter 在转发前决定转换链；passthrough / full_passthrough
	// 模式下才要求请求入站协议与该 protocol 匹配。
	PlatformCustom = "custom"
)

// Custom protocol constants —— Custom 账号选择的上游协议（存于 accounts.extra["protocol"]）。
// 每个协议对应一个原生转发栈与入站 endpoint。
const (
	CustomProtocolOpenAIResponses       = "openai_responses"        // OpenAI Responses API (/v1/responses)
	CustomProtocolOpenAIChatCompletions = "openai_chat_completions" // OpenAI Chat Completions (/v1/chat/completions)
	CustomProtocolOpenAIEmbeddings      = "openai_embeddings"       // OpenAI Embeddings (/v1/embeddings)
	CustomProtocolAnthropicMessages     = "anthropic_messages"      // Anthropic Messages (/v1/messages)
	CustomProtocolGemini                = "gemini"                  // Gemini (/v1beta/models)
)

// Relay mode constants —— 账号参与网关调度时的请求中转模式（存于 accounts.extra["relay_mode"]）。
const (
	RelayModeRouter          = "router"           // 允许协议转换，由 ProtocolRouter 决定转换链
	RelayModePassthrough     = "passthrough"      // 同协议透传，不做协议转换
	RelayModeFullPassthrough = "full_passthrough" // 完全透传，继承旧的原样转发语义
)

// Sub-platform constants —— 同一 platform 下的账号变体判别符（accounts.sub_platform）。
const (
	// SubPlatformAntigravity 标识 gemini 平台下的 Antigravity 账号。
	// 与 PlatformAntigravity 同值（"antigravity"），便于别名与子平台互转。
	SubPlatformAntigravity = PlatformAntigravity
)

// NormalizePlatform 将外部输入或历史存储的 platform 别名归一化为内部使用的
// (platform, subPlatform) 二元组。兼容旧的 "antigravity" 平台值：
// 返回 ("gemini", "antigravity")。其他平台原样返回，subPlatform 为空。
//
// 写入账户（创建/编辑/导入/OAuth）时应统一经过此函数，确保 Antigravity 账号
// 始终以 platform="gemini" + sub_platform="antigravity" 落库。
func NormalizePlatform(platform string) (normalizedPlatform, subPlatform string) {
	if platform == PlatformAntigravity {
		return PlatformGemini, SubPlatformAntigravity
	}
	return platform, ""
}

// Account type constants
const (
	AccountTypeOAuth          = "oauth"           // OAuth类型账号（full scope: profile + inference）
	AccountTypeSetupToken     = "setup-token"     // Setup Token类型账号（inference only scope）
	AccountTypeAPIKey         = "apikey"          // API Key类型账号
	AccountTypeUpstream       = "upstream"        // 上游透传类型账号（通过 Base URL + API Key 连接上游）
	AccountTypeBedrock        = "bedrock"         // AWS Bedrock 类型账号（通过 SigV4 签名或 API Key 连接 Bedrock，由 credentials.auth_mode 区分）
	AccountTypeServiceAccount = "service_account" // Google Service Account 类型账号（用于 Vertex AI）
)

// Redeem type constants
const (
	RedeemTypeBalance      = "balance"
	RedeemTypeConcurrency  = "concurrency"
	RedeemTypeSubscription = "subscription"
	RedeemTypeInvitation   = "invitation"
)

// PromoCode status constants
const (
	PromoCodeStatusActive   = "active"
	PromoCodeStatusDisabled = "disabled"
)

// Admin adjustment type constants
const (
	AdjustmentTypeAdminBalance     = "admin_balance"     // 管理员调整余额
	AdjustmentTypeAdminConcurrency = "admin_concurrency" // 管理员调整并发数
)

// Group subscription type constants
const (
	SubscriptionTypeStandard     = "standard"     // 标准计费模式（按余额扣费）
	SubscriptionTypeSubscription = "subscription" // 订阅模式（按限额控制）
)

// Subscription status constants
const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusSuspended = "suspended"
)

// DefaultAntigravityModelMapping 是 Antigravity 平台的默认模型映射
// 当账号未配置 model_mapping 时使用此默认值
// 与前端 useModelWhitelist.ts 中的 antigravityDefaultMappings 保持一致
var DefaultAntigravityModelMapping = map[string]string{
	// Claude 白名单
	"claude-opus-4-8":            "claude-opus-4-8",          // 官方模型
	"claude-opus-4-7":            "claude-opus-4-7",          // 官方模型
	"claude-opus-4-6-thinking":   "claude-opus-4-6-thinking", // 官方模型
	"claude-opus-4-6":            "claude-opus-4-6-thinking", // 简称映射
	"claude-opus-4-5-thinking":   "claude-opus-4-6-thinking", // 迁移旧模型
	"claude-fable-5":             "claude-fable-5",
	"claude-sonnet-5":            "claude-sonnet-5",
	"claude-sonnet-4-6":          "claude-sonnet-4-6",
	"claude-sonnet-4-5":          "claude-sonnet-4-5",
	"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
	// Claude 详细版本 ID 映射
	"claude-opus-4-5-20251101":   "claude-opus-4-6-thinking", // 迁移旧模型
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-5",
	// Claude Haiku → Sonnet（无 Haiku 支持）
	"claude-haiku-4-5":          "claude-sonnet-4-6",
	"claude-haiku-4-5-20251001": "claude-sonnet-4-6",
	// Gemini 2.5 白名单
	"gemini-2.5-flash":               "gemini-2.5-flash",
	"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
	"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
	"gemini-2.5-flash-lite":          "gemini-2.5-flash-lite",
	"gemini-2.5-flash-thinking":      "gemini-2.5-flash-thinking",
	"gemini-2.5-pro":                 "gemini-2.5-pro",
	// Gemini 3 白名单
	"gemini-3-flash":    "gemini-3-flash",
	"gemini-3-pro-high": "gemini-3-pro-high",
	"gemini-3-pro-low":  "gemini-3-pro-low",
	// Gemini 3 preview 映射
	"gemini-3-flash-preview": "gemini-3-flash",
	"gemini-3-pro-preview":   "gemini-3-pro-high",
	// Gemini 3.1 白名单
	"gemini-3.1-pro-high": "gemini-3.1-pro-high",
	"gemini-3.1-pro-low":  "gemini-3.1-pro-low",
	// Gemini 3.1 preview 映射
	"gemini-3.1-pro-preview": "gemini-3.1-pro-high",
	// Gemini 3.1 别名映射
	"gemini-3.1-pro": "gemini-3.1-pro-high",
	// Gemini 3.1 image 白名单
	"gemini-3.1-flash-image": "gemini-3.1-flash-image",
	// Gemini 3.1 image preview 映射
	"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
	// Gemini 3 image 兼容映射（向 3.1 image 迁移）
	"gemini-3-pro-image":         "gemini-3.1-flash-image",
	"gemini-3-pro-image-preview": "gemini-3.1-flash-image",
	// 其他官方模型
	"gpt-oss-120b-medium":    "gpt-oss-120b-medium",
	"tab_flash_lite_preview": "tab_flash_lite_preview",
}

// DefaultBedrockModelMapping 是 AWS Bedrock 平台的默认模型映射
// 将 Anthropic 标准模型名映射到 Bedrock 模型 ID
// 注意：此处的 "us." 前缀仅为默认值，ResolveBedrockModelID 会根据账号配置的
// aws_region 自动调整为匹配的区域前缀（如 eu.、apac.、jp. 等）
var DefaultBedrockModelMapping = map[string]string{
	// Claude Opus
	"claude-opus-4-8":          "us.anthropic.claude-opus-4-8-v1",
	"claude-opus-4-7":          "us.anthropic.claude-opus-4-7-v1",
	"claude-opus-4-6-thinking": "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-6":          "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-5-thinking": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-5-20251101": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-1":          "us.anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-opus-4-20250514":   "us.anthropic.claude-opus-4-20250514-v1:0",
	// Claude Fable
	"claude-fable-5": "us.anthropic.claude-fable-5-v1:0",
	// Claude Sonnet
	"claude-sonnet-5":            "us.anthropic.claude-sonnet-5-v1:0",
	"claude-sonnet-4-6-thinking": "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-6":          "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-5":          "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-thinking": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-20250929": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-20250514":   "us.anthropic.claude-sonnet-4-20250514-v1:0",
	// Claude Haiku
	"claude-haiku-4-5":          "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-haiku-4-5-20251001": "us.anthropic.claude-haiku-4-5-20251001-v1:0",
}
