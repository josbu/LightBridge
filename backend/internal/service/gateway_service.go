package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/modules"
	"github.com/WilliamWang1721/LightBridge/internal/outbound"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/util/responseheaders"
	gocache "github.com/patrickmn/go-cache"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

const (
	claudeAPIURL            = "https://api.anthropic.com/v1/messages?beta=true"
	claudeAPICountTokensURL = "https://api.anthropic.com/v1/messages/count_tokens?beta=true"
	stickySessionTTL        = time.Hour // 粘性会话TTL
	defaultMaxLineSize      = 500 * 1024 * 1024
	// Canonical Claude Code banner. Keep it EXACT (no trailing whitespace/newlines)
	// to match real Claude CLI traffic as closely as possible. When we need a visual
	// separator between system blocks, we add "\n\n" at concatenation time.
	claudeCodeSystemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."
	maxCacheControlBlocks  = 4 // Anthropic API 允许的最大 cache_control 块数量

	defaultUserGroupRateCacheTTL = 30 * time.Second
	defaultModelsListCacheTTL    = 15 * time.Second
	postUsageBillingTimeout      = 15 * time.Second
	debugGatewayBodyEnv          = "LightBridge_DEBUG_GATEWAY_BODY"
)

const (
	claudeMimicDebugInfoKey = "claude_mimic_debug_info"
)

const (
	cacheTTLTarget5m = "5m"
	cacheTTLTarget1h = "1h"
)

// ForceCacheBillingContextKey 强制缓存计费上下文键
// 用于粘性会话切换时，将 input_tokens 转为 cache_read_input_tokens 计费
type forceCacheBillingKeyType struct{}

// accountWithLoad 账号与负载信息的组合，用于负载感知调度
type accountWithLoad struct {
	account  *Account
	loadInfo *AccountLoadInfo
}

var ForceCacheBillingContextKey = forceCacheBillingKeyType{}

var (
	windowCostPrefetchCacheHitTotal  atomic.Int64
	windowCostPrefetchCacheMissTotal atomic.Int64
	windowCostPrefetchBatchSQLTotal  atomic.Int64
	windowCostPrefetchFallbackTotal  atomic.Int64
	windowCostPrefetchErrorTotal     atomic.Int64

	userGroupRateCacheHitTotal      atomic.Int64
	userGroupRateCacheMissTotal     atomic.Int64
	userGroupRateCacheLoadTotal     atomic.Int64
	userGroupRateCacheSFSharedTotal atomic.Int64
	userGroupRateCacheFallbackTotal atomic.Int64

	modelsListCacheHitTotal   atomic.Int64
	modelsListCacheMissTotal  atomic.Int64
	modelsListCacheStoreTotal atomic.Int64

	// Deprecated: flusher_enabled=true 后不再增长(仅 flag=false 降级直写路径使用);新主路径见 FlusherMetrics。remove after 2026-09。
	// userPlatformQuotaDBIncrErrorTotal 统计主计费路径中
	// IncrementUsageWithReset 失败次数。Redis 已成功累加 + DB 写失败意味着
	// Redis cache TTL 过期或被清后该笔 cost 会丢失（与实际消费偏差）。
	// oncall 通过 GatewayUserPlatformQuotaIncrStats() 暴露给 ops 面板做阈值告警。
	userPlatformQuotaDBIncrErrorTotal atomic.Int64
	// Deprecated: flusher_enabled=true 后不再增长(仅 flag=false 降级直写路径使用);新主路径见 FlusherMetrics。remove after 2026-09。
	// userPlatformQuotaDBIncrLegacyErrorTotal 统计 legacy postUsageBilling
	// （applyUsageBilling 在 repo==nil 时 fallback）路径下的失败次数；
	// 与 DB Incr 失败分开计数，便于区分"主路径暂时故障"vs"基础设施长期未配齐"。
	userPlatformQuotaDBIncrLegacyErrorTotal atomic.Int64
	// userPlatformQuotaSentinelSetCacheErrorTotal 统计 checkUserPlatformQuotaEligibility
	// 在 DB 无行时回填 sentinel cache entry 写 Redis 失败的次数（phase A）。
	userPlatformQuotaSentinelSetCacheErrorTotal atomic.Int64
)

