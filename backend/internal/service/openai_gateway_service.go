package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/outbound"
	"github.com/WilliamWang1721/LightBridge/internal/util/responseheaders"
)

const (
	// ChatGPT internal API for OAuth accounts
	chatgptCodexURL = "https://chatgpt.com/backend-api/codex/responses"
	// OpenAI Platform API for API Key accounts (fallback)
	openaiPlatformAPIURL     = "https://api.openai.com/v1/responses"
	openaiStickySessionTTL   = time.Hour // 粘性会话TTL
	codexCLIUserAgent        = "codex_cli_rs/0.125.0"
	grokQuotaPauseStaleAfter = 2 * time.Hour
	// codex_cli_only 拒绝时单个请求头日志长度上限（字符）
	codexCLIOnlyHeaderValueMaxBytes = 256

	// OpenAIParsedRequestBodyKey 缓存 handler 侧已解析的请求体，避免重复解析。
	OpenAIParsedRequestBodyKey = "openai_parsed_request_body"
	// OpenAI WS Mode 失败后的重连次数上限（不含首次尝试）。
	// 与 Codex 客户端保持一致：失败后最多重连 5 次。
	openAIWSReconnectRetryLimit = 5
	// OpenAI WS Mode 重连退避默认值（可由配置覆盖）。
	openAIWSRetryBackoffInitialDefault = 120 * time.Millisecond
	openAIWSRetryBackoffMaxDefault     = 2 * time.Second
	openAIWSRetryJitterRatioDefault    = 0.2
	openAICompactSessionSeedKey        = "openai_compact_session_seed"
	codexCLIVersion                    = "0.125.0"
	// Codex 限额快照仅用于后台展示/诊断，不需要每个成功请求都立即落库。
	openAICodexSnapshotPersistMinInterval = 30 * time.Second
)

// OpenAI allowed headers whitelist (for non-passthrough).
var openaiAllowedHeaders = map[string]bool{
	"accept-language":       true,
	"content-type":          true,
	"conversation_id":       true,
	"user-agent":            true,
	"originator":            true,
	"session_id":            true,
	"x-codex-turn-state":    true,
	"x-codex-turn-metadata": true,
}

// OpenAI passthrough allowed headers whitelist.
// 透传模式下仅放行这些低风险请求头，避免将非标准/环境噪声头传给上游触发风控。
var openaiPassthroughAllowedHeaders = map[string]bool{
	"accept":                true,
	"accept-language":       true,
	"content-type":          true,
	"conversation_id":       true,
	"openai-beta":           true,
	"user-agent":            true,
	"originator":            true,
	"session_id":            true,
	"x-codex-turn-state":    true,
	"x-codex-turn-metadata": true,
}

// codex_cli_only 拒绝时记录的请求头白名单（仅用于诊断日志，不参与上游透传）
var codexCLIOnlyDebugHeaderWhitelist = []string{
	"User-Agent",
	"Content-Type",
	"Accept",
	"Accept-Language",
	"OpenAI-Beta",
	"Originator",
	"Session_ID",
	"Conversation_ID",
	"X-Request-ID",
	"X-Client-Request-ID",
	"X-Forwarded-For",
	"X-Real-IP",
}

// OpenAICodexUsageSnapshot represents Codex API usage limits from response headers
type OpenAICodexUsageSnapshot struct {
	PrimaryUsedPercent          *float64 `json:"primary_used_percent,omitempty"`
	PrimaryResetAfterSeconds    *int     `json:"primary_reset_after_seconds,omitempty"`
	PrimaryWindowMinutes        *int     `json:"primary_window_minutes,omitempty"`
	SecondaryUsedPercent        *float64 `json:"secondary_used_percent,omitempty"`
	SecondaryResetAfterSeconds  *int     `json:"secondary_reset_after_seconds,omitempty"`
	SecondaryWindowMinutes      *int     `json:"secondary_window_minutes,omitempty"`
	PrimaryOverSecondaryPercent *float64 `json:"primary_over_secondary_percent,omitempty"`
	UpdatedAt                   string   `json:"updated_at,omitempty"`
}

