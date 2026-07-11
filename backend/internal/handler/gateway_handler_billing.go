package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"

	pkgerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"go.uber.org/zap"
)

// extractQuotaResetSeconds 从 quota 错误的 metadata 中提取 window_resets_at 并计算
// 距重置剩余秒数。fallback 路径必须返回 ≥1 秒，避免客户端立即重试无限循环。
func extractQuotaResetSeconds(err error) int {
	const fallback = 60
	appErr := pkgerrors.FromError(err)
	if appErr == nil {
		return fallback
	}
	raw, ok := appErr.Metadata["window_resets_at"]
	if !ok || raw == "" {
		return fallback
	}
	resetAt, parseErr := time.Parse(time.RFC3339, raw)
	if parseErr != nil {
		logger.L().With(
			zap.String("component", "handler.gateway.billing"),
			zap.String("raw", raw),
			zap.Error(parseErr),
		).Warn("quota.invalid_window_resets_at_format")
		return fallback
	}
	secs := time.Until(resetAt).Seconds()
	if secs <= 0 {
		// reset 时间已过：cache 与 DB 应该正在自愈，返回 fallback 让客户端按常规节奏退避，
		// 避免返回 1 秒导致客户端立即重试仍触发限额的退避循环。
		return fallback
	}
	return int(math.Ceil(secs))
}

func billingErrorDetails(err error) (status int, code, message string, retryAfter int) {
	if errors.Is(err, service.ErrBillingServiceUnavailable) {
		msg := pkgerrors.Message(err)
		if msg == "" {
			msg = "Billing service temporarily unavailable. Please retry later."
		}
		return http.StatusServiceUnavailable, "billing_service_error", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit1dExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded) {
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, 0
	}
	// 用户/分组 RPM 超限统一映射为 HTTP 429；保留与其它 rate_limit 一致的错误码便于客户端分类。
	// 返回 Retry-After 秒数（当前分钟剩余秒数），让 SDK 自动退避。
	if errors.Is(err, service.ErrGroupRPMExceeded) || errors.Is(err, service.ErrUserRPMExceeded) {
		msg := pkgerrors.Message(err)
		retrySeconds := 60 - int(time.Now().Unix()%60)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, retrySeconds
	}
	if errors.Is(err, service.ErrUserPlatformDailyQuotaExhausted) ||
		errors.Is(err, service.ErrUserPlatformWeeklyQuotaExhausted) ||
		errors.Is(err, service.ErrUserPlatformMonthlyQuotaExhausted) {
		// 与 RPM 超限一致映射 429 + Retry-After，让 SDK 自动退避（而非 403 直接失败）。
		// 错误码用 rate_limit_exceeded 与 OpenAI 兼容客户端一致；细分类型由 ErrCode + window_resets_at metadata 区分。
		msg := pkgerrors.Message(err)
		return http.StatusTooManyRequests, "rate_limit_exceeded", msg, extractQuotaResetSeconds(err)
	}
	msg := pkgerrors.Message(err)
	if msg == "" {
		logger.L().With(
			zap.String("component", "handler.gateway.billing"),
			zap.Error(err),
		).Warn("gateway.billing_error_missing_message")
		msg = "Billing error"
	}
	return http.StatusForbidden, "billing_error", msg, 0
}

func (h *GatewayHandler) metadataBridgeEnabled() bool {
	if h == nil || h.cfg == nil {
		return true
	}
	return h.cfg.Gateway.OpenAIWS.MetadataBridgeEnabled
}

func (h *GatewayHandler) maybeLogCompatibilityFallbackMetrics(reqLog *zap.Logger) {
	if reqLog == nil {
		return
	}
	if gatewayCompatibilityMetricsLogCounter.Add(1)%gatewayCompatibilityMetricsLogInterval != 0 {
		return
	}
	metrics := service.SnapshotOpenAICompatibilityFallbackMetrics()
	reqLog.Info("gateway.compatibility_fallback_metrics",
		zap.Int64("session_hash_legacy_read_fallback_total", metrics.SessionHashLegacyReadFallbackTotal),
		zap.Int64("session_hash_legacy_read_fallback_hit", metrics.SessionHashLegacyReadFallbackHit),
		zap.Int64("session_hash_legacy_dual_write_total", metrics.SessionHashLegacyDualWriteTotal),
		zap.Float64("session_hash_legacy_read_hit_rate", metrics.SessionHashLegacyReadHitRate),
		zap.Int64("metadata_legacy_fallback_total", metrics.MetadataLegacyFallbackTotal),
	)
}

func (h *GatewayHandler) submitUsageRecordTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if h.usageRecordWorkerPool != nil {
		h.usageRecordWorkerPool.Submit(task)
		return
	}
	// 回退路径：worker 池未注入时同步执行，避免退回到无界 goroutine 模式。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.messages"),
				zap.Any("panic", recovered),
			).Error("gateway.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

// getUserMsgQueueMode 获取当前请求的 UMQ 模式
// 返回 "serialize" | "throttle" | ""
func (h *GatewayHandler) getUserMsgQueueMode(account *service.Account, parsed *service.ParsedRequest) string {
	if h.userMsgQueueHelper == nil {
		return ""
	}
	// 仅适用于 Anthropic OAuth/SetupToken 账号
	if !account.IsAnthropicOAuthOrSetupToken() {
		return ""
	}
	if !service.IsRealUserMessage(parsed) {
		return ""
	}
	// 账号级模式优先，fallback 到全局配置
	mode := account.GetUserMsgQueueMode()
	if mode == "" {
		mode = h.cfg.Gateway.UserMessageQueue.GetEffectiveMode()
	}
	return mode
}
