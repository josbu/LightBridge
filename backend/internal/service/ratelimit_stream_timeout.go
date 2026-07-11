package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// HandleStreamTimeout 处理流数据超时
// 根据系统设置决定是否标记账户为临时不可调度或错误状态
// 返回是否应该停止该账号的调度
func (s *RateLimitService) HandleStreamTimeout(ctx context.Context, account *Account, model string) bool {
	if account == nil {
		return false
	}

	// 获取系统设置
	if s.settingService == nil {
		slog.Warn("stream_timeout_setting_service_missing", "account_id", account.ID)
		return false
	}

	settings, err := s.settingService.GetStreamTimeoutSettings(ctx)
	if err != nil {
		slog.Warn("stream_timeout_get_settings_failed", "account_id", account.ID, "error", err)
		return false
	}

	if !settings.Enabled {
		return false
	}

	if settings.Action == StreamTimeoutActionNone {
		return false
	}

	// 增加超时计数
	var count int64 = 1
	if s.timeoutCounterCache != nil {
		count, err = s.timeoutCounterCache.IncrementTimeoutCount(ctx, account.ID, settings.ThresholdWindowMinutes)
		if err != nil {
			slog.Warn("stream_timeout_increment_count_failed", "account_id", account.ID, "error", err)
			// 继续处理，使用 count=1
			count = 1
		}
	}

	slog.Info("stream_timeout_count", "account_id", account.ID, "count", count, "threshold", settings.ThresholdCount, "window_minutes", settings.ThresholdWindowMinutes, "model", model)

	// 检查是否达到阈值
	if count < int64(settings.ThresholdCount) {
		return false
	}

	// 达到阈值，执行相应操作
	switch settings.Action {
	case StreamTimeoutActionTempUnsched:
		return s.triggerStreamTimeoutTempUnsched(ctx, account, settings, model)
	case StreamTimeoutActionError:
		return s.triggerStreamTimeoutError(ctx, account, model)
	default:
		return false
	}
}

// triggerStreamTimeoutTempUnsched 触发流超时临时不可调度
func (s *RateLimitService) triggerStreamTimeoutTempUnsched(ctx context.Context, account *Account, settings *StreamTimeoutSettings, model string) bool {
	now := time.Now()
	until := now.Add(time.Duration(settings.TempUnschedMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      0, // 超时没有状态码
		MatchedKeyword:  "stream_timeout",
		RuleIndex:       -1, // 表示系统级规则
		ErrorMessage:    "Stream data interval timeout for model: " + model,
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = state.ErrorMessage
	}

	s.notifyAccountSchedulingBlocked(account, until, "stream_timeout_temp_unschedulable")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("stream_timeout_set_temp_unsched_failed", "account_id", account.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state); err != nil {
			slog.Warn("stream_timeout_set_temp_unsched_cache_failed", "account_id", account.ID, "error", err)
		}
	}

	// 重置超时计数
	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, account.ID); err != nil {
			slog.Warn("stream_timeout_reset_count_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Info("stream_timeout_temp_unschedulable", "account_id", account.ID, "until", until, "model", model)
	return true
}

// triggerStreamTimeoutError 触发流超时错误状态
func (s *RateLimitService) triggerStreamTimeoutError(ctx context.Context, account *Account, model string) bool {
	errorMsg := "Stream data interval timeout (repeated failures) for model: " + model

	s.notifyAccountSchedulingBlocked(account, time.Time{}, "stream_timeout_error")
	if err := s.accountRepo.SetError(ctx, account.ID, errorMsg); err != nil {
		slog.Warn("stream_timeout_set_error_failed", "account_id", account.ID, "error", err)
		return false
	}

	// 重置超时计数
	if s.timeoutCounterCache != nil {
		if err := s.timeoutCounterCache.ResetTimeoutCount(ctx, account.ID); err != nil {
			slog.Warn("stream_timeout_reset_count_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Warn("stream_timeout_account_error", "account_id", account.ID, "model", model)
	return true
}