// NormalizedCodexLimits contains normalized 5h/7d rate limit data
type NormalizedCodexLimits struct {
	Used5hPercent   *float64
	Reset5hSeconds  *int
	Window5hMinutes *int
	Used7dPercent   *float64
	Reset7dSeconds  *int
	Window7dMinutes *int
}

// Normalize converts primary/secondary fields to canonical 5h/7d fields.
// Strategy: Compare window_minutes to determine which is 5h vs 7d.
// Returns nil if snapshot is nil or has no useful data.
func (s *OpenAICodexUsageSnapshot) Normalize() *NormalizedCodexLimits {
	if s == nil {
		return nil
	}

	result := &NormalizedCodexLimits{}

	primaryMins := 0
	secondaryMins := 0
	hasPrimaryWindow := false
	hasSecondaryWindow := false

	if s.PrimaryWindowMinutes != nil {
		primaryMins = *s.PrimaryWindowMinutes
		hasPrimaryWindow = true
	}
	if s.SecondaryWindowMinutes != nil {
		secondaryMins = *s.SecondaryWindowMinutes
		hasSecondaryWindow = true
	}

	// Determine mapping based on window_minutes
	use5hFromPrimary := false
	use7dFromPrimary := false

	if hasPrimaryWindow && hasSecondaryWindow {
		// Both known: smaller window is 5h, larger is 7d
		if primaryMins < secondaryMins {
			use5hFromPrimary = true
		} else {
			use7dFromPrimary = true
		}
	} else if hasPrimaryWindow {
		// Only primary known: classify by threshold (<=360 min = 6h -> 5h window)
		if primaryMins <= 360 {
			use5hFromPrimary = true
		} else {
			use7dFromPrimary = true
		}
	} else if hasSecondaryWindow {
		// Only secondary known: classify by threshold
		if secondaryMins <= 360 {
			// 5h from secondary, so primary (if any data) is 7d
			use7dFromPrimary = true
		} else {
			// 7d from secondary, so primary (if any data) is 5h
			use5hFromPrimary = true
		}
	} else {
		// No window_minutes: fall back to legacy assumption (primary=7d, secondary=5h)
		use7dFromPrimary = true
	}

	// Assign values
	if use5hFromPrimary {
		result.Used5hPercent = s.PrimaryUsedPercent
		result.Reset5hSeconds = s.PrimaryResetAfterSeconds
		result.Window5hMinutes = s.PrimaryWindowMinutes
		result.Used7dPercent = s.SecondaryUsedPercent
		result.Reset7dSeconds = s.SecondaryResetAfterSeconds
		result.Window7dMinutes = s.SecondaryWindowMinutes
	} else if use7dFromPrimary {
		result.Used7dPercent = s.PrimaryUsedPercent
		result.Reset7dSeconds = s.PrimaryResetAfterSeconds
		result.Window7dMinutes = s.PrimaryWindowMinutes
		result.Used5hPercent = s.SecondaryUsedPercent
		result.Reset5hSeconds = s.SecondaryResetAfterSeconds
		result.Window5hMinutes = s.SecondaryWindowMinutes
	}

	return result
}

// OpenAIUsage represents OpenAI API response usage
type OpenAIUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	ImageOutputTokens        int `json:"image_output_tokens,omitempty"`
}

// OpenAIForwardResult represents the result of forwarding
type OpenAIForwardResult struct {
	RequestID  string
	ResponseID string
	Usage      OpenAIUsage
	Model      string // 原始模型（用于响应和日志显示）
	// BillingModel is the model used for cost calculation.
	// When non-empty, CalculateCost uses this instead of Model.
	// This is set by the Anthropic Messages conversion path where
	// the mapped upstream model differs from the client-facing model.
	BillingModel string
	// UpstreamModel is the actual model sent to the upstream provider after mapping.
	// Empty when no mapping was applied (requested model was used as-is).
	UpstreamModel string
	// ServiceTier records the OpenAI Responses API service tier, e.g. "priority" / "flex".
	// Nil means the request did not specify a recognized tier.
	ServiceTier *string
	// ReasoningEffort is extracted from request body (reasoning.effort) or derived from model suffix.
	// Stored for usage records display; nil means not provided / not applicable.
	ReasoningEffort     *string
	Stream              bool
	OpenAIWSMode        bool
	ResponseHeaders     http.Header
	Duration            time.Duration
	FirstTokenMs        *int
	ImageCount          int
	ImageSize           string
	ImageInputSize      string
	ImageOutputSize     string
	ImageOutputSizes    []string
	ImageSizeSource     string
	ImageSizeBreakdown  map[string]int
	wsReplayInput       []json.RawMessage
	wsReplayInputExists bool
}