func GatewayWindowCostPrefetchStats() (cacheHit, cacheMiss, batchSQL, fallback, errCount int64) {
	return windowCostPrefetchCacheHitTotal.Load(),
		windowCostPrefetchCacheMissTotal.Load(),
		windowCostPrefetchBatchSQLTotal.Load(),
		windowCostPrefetchFallbackTotal.Load(),
		windowCostPrefetchErrorTotal.Load()
}

func GatewayUserGroupRateCacheStats() (cacheHit, cacheMiss, load, singleflightShared, fallback int64) {
	return userGroupRateCacheHitTotal.Load(),
		userGroupRateCacheMissTotal.Load(),
		userGroupRateCacheLoadTotal.Load(),
		userGroupRateCacheSFSharedTotal.Load(),
		userGroupRateCacheFallbackTotal.Load()
}

func GatewayModelsListCacheStats() (cacheHit, cacheMiss, store int64) {
	return modelsListCacheHitTotal.Load(), modelsListCacheMissTotal.Load(), modelsListCacheStoreTotal.Load()
}

// GatewayUserPlatformQuotaIncrStats 返回 (mainPathErr, legacyPathErr, sentinelSetErr)。
// mainPathErr：主计费路径同步镜像写 DB 失败累计次数；
// legacyPathErr：postUsageBilling fallback 路径写 DB 失败累计次数；
// sentinelSetErr：DB 无行时回填 sentinel cache entry 写 Redis 失败累计次数。
// ops 监控面板可以按"持续上升斜率"做告警阈值。
func GatewayUserPlatformQuotaIncrStats() (mainPathErr, legacyPathErr, sentinelSetErr int64) {
	return userPlatformQuotaDBIncrErrorTotal.Load(),
		userPlatformQuotaDBIncrLegacyErrorTotal.Load(),
		userPlatformQuotaSentinelSetCacheErrorTotal.Load()
}

// GatewayUserPlatformQuotaFlusherStats 暴露 flusher 运行指标供 ops/health 面板查询。
func GatewayUserPlatformQuotaFlusherStats(f *UserPlatformQuotaUsageFlusher) map[string]int64 {
	if f == nil || f.metrics == nil {
		return nil
	}
	m := f.metrics
	return map[string]int64{
		"flush_success":        m.FlushSuccessTotal.Load(),
		"flush_error":          m.FlushErrorTotal.Load(),
		"flush_batch_size":     m.FlushBatchSizeTotal.Load(),
		"flush_latency_ms_max": m.FlushLatencyMsMax.Load(),
		"dirty_readd":          m.DirtyReaddTotal.Load(),
		"dirty_lost":           m.DirtyLostTotal.Load(),
		"flush_fk_violation":   m.FlushFKViolationTotal.Load(),
	}
}

func openAIStreamEventIsTerminal(data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	switch gjson.Get(trimmed, "type").String() {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func anthropicStreamEventIsTerminal(eventName, data string) bool {
	if strings.EqualFold(strings.TrimSpace(eventName), "message_stop") {
		return true
	}
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return gjson.Get(trimmed, "type").String() == "message_stop"
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// IsForceCacheBilling 检查是否启用强制缓存计费
func IsForceCacheBilling(ctx context.Context) bool {
	v, _ := ctx.Value(ForceCacheBillingContextKey).(bool)
	return v
}

// WithForceCacheBilling 返回带有强制缓存计费标记的上下文
func WithForceCacheBilling(ctx context.Context) context.Context {
	return context.WithValue(ctx, ForceCacheBillingContextKey, true)
}

func (s *GatewayService) debugModelRoutingEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugModelRouting.Load()
}

func (s *GatewayService) debugClaudeMimicEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugClaudeMimic.Load()
}

func parseDebugEnvBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shortSessionHash(sessionHash string) string {
	if sessionHash == "" {
		return ""
	}
	if len(sessionHash) <= 8 {
		return sessionHash
	}
	return sessionHash[:8]
}

func redactAuthHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Keep scheme for debugging, redact secret.
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "Bearer [redacted]"
	}
	return "[redacted]"
}

