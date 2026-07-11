package service

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// handle529 处理529过载错误
// 根据配置决定是否暂停账号调度及冷却时长
func (s *RateLimitService) handle529(ctx context.Context, account *Account) {
	var settings *OverloadCooldownSettings
	if s.settingService != nil {
		var err error
		settings, err = s.settingService.GetOverloadCooldownSettings(ctx)
		if err != nil {
			slog.Warn("overload_settings_read_failed", "account_id", account.ID, "error", err)
			settings = nil
		}
	}
	// 回退到配置文件
	if settings == nil {
		cooldown := s.cfg.RateLimit.OverloadCooldownMinutes
		if cooldown <= 0 {
			cooldown = 10
		}
		settings = &OverloadCooldownSettings{Enabled: true, CooldownMinutes: cooldown}
	}

	if !settings.Enabled {
		slog.Info("account_529_ignored", "account_id", account.ID, "reason", "overload_cooldown_disabled")
		return
	}

	cooldownMinutes := settings.CooldownMinutes
	if cooldownMinutes <= 0 {
		cooldownMinutes = 10
	}

	until := time.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
	s.notifyAccountSchedulingBlocked(account, until, "529")
	if err := s.accountRepo.SetOverloaded(ctx, account.ID, until); err != nil {
		slog.Warn("overload_set_failed", "account_id", account.ID, "error", err)
		return
	}

	slog.Info("account_overloaded", "account_id", account.ID, "until", until)
}

// UpdateSessionWindow 从成功响应更新5h窗口状态
func (s *RateLimitService) UpdateSessionWindow(ctx context.Context, account *Account, headers http.Header) {
	status := headers.Get("anthropic-ratelimit-unified-5h-status")
	if status == "" {
		return
	}

	// 检查是否需要初始化时间窗口
	// 对于 Setup Token 账号，首次成功请求时需要预测时间窗口
	var windowStart, windowEnd *time.Time
	needInitWindow := account.SessionWindowEnd == nil || time.Now().After(*account.SessionWindowEnd)

	// 优先使用响应头中的真实重置时间（比预测更准确）
	if resetStr := headers.Get("anthropic-ratelimit-unified-5h-reset"); resetStr != "" {
		if ts, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			// 检测可能的毫秒时间戳（秒级约为 1e9，毫秒约为 1e12）
			if ts > 1e11 {
				slog.Warn("account_session_window_header_millis_detected", "account_id", account.ID, "raw_reset", resetStr)
				ts = ts / 1000
			}
			end := time.Unix(ts, 0)
			// 校验时间戳是否在合理范围内（不早于 5h 前，不晚于 7 天后）
			minAllowed := time.Now().Add(-5 * time.Hour)
			maxAllowed := time.Now().Add(7 * 24 * time.Hour)
			if end.Before(minAllowed) || end.After(maxAllowed) {
				slog.Warn("account_session_window_header_out_of_range", "account_id", account.ID, "raw_reset", resetStr, "parsed_end", end)
			} else if needInitWindow || account.SessionWindowEnd == nil || !end.Equal(*account.SessionWindowEnd) {
				// 窗口需要初始化，或者真实重置时间与已存储的不同，则更新
				start := end.Add(-5 * time.Hour)
				windowStart = &start
				windowEnd = &end
				slog.Info("account_session_window_from_header", "account_id", account.ID, "window_start", start, "window_end", end, "status", status)
			}
		} else {
			slog.Warn("account_session_window_header_parse_failed", "account_id", account.ID, "raw_reset", resetStr, "error", err)
		}
	}

	// 回退：如果没有真实重置时间且需要初始化窗口，使用预测
	if windowEnd == nil && needInitWindow && (status == "allowed" || status == "allowed_warning") {
		now := time.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		end := start.Add(5 * time.Hour)
		windowStart = &start
		windowEnd = &end
		slog.Info("account_session_window_initialized", "account_id", account.ID, "window_start", start, "window_end", end, "status", status)
	}

	// 窗口重置时清除旧的 utilization 和被动采样数据，避免残留上个窗口的数据
	if windowEnd != nil && needInitWindow {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			"session_window_utilization":      nil,
			"passive_usage_7d_utilization":    nil,
			"passive_usage_7d_reset":          nil,
			"passive_usage_7d_oi_utilization": nil,
			"passive_usage_7d_oi_reset":       nil,
			"passive_usage_sampled_at":        nil,
		})
	}

	if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, windowStart, windowEnd, status); err != nil {
		slog.Warn("session_window_update_failed", "account_id", account.ID, "error", err)
	}

	s.samplePassiveUsageFromHeaders(ctx, account, headers)

	// 如果状态为allowed且之前有限流，说明窗口已重置，清除限流状态
	if status == "allowed" && account.IsRateLimited() {
		if err := s.ClearRateLimit(ctx, account.ID); err != nil {
			slog.Warn("rate_limit_clear_failed", "account_id", account.ID, "error", err)
		}
	}
}

// samplePassiveUsageFromHeaders 从 Anthropic 响应头收集 5h/7d/7d_oi 的
// utilization 与 reset 被动采样数据，合并为一次 Extra 写入。无数据时不写。
func (s *RateLimitService) samplePassiveUsageFromHeaders(ctx context.Context, account *Account, headers http.Header) {
	extraUpdates := make(map[string]any, 6)
	// 5h utilization（0-1 小数），供 estimateSetupTokenUsage 使用
	if utilStr := headers.Get("anthropic-ratelimit-unified-5h-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			extraUpdates["session_window_utilization"] = util
		}
	}
	// 7d utilization（0-1 小数）
	if utilStr := headers.Get("anthropic-ratelimit-unified-7d-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			extraUpdates["passive_usage_7d_utilization"] = util
		}
	}
	// 7d reset timestamp
	if resetStr := headers.Get("anthropic-ratelimit-unified-7d-reset"); resetStr != "" {
		if ts, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			if ts > 1e11 {
				ts = ts / 1000
			}
			extraUpdates["passive_usage_7d_reset"] = ts
		}
	}
	// 7d_oi (Fable 专属 7d 窗口) utilization（0-1 小数）
	if utilStr := headers.Get("anthropic-ratelimit-unified-7d_oi-utilization"); utilStr != "" {
		if util, err := strconv.ParseFloat(utilStr, 64); err == nil {
			extraUpdates["passive_usage_7d_oi_utilization"] = util
		}
	}
	// 7d_oi reset timestamp
	if resetStr := headers.Get("anthropic-ratelimit-unified-7d_oi-reset"); resetStr != "" {
		if ts, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
			if ts > 1e11 {
				ts = ts / 1000
			}
			extraUpdates["passive_usage_7d_oi_reset"] = ts
		}
	}
	if len(extraUpdates) > 0 {
		extraUpdates["passive_usage_sampled_at"] = time.Now().UTC().Format(time.RFC3339)
		if err := s.accountRepo.UpdateExtra(ctx, account.ID, extraUpdates); err != nil {
			slog.Warn("passive_usage_update_failed", "account_id", account.ID, "error", err)
		}
	}
}