type OpenAIWSRetryMetricsSnapshot struct {
	RetryAttemptsTotal            int64 `json:"retry_attempts_total"`
	RetryBackoffMsTotal           int64 `json:"retry_backoff_ms_total"`
	RetryExhaustedTotal           int64 `json:"retry_exhausted_total"`
	NonRetryableFastFallbackTotal int64 `json:"non_retryable_fast_fallback_total"`
}

type OpenAICompatibilityFallbackMetricsSnapshot struct {
	SessionHashLegacyReadFallbackTotal int64   `json:"session_hash_legacy_read_fallback_total"`
	SessionHashLegacyReadFallbackHit   int64   `json:"session_hash_legacy_read_fallback_hit"`
	SessionHashLegacyDualWriteTotal    int64   `json:"session_hash_legacy_dual_write_total"`
	SessionHashLegacyReadHitRate       float64 `json:"session_hash_legacy_read_hit_rate"`

	MetadataLegacyFallbackIsMaxTokensOneHaikuTotal int64 `json:"metadata_legacy_fallback_is_max_tokens_one_haiku_total"`
	MetadataLegacyFallbackThinkingEnabledTotal     int64 `json:"metadata_legacy_fallback_thinking_enabled_total"`
	MetadataLegacyFallbackPrefetchedStickyAccount  int64 `json:"metadata_legacy_fallback_prefetched_sticky_account_total"`
	MetadataLegacyFallbackPrefetchedStickyGroup    int64 `json:"metadata_legacy_fallback_prefetched_sticky_group_total"`
	MetadataLegacyFallbackSingleAccountRetryTotal  int64 `json:"metadata_legacy_fallback_single_account_retry_total"`
	MetadataLegacyFallbackAccountSwitchCountTotal  int64 `json:"metadata_legacy_fallback_account_switch_count_total"`
	MetadataLegacyFallbackTotal                    int64 `json:"metadata_legacy_fallback_total"`
}

type openAIWSRetryMetrics struct {
	retryAttempts            atomic.Int64
	retryBackoffMs           atomic.Int64
	retryExhausted           atomic.Int64
	nonRetryableFastFallback atomic.Int64
}

type accountWriteThrottle struct {
	minInterval time.Duration
	mu          sync.Mutex
	lastByID    map[int64]time.Time
}

func newAccountWriteThrottle(minInterval time.Duration) *accountWriteThrottle {
	return &accountWriteThrottle{
		minInterval: minInterval,
		lastByID:    make(map[int64]time.Time),
	}
}