func safeHeaderValueForLog(key string, v string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "authorization", "x-api-key":
		return redactAuthHeaderValue(v)
	default:
		return strings.TrimSpace(v)
	}
}

func extractSystemPreviewFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sys := gjson.GetBytes(body, "system")
	if !sys.Exists() {
		return ""
	}

	switch {
	case sys.IsArray():
		for _, item := range sys.Array() {
			if !item.IsObject() {
				continue
			}
			if strings.EqualFold(item.Get("type").String(), "text") {
				if t := item.Get("text").String(); strings.TrimSpace(t) != "" {
					return t
				}
			}
		}
		return ""
	case sys.Type == gjson.String:
		return sys.String()
	default:
		return ""
	}
}

func buildClaudeMimicDebugLine(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) string {
	if req == nil {
		return ""
	}

	// Only log a minimal fingerprint to avoid leaking user content.
	interesting := []string{
		"user-agent",
		"x-app",
		"anthropic-dangerous-direct-browser-access",
		"anthropic-version",
		"anthropic-beta",
		"x-stainless-lang",
		"x-stainless-package-version",
		"x-stainless-os",
		"x-stainless-arch",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
		"x-stainless-retry-count",
		"x-stainless-timeout",
		"authorization",
		"x-api-key",
		"content-type",
		"accept",
		"x-stainless-helper-method",
	}

	h := make([]string, 0, len(interesting))
	for _, k := range interesting {
		if v := req.Header.Get(k); v != "" {
			h = append(h, fmt.Sprintf("%s=%q", k, safeHeaderValueForLog(k, v)))
		}
	}

	metaUserID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	sysPreview := strings.TrimSpace(extractSystemPreviewFromBody(body))

	// Truncate preview to keep logs sane.
	if len(sysPreview) > 300 {
		sysPreview = sysPreview[:300] + "..."
	}
	sysPreview = strings.ReplaceAll(sysPreview, "\n", "\\n")
	sysPreview = strings.ReplaceAll(sysPreview, "\r", "\\r")

	aid := int64(0)
	aname := ""
	if account != nil {
		aid = account.ID
		aname = account.Name
	}

	return fmt.Sprintf(
		"url=%s account=%d(%s) tokenType=%s mimic=%t meta.user_id=%q system.preview=%q headers={%s}",
		req.URL.String(),
		aid,
		aname,
		tokenType,
		mimicClaudeCode,
		metaUserID,
		sysPreview,
		strings.Join(h, " "),
	)
}

func logClaudeMimicDebug(req *http.Request, body []byte, account *Account, tokenType string, mimicClaudeCode bool) {
	line := buildClaudeMimicDebugLine(req, body, account, tokenType, mimicClaudeCode)
	if line == "" {
		return
	}
	logger.LegacyPrintf("service.gateway", "[ClaudeMimicDebug] %s", line)
}

func isClaudeCodeCredentialScopeError(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	return strings.Contains(m, "only authorized for use with claude code") &&
		strings.Contains(m, "cannot be used for other api requests")
}

