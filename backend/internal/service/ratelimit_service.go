package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
)

// RateLimitService 处理限流和过载状态管理
type RateLimitService struct {
	accountRepo           AccountRepository
	usageRepo             UsageLogRepository
	cfg                   *config.Config
	geminiQuotaService    *GeminiQuotaService
	tempUnschedCache      TempUnschedCache
	timeoutCounterCache   TimeoutCounterCache
	openAI403CounterCache OpenAI403CounterCache
	settingService        *SettingService
	tokenCacheInvalidator TokenCacheInvalidator
	runtimeBlocker        AccountRuntimeBlocker
	usageCacheMu          sync.RWMutex
	usageCache            map[int64]*geminiUsageCacheEntry
}

type AccountRuntimeBlocker interface {
	BlockAccountScheduling(account *Account, until time.Time, reason string)
	ClearAccountSchedulingBlock(accountID int64)
}

// SuccessfulTestRecoveryResult 表示测试成功后恢复了哪些运行时状态。
type SuccessfulTestRecoveryResult struct {
	ClearedError     bool
	ClearedRateLimit bool
}

// AccountRecoveryOptions 控制账号恢复时的附加行为。
type AccountRecoveryOptions struct {
	InvalidateToken bool
}

type geminiUsageCacheEntry struct {
	windowStart time.Time
	cachedAt    time.Time
	totals      GeminiUsageTotals
}

type geminiUsageTotalsBatchProvider interface {
	GetGeminiUsageTotalsBatch(ctx context.Context, accountIDs []int64, startTime, endTime time.Time) (map[int64]GeminiUsageTotals, error)
}

const geminiPrecheckCacheTTL = time.Minute

const (
	defaultRateLimit429CooldownSeconds = 5
	maxRateLimit429CooldownSeconds     = 7200
)

const (
	openAI403CooldownMinutesDefault = 10
	openAI403DisableThreshold       = 3
	openAI403CounterWindowMinutes   = 180
)

// NewRateLimitService 创建RateLimitService实例
func NewRateLimitService(accountRepo AccountRepository, usageRepo UsageLogRepository, cfg *config.Config, geminiQuotaService *GeminiQuotaService, tempUnschedCache TempUnschedCache) *RateLimitService {
	return &RateLimitService{
		accountRepo:        accountRepo,
		usageRepo:          usageRepo,
		cfg:                cfg,
		geminiQuotaService: geminiQuotaService,
		tempUnschedCache:   tempUnschedCache,
		usageCache:         make(map[int64]*geminiUsageCacheEntry),
	}
}

// SetTimeoutCounterCache 设置超时计数器缓存（可选依赖）
func (s *RateLimitService) SetTimeoutCounterCache(cache TimeoutCounterCache) {
	s.timeoutCounterCache = cache
}

// SetOpenAI403CounterCache 设置 OpenAI 403 连续失败计数器（可选依赖）
func (s *RateLimitService) SetOpenAI403CounterCache(cache OpenAI403CounterCache) {
	s.openAI403CounterCache = cache
}

// SetSettingService 设置系统设置服务（可选依赖）
func (s *RateLimitService) SetSettingService(settingService *SettingService) {
	s.settingService = settingService
}

// SetTokenCacheInvalidator 设置 token 缓存清理器（可选依赖）
func (s *RateLimitService) SetTokenCacheInvalidator(invalidator TokenCacheInvalidator) {
	s.tokenCacheInvalidator = invalidator
}

func (s *RateLimitService) SetAccountRuntimeBlocker(blocker AccountRuntimeBlocker) {
	s.runtimeBlocker = blocker
}

func (s *RateLimitService) notifyAccountSchedulingBlocked(account *Account, until time.Time, reason string) {
	if s == nil || s.runtimeBlocker == nil || account == nil {
		return
	}
	s.runtimeBlocker.BlockAccountScheduling(account, until, reason)
}

func (s *RateLimitService) notifyAccountSchedulingBlockCleared(accountID int64) {
	if s == nil || s.runtimeBlocker == nil || accountID <= 0 {
		return
	}
	s.runtimeBlocker.ClearAccountSchedulingBlock(accountID)
}

// ErrorPolicyResult 表示错误策略检查的结果
type ErrorPolicyResult int

const (
	ErrorPolicyNone            ErrorPolicyResult = iota // 未命中任何策略，继续默认逻辑
	ErrorPolicySkipped                                  // 自定义错误码开启但未命中，跳过处理
	ErrorPolicyMatched                                  // 自定义错误码命中，应停止调度
	ErrorPolicyTempUnscheduled                          // 临时不可调度规则命中
)

// CheckErrorPolicy 检查自定义错误码和临时不可调度规则。
// 自定义错误码开启时覆盖后续所有逻辑（包括临时不可调度）。
func (s *RateLimitService) CheckErrorPolicy(ctx context.Context, account *Account, statusCode int, responseBody []byte) ErrorPolicyResult {
	if account.IsCustomErrorCodesEnabled() {
		if account.ShouldHandleErrorCode(statusCode) {
			return ErrorPolicyMatched
		}
		slog.Info("account_error_code_skipped", "account_id", account.ID, "status_code", statusCode)
		return ErrorPolicySkipped
	}
	if account.IsPoolMode() {
		return ErrorPolicySkipped
	}
	if s.tryTempUnschedulable(ctx, account, statusCode, responseBody) {
		return ErrorPolicyTempUnscheduled
	}
	return ErrorPolicyNone
}