func (t *accountWriteThrottle) Allow(id int64, now time.Time) bool {
	if t == nil || id <= 0 || t.minInterval <= 0 {
		return true
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if last, ok := t.lastByID[id]; ok && now.Sub(last) < t.minInterval {
		return false
	}
	t.lastByID[id] = now

	if len(t.lastByID) > 4096 {
		cutoff := now.Add(-4 * t.minInterval)
		for accountID, writtenAt := range t.lastByID {
			if writtenAt.Before(cutoff) {
				delete(t.lastByID, accountID)
			}
		}
	}

	return true
}

var defaultOpenAICodexSnapshotPersistThrottle = newAccountWriteThrottle(openAICodexSnapshotPersistMinInterval)

// ErrNoAvailableCompactAccounts indicates the request needs /responses/compact
// support but no compatible account is available.
var ErrNoAvailableCompactAccounts = errors.New("no available OpenAI accounts support /responses/compact")

// OpenAIGatewayService handles OpenAI API gateway operations
type OpenAIGatewayService struct {
	accountRepo           AccountRepository
	usageLogRepo          UsageLogRepository
	usageBillingRepo      UsageBillingRepository
	userRepo              UserRepository
	userSubRepo           UserSubscriptionRepository
	cache                 GatewayCache
	cfg                   *config.Config
	codexDetector         CodexClientRestrictionDetector
	schedulerSnapshot     *SchedulerSnapshotService
	concurrencyService    *ConcurrencyService
	billingService        *BillingService
	rateLimitService      *RateLimitService
	billingCacheService   *BillingCacheService
	userGroupRateResolver *userGroupRateResolver
	httpUpstream          HTTPUpstream
	deferredService       *DeferredService
	openAITokenProvider   *OpenAITokenProvider
	grokTokenProvider     *GrokTokenProvider
	toolCorrector         *CodexToolCorrector
	openaiWSResolver      OpenAIWSProtocolResolver
	resolver              *ModelPricingResolver
	channelService        *ChannelService
	balanceNotifyService  *BalanceNotifyService
	settingService        *SettingService
	userPlatformQuotaRepo UserPlatformQuotaRepository
	outboundRegistry      *outbound.Registry

	openaiWSPoolOnce              sync.Once
	openaiWSStateStoreOnce        sync.Once
	openaiSchedulerOnce           sync.Once
	openaiWSPassthroughDialerOnce sync.Once
	openaiWSPool                  *openAIWSConnPool
	openaiWSStateStore            OpenAIWSStateStore
	openaiScheduler               OpenAIAccountScheduler
	openaiWSPassthroughDialer     openAIWSClientDialer
	openaiAccountStats            *openAIAccountRuntimeStats

	openaiWSFallbackUntil               sync.Map // key: int64(accountID), value: time.Time
	openaiAccountRuntimeBlockUntil      sync.Map // key: int64(accountID), value: time.Time
	openaiOAuth429WindowStartUnixNano   atomic.Int64
	openaiOAuth429WindowCount           atomic.Int64
	openaiWSRetryMetrics                openAIWSRetryMetrics
	responseHeaderFilter                *responseheaders.CompiledHeaderFilter
	codexSnapshotThrottle               *accountWriteThrottle
	openaiCompatSessionResponses        sync.Map
	openaiCompatAnthropicDigestSessions sync.Map
}

// NewOpenAIGatewayService creates a new OpenAIGatewayService
func NewOpenAIGatewayService(
	accountRepo AccountRepository,
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
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	grokTokenProvider *GrokTokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	outboundRegistry *outbound.Registry,
) *OpenAIGatewayService {
	svc := &OpenAIGatewayService{
		accountRepo:         accountRepo,
		usageLogRepo:        usageLogRepo,
		usageBillingRepo:    usageBillingRepo,
		userRepo:            userRepo,
		userSubRepo:         userSubRepo,
		cache:               cache,
		cfg:                 cfg,
		codexDetector:       NewOpenAICodexClientRestrictionDetector(cfg),
		schedulerSnapshot:   schedulerSnapshot,
		concurrencyService:  concurrencyService,
		billingService:      billingService,
		rateLimitService:    rateLimitService,
		billingCacheService: billingCacheService,
		userGroupRateResolver: newUserGroupRateResolver(
			userGroupRateRepo,
			nil,
			resolveUserGroupRateCacheTTL(cfg),
			nil,
			"service.openai_gateway",
		),
		httpUpstream:          httpUpstream,
		deferredService:       deferredService,
		openAITokenProvider:   openAITokenProvider,
		grokTokenProvider:     grokTokenProvider,
		toolCorrector:         NewCodexToolCorrector(),
		openaiWSResolver:      NewOpenAIWSProtocolResolver(cfg),
		resolver:              resolver,
		channelService:        channelService,
		balanceNotifyService:  balanceNotifyService,
		settingService:        settingService,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
		outboundRegistry:      outboundRegistry,
		responseHeaderFilter:  compileResponseHeaderFilter(cfg),
		codexSnapshotThrottle: newAccountWriteThrottle(openAICodexSnapshotPersistMinInterval),
	}
	if rateLimitService != nil {
		rateLimitService.SetAccountRuntimeBlocker(svc)
	}
	if openAITokenProvider != nil {
		openAITokenProvider.SetAccountRuntimeBlocker(svc)
	}
	svc.logOpenAIWSModeBootstrap()
	return svc
}

// ResolveChannelMapping 解析渠道级模型映射（代理到 ChannelService）
func (s *OpenAIGatewayService) ResolveChannelMapping(ctx context.Context, groupID int64, model string) ChannelMappingResult {
	if s.channelService == nil {
		return ChannelMappingResult{MappedModel: model}
	}
	return s.channelService.ResolveChannelMapping(ctx, groupID, model)
}

// IsModelRestricted 检查模型是否被渠道限制（代理到 ChannelService）
func (s *OpenAIGatewayService) IsModelRestricted(ctx context.Context, groupID int64, model string) bool {
	if s.channelService == nil {
		return false
	}
	return s.channelService.IsModelRestricted(ctx, groupID, model)
}

// ResolveChannelMappingAndRestrict 解析渠道映射。
// 模型限制检查已移至调度阶段，restricted 始终返回 false。
func (s *OpenAIGatewayService) ResolveChannelMappingAndRestrict(ctx context.Context, groupID *int64, model string) (ChannelMappingResult, bool) {
	if s.channelService == nil {
		return ChannelMappingResult{MappedModel: model}, false
	}
	return s.channelService.ResolveChannelMappingAndRestrict(ctx, groupID, model)
}

func (s *OpenAIGatewayService) isCodexImageGenerationBridgeEnabled(ctx context.Context, account *Account, apiKey *APIKey) bool {
	return s.codexImageGenerationBridgeMode(ctx, account, apiKey) == CodexImageGenerationBridgeForce
}

func (s *OpenAIGatewayService) codexImageGenerationBridgeMode(ctx context.Context, account *Account, apiKey *APIKey) CodexImageGenerationBridgeMode {
	if account == nil || account.Platform != PlatformOpenAI {
		return CodexImageGenerationBridgeOff
	}
	if mode := account.CodexImageGenerationBridgeOverride(); mode != CodexImageGenerationBridgeFollow {
		return mode
	}
	if s != nil && s.channelService != nil && apiKey != nil && apiKey.GroupID != nil {
		ch, err := s.channelService.GetChannelForGroup(ctx, *apiKey.GroupID)
		if err != nil {
			slog.Warn("failed to resolve codex image generation bridge channel override", "group_id", *apiKey.GroupID, "error", err)
		} else if mode := ch.CodexImageGenerationBridgeOverride(PlatformOpenAI); mode != CodexImageGenerationBridgeFollow {
			return mode
		}
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.CodexImageGenerationBridgeEnabled {
		return CodexImageGenerationBridgeForce
	}
	return CodexImageGenerationBridgeFollow
}

func (s *OpenAIGatewayService) checkChannelPricingRestriction(ctx context.Context, groupID *int64, requestedModel string) bool {
	return false
}

func (s *OpenAIGatewayService) isUpstreamModelRestrictedByChannel(ctx context.Context, groupID int64, account *Account, requestedModel string, requireCompact bool) bool {
	return false
}

func (s *OpenAIGatewayService) needsUpstreamChannelRestrictionCheck(ctx context.Context, groupID *int64) bool {
	return false
}

// ReplaceModelInBody 替换请求体中的 JSON model 字段（通用 gjson/sjson 实现）。
func (s *OpenAIGatewayService) ReplaceModelInBody(body []byte, newModel string) []byte {
	return ReplaceModelInBody(body, newModel)
}

func (s *OpenAIGatewayService) getCodexSnapshotThrottle() *accountWriteThrottle {
	if s != nil && s.codexSnapshotThrottle != nil {
		return s.codexSnapshotThrottle
	}
	return defaultOpenAICodexSnapshotPersistThrottle
}

func (s *OpenAIGatewayService) billingDeps() *billingDeps {
	return &billingDeps{
		accountRepo:           s.accountRepo,
		userRepo:              s.userRepo,
		userSubRepo:           s.userSubRepo,
		billingCacheService:   s.billingCacheService,
		deferredService:       s.deferredService,
		balanceNotifyService:  s.balanceNotifyService,
		userPlatformQuotaRepo: s.userPlatformQuotaRepo,
	}
}