// sseDataRe matches SSE data lines with optional whitespace after colon.
// Some upstream APIs return non-standard "data:" without space (should be "data: ").
var (
	sseDataRe            = regexp.MustCompile(`^data:\s*`)
	claudeCliUserAgentRe = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+`)

	// claudeCodePromptPrefixes 用于检测 Claude Code 系统提示词的前缀列表
	// 支持多种变体：标准版、Agent SDK 版、Explore Agent 版、Compact 版等
	// 注意：前缀之间不应存在包含关系，否则会导致冗余匹配
	claudeCodePromptPrefixes = []string{
		"You are Claude Code, Anthropic's official CLI for Claude",             // 标准版 & Agent SDK 版（含 running within...）
		"You are a Claude agent, built on Anthropic's Claude Agent SDK",        // Agent SDK 变体
		"You are a file search specialist for Claude Code",                     // Explore Agent 版
		"You are a helpful AI assistant tasked with summarizing conversations", // Compact 版
	}
)

// ErrNoAvailableAccounts 表示没有可用的账号
var ErrNoAvailableAccounts = errors.New("no available accounts")

// ErrClaudeCodeOnly 表示分组仅允许 Claude Code 客户端访问
var ErrClaudeCodeOnly = errors.New("this group only allows Claude Code clients")

// allowedHeaders 白名单headers（参考CRS项目）
var allowedHeaders = map[string]bool{
	"accept":                                    true,
	"x-stainless-retry-count":                   true,
	"x-stainless-timeout":                       true,
	"x-stainless-lang":                          true,
	"x-stainless-package-version":               true,
	"x-stainless-os":                            true,
	"x-stainless-arch":                          true,
	"x-stainless-runtime":                       true,
	"x-stainless-runtime-version":               true,
	"x-stainless-helper-method":                 true,
	"anthropic-dangerous-direct-browser-access": true,
	"anthropic-version":                         true,
	"x-app":                                     true,
	"anthropic-beta":                            true,
	"accept-language":                           true,
	"sec-fetch-mode":                            true,
	"user-agent":                                true,
	"content-type":                              true,
	"accept-encoding":                           true,
	"x-claude-code-session-id":                  true,
	"x-client-request-id":                       true,
}

// GatewayCache 定义网关服务的缓存操作接口。
// 提供粘性会话（Sticky Session）的存储、查询、刷新和删除功能。
//
// GatewayCache defines cache operations for gateway service.
// Provides sticky session storage, retrieval, refresh and deletion capabilities.
type GatewayCache interface {
	// GetSessionAccountID 获取粘性会话绑定的账号 ID
	// Get the account ID bound to a sticky session
	GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error)
	// SetSessionAccountID 设置粘性会话与账号的绑定关系
	// Set the binding between sticky session and account
	SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error
	// RefreshSessionTTL 刷新粘性会话的过期时间
	// Refresh the expiration time of a sticky session
	RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error
	// DeleteSessionAccountID 删除粘性会话绑定，用于账号不可用时主动清理
	// Delete sticky session binding, used to proactively clean up when account becomes unavailable
	DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error
}

// derefGroupID safely dereferences *int64 to int64, returning 0 if nil
func derefGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func resolveUserGroupRateCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.UserGroupRateCacheTTLSeconds <= 0 {
		return defaultUserGroupRateCacheTTL
	}
	return time.Duration(cfg.Gateway.UserGroupRateCacheTTLSeconds) * time.Second
}

func resolveModelsListCacheTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.ModelsListCacheTTLSeconds <= 0 {
		return defaultModelsListCacheTTL
	}
	return time.Duration(cfg.Gateway.ModelsListCacheTTLSeconds) * time.Second
}

func modelsListCacheKey(groupID *int64, platform string) string {
	return fmt.Sprintf("%d|%s", derefGroupID(groupID), strings.TrimSpace(platform))
}

func prefetchedStickyGroupIDFromContext(ctx context.Context) (int64, bool) {
	return PrefetchedStickyGroupIDFromContext(ctx)
}

func prefetchedStickyAccountIDFromContext(ctx context.Context, groupID *int64) int64 {
	prefetchedGroupID, ok := prefetchedStickyGroupIDFromContext(ctx)
	if !ok || prefetchedGroupID != derefGroupID(groupID) {
		return 0
	}
	if accountID, ok := PrefetchedStickyAccountIDFromContext(ctx); ok && accountID > 0 {
		return accountID
	}
	return 0
}

// shouldClearStickySession 检查账号是否处于不可调度状态，需要清理粘性会话绑定。
// 委托 IsSchedulable() 判断账号级可调度性（状态、配额、过载、限流等），
// 额外检查模型级限流。
//
// shouldClearStickySession checks if an account is in an unschedulable state
// and the sticky session binding should be cleared.
// Delegates to IsSchedulable() for account-level checks, plus model-level rate limiting.
func shouldClearStickySession(account *Account, requestedModel string) bool {
	if account == nil {
		return false
	}
	if !account.IsSchedulable() {
		return true
	}
	if remaining := account.GetRateLimitRemainingTimeWithContext(context.Background(), requestedModel); remaining > 0 {
		return true
	}
	return false
}

type AccountWaitPlan struct {
	AccountID      int64
	MaxConcurrency int
	Timeout        time.Duration
	MaxWaiting     int
}

type AccountSelectionResult struct {
	Account     *Account
	Acquired    bool
	ReleaseFunc func()
	WaitPlan    *AccountWaitPlan // nil means no wait allowed
}

// ClaudeUsage 表示Claude API返回的usage信息
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreation5mTokens    int // 5分钟缓存创建token（来自嵌套 cache_creation 对象）
	CacheCreation1hTokens    int // 1小时缓存创建token（来自嵌套 cache_creation 对象）
	ImageOutputTokens        int `json:"image_output_tokens,omitempty"`
}

// ForwardResult 转发结果
type ForwardResult struct {
	RequestID string
	Usage     ClaudeUsage
	Model     string
	// UpstreamModel is the actual upstream model after mapping.
	// Prefer empty when it is identical to Model; persistence normalizes equal values away as no-op mappings.
	UpstreamModel    string
	Stream           bool
	Duration         time.Duration
	FirstTokenMs     *int // 首字时间（流式请求）
	ClientDisconnect bool // 客户端是否在流式传输过程中断开
	ReasoningEffort  *string

	// 图片生成计费字段（图片生成模型使用）
	ImageCount         int    // 生成的图片数量
	ImageSize          string // 最终计费尺寸 "1K", "2K", "4K"
	ImageInputSize     string // 请求中的原始图片尺寸
	ImageOutputSize    string // 上游响应中的图片尺寸
	ImageOutputSizes   []string
	ImageSizeSource    string
	ImageSizeBreakdown map[string]int
}

// UpstreamFailoverError indicates an upstream error that should trigger account failover.
type UpstreamFailoverError struct {
	StatusCode             int
	ResponseBody           []byte      // 上游响应体，用于错误透传规则匹配
	ResponseHeaders        http.Header // 上游响应头，用于透传 cf-ray/cf-mitigated/content-type 等诊断信息
	ForceCacheBilling      bool        // Antigravity 粘性会话切换时设为 true
	RetryableOnSameAccount bool        // 临时性错误（如 Google 间歇性 400、空响应），应在同一账号上重试 N 次再切换
}

func (e *UpstreamFailoverError) Error() string {
	return fmt.Sprintf("upstream error: %d (failover)", e.StatusCode)
}

// TempUnscheduleRetryableError 对 RetryableOnSameAccount 类型的 failover 错误触发临时封禁。
// 由 handler 层在同账号重试全部用尽、切换账号时调用。
func (s *GatewayService) TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return
	}
	// 根据状态码选择封禁策略
	switch failoverErr.StatusCode {
	case http.StatusBadRequest:
		tempUnscheduleGoogleConfigError(ctx, s.accountRepo, accountID, "[handler]")
	case http.StatusBadGateway:
		tempUnscheduleEmptyResponse(ctx, s.accountRepo, accountID, "[handler]")
	}
}

// GatewayService handles API gateway operations
type GatewayService struct {
	accountRepo           AccountRepository
	groupRepo             GroupRepository
	usageLogRepo          UsageLogRepository
	usageBillingRepo      UsageBillingRepository
	userRepo              UserRepository
	userSubRepo           UserSubscriptionRepository
	userGroupRateRepo     UserGroupRateRepository
	cache                 GatewayCache
	digestStore           *DigestSessionStore
	cfg                   *config.Config
	schedulerSnapshot     *SchedulerSnapshotService
	billingService        *BillingService
	rateLimitService      *RateLimitService
	billingCacheService   *BillingCacheService
	identityService       *IdentityService
	httpUpstream          HTTPUpstream
	deferredService       *DeferredService
	concurrencyService    *ConcurrencyService
	claudeTokenProvider   *ClaudeTokenProvider
	sessionLimitCache     SessionLimitCache // 会话数量限制缓存（仅 Anthropic OAuth/SetupToken）
	rpmCache              RPMCache          // RPM 计数缓存（仅 Anthropic OAuth/SetupToken）
	userGroupRateResolver *userGroupRateResolver
	userGroupRateCache    *gocache.Cache
	userGroupRateSF       singleflight.Group
	modelsListCache       *gocache.Cache
	modelsListCacheTTL    time.Duration
	settingService        *SettingService
	responseHeaderFilter  *responseheaders.CompiledHeaderFilter
	debugModelRouting     atomic.Bool
	debugClaudeMimic      atomic.Bool
	channelService        *ChannelService
	resolver              *ModelPricingResolver
	debugGatewayBodyFile  atomic.Pointer[os.File] // non-nil when LightBridge_DEBUG_GATEWAY_BODY is set
	tlsFPProfileService   *TLSFingerprintProfileService
	balanceNotifyService  *BalanceNotifyService
	userPlatformQuotaRepo UserPlatformQuotaRepository
	providerRegistry      *modules.ProviderRegistry
	outboundRegistry      *outbound.Registry
}

// NewGatewayService creates a new GatewayService
func NewGatewayService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	identityService *IdentityService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	claudeTokenProvider *ClaudeTokenProvider,
	sessionLimitCache SessionLimitCache,
	rpmCache RPMCache,
	digestStore *DigestSessionStore,
	settingService *SettingService,
	tlsFPProfileService *TLSFingerprintProfileService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
	balanceNotifyService *BalanceNotifyService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	outboundRegistry *outbound.Registry,
) *GatewayService {
	userGroupRateTTL := resolveUserGroupRateCacheTTL(cfg)
	modelsListTTL := resolveModelsListCacheTTL(cfg)

	svc := &GatewayService{
		accountRepo:           accountRepo,
		groupRepo:             groupRepo,
		usageLogRepo:          usageLogRepo,
		usageBillingRepo:      usageBillingRepo,
		userRepo:              userRepo,
		userSubRepo:           userSubRepo,
		userGroupRateRepo:     userGroupRateRepo,
		cache:                 cache,
		digestStore:           digestStore,
		cfg:                   cfg,
		schedulerSnapshot:     schedulerSnapshot,
		concurrencyService:    concurrencyService,
		billingService:        billingService,
		rateLimitService:      rateLimitService,
		billingCacheService:   billingCacheService,
		identityService:       identityService,
		httpUpstream:          httpUpstream,
		deferredService:       deferredService,
		claudeTokenProvider:   claudeTokenProvider,
		sessionLimitCache:     sessionLimitCache,
		rpmCache:              rpmCache,
		userGroupRateCache:    gocache.New(userGroupRateTTL, time.Minute),
		settingService:        settingService,
		modelsListCache:       gocache.New(modelsListTTL, time.Minute),
		modelsListCacheTTL:    modelsListTTL,
		responseHeaderFilter:  compileResponseHeaderFilter(cfg),
		tlsFPProfileService:   tlsFPProfileService,
		channelService:        channelService,
		resolver:              resolver,
		balanceNotifyService:  balanceNotifyService,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
		outboundRegistry:      outboundRegistry,
	}
	svc.userGroupRateResolver = newUserGroupRateResolver(
		userGroupRateRepo,
		svc.userGroupRateCache,
		userGroupRateTTL,
		&svc.userGroupRateSF,
		"service.gateway",
	)
	svc.debugModelRouting.Store(parseDebugEnvBool(os.Getenv("LightBridge_DEBUG_MODEL_ROUTING")))
	svc.debugClaudeMimic.Store(parseDebugEnvBool(os.Getenv("LightBridge_DEBUG_CLAUDE_MIMIC")))
	if path := strings.TrimSpace(os.Getenv(debugGatewayBodyEnv)); path != "" {
		svc.initDebugGatewayBodyFile(path)
	}
	return svc
}
