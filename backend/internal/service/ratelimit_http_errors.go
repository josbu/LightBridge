package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleAuthError 处理认证类错误(401/403)，停止账号调度
func (s *RateLimitService) handleAuthError(ctx context.Context, account *Account, errorMsg string) {
	s.notifyAccountSchedulingBlocked(account, time.Time{}, "auth_error")
	if err := s.accountRepo.SetError(ctx, account.ID, errorMsg); err != nil {
		slog.Warn("account_set_error_failed", "account_id", account.ID, "error", err)
		return
	}
	slog.Warn("account_disabled_auth_error", "account_id", account.ID, "error", errorMsg)
}

func buildForbiddenErrorMessage(prefix string, upstreamMsg string, responseBody []byte, fallback string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}

	if msg := strings.TrimSpace(upstreamMsg); msg != "" {
		return prefix + msg
	}

	rawBody := bytes.TrimSpace(responseBody)
	if len(rawBody) > 0 {
		if json.Valid(rawBody) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, rawBody); err == nil {
				return prefix + truncateForLog(compact.Bytes(), 512)
			}
		}
		return prefix + truncateForLog(rawBody, 512)
	}

	return prefix + fallback
}

// handle403 处理 403 Forbidden 错误
// Antigravity 平台区分 validation/violation/generic 三种类型，均 SetError 永久禁用；
// 其他平台保持原有 SetError 行为。
func (s *RateLimitService) handle403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	if account.IsAntigravity() {
		return s.handleAntigravity403(ctx, account, upstreamMsg, responseBody)
	}
	if account.Platform == PlatformOpenAI {
		return s.handleOpenAI403(ctx, account, upstreamMsg, responseBody)
	}
	// 非 Antigravity 平台：保持原有行为
	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		upstreamMsg,
		responseBody,
		"account may be suspended or lack permissions",
	)
	s.handleAuthError(ctx, account, msg)
	return true
}

func (s *RateLimitService) handleOpenAI403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	msg := buildForbiddenErrorMessage(
		"Access forbidden (403):",
		upstreamMsg,
		responseBody,
		"account may be suspended or lack permissions",
	)

	if s.openAI403CounterCache == nil {
		s.handleAuthError(ctx, account, msg)
		return true
	}

	count, err := s.openAI403CounterCache.IncrementOpenAI403Count(ctx, account.ID, openAI403CounterWindowMinutes)
	if err != nil {
		slog.Warn("openai_403_increment_failed", "account_id", account.ID, "error", err)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	if count >= openAI403DisableThreshold {
		msg = fmt.Sprintf("%s | consecutive_403=%d/%d", msg, count, openAI403DisableThreshold)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	until := time.Now().Add(time.Duration(openAI403CooldownMinutesDefault) * time.Minute)
	reason := fmt.Sprintf("OpenAI 403 temporary cooldown (%d/%d): %s", count, openAI403DisableThreshold, msg)
	s.notifyAccountSchedulingBlocked(account, until, "openai_403_temp")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("openai_403_set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
		s.handleAuthError(ctx, account, msg)
		return true
	}

	slog.Warn(
		"openai_403_temp_unschedulable",
		"account_id", account.ID,
		"until", until,
		"count", count,
		"threshold", openAI403DisableThreshold,
	)
	return true
}

// handleAntigravity403 处理 Antigravity 平台的 403 错误
// validation（需要验证）→ 永久 SetError（需人工去 Google 验证后恢复）
// violation（违规封号）→ 永久 SetError（需人工处理）
// generic（通用禁止）→ 永久 SetError
func (s *RateLimitService) handleAntigravity403(ctx context.Context, account *Account, upstreamMsg string, responseBody []byte) (shouldDisable bool) {
	fbType := classifyForbiddenType(string(responseBody))

	switch fbType {
	case forbiddenTypeValidation:
		// VALIDATION_REQUIRED: 永久禁用，需人工去 Google 验证后手动恢复
		msg := buildForbiddenErrorMessage(
			"Validation required (403):",
			upstreamMsg,
			responseBody,
			"account needs Google verification",
		)
		if validationURL := extractValidationURL(string(responseBody)); validationURL != "" {
			msg += " | validation_url: " + validationURL
		}
		s.handleAuthError(ctx, account, msg)
		return true

	case forbiddenTypeViolation:
		// 违规封号: 永久禁用，需人工处理
		msg := buildForbiddenErrorMessage(
			"Account violation (403):",
			upstreamMsg,
			responseBody,
			"terms of service violation",
		)
		s.handleAuthError(ctx, account, msg)
		return true

	default:
		// 通用 403: 保持原有行为
		msg := buildForbiddenErrorMessage(
			"Access forbidden (403):",
			upstreamMsg,
			responseBody,
			"account may be suspended or lack permissions",
		)
		s.handleAuthError(ctx, account, msg)
		return true
	}
}

// handleCustomErrorCode 处理自定义错误码，停止账号调度
func (s *RateLimitService) handleCustomErrorCode(ctx context.Context, account *Account, statusCode int, errorMsg string) {
	msg := "Custom error code " + strconv.Itoa(statusCode) + ": " + errorMsg
	s.notifyAccountSchedulingBlocked(account, time.Time{}, "custom_error_code")
	if err := s.accountRepo.SetError(ctx, account.ID, msg); err != nil {
		slog.Warn("account_set_error_failed", "account_id", account.ID, "status_code", statusCode, "error", err)
		return
	}
	slog.Warn("account_disabled_custom_error", "account_id", account.ID, "status_code", statusCode, "error", errorMsg)
}

// handle429 处理429限流错误
// 解析响应头获取重置时间，标记账号为限流状态
func (s *RateLimitService) handle429(ctx context.Context, account *Account, headers http.Header, responseBody []byte) {
	// 1. OpenAI 平台：优先尝试解析 x-codex-* 响应头（用于 rate_limit_exceeded）
	if account.Platform == PlatformOpenAI {
		persistOpenAI429PlanType(ctx, s.accountRepo, account, responseBody)
		s.persistOpenAICodexSnapshot(ctx, account, headers)
		if resetAt := s.calculateOpenAI429ResetTime(headers); resetAt != nil {
			s.notifyAccountSchedulingBlocked(account, *resetAt, "429")
			if err := s.accountRepo.SetRateLimited(ctx, account.ID, *resetAt); err != nil {
				slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
				return
			}
			slog.Info("openai_account_rate_limited", "account_id", account.ID, "reset_at", *resetAt)
			return
		}
	}

	// 2. Anthropic 平台：尝试解析 per-window 头（5h / 7d），选择实际触发的窗口
	if result := calculateAnthropic429ResetTime(headers); result != nil {
		s.notifyAccountSchedulingBlocked(account, result.resetAt, "429")
		if err := s.accountRepo.SetRateLimited(ctx, account.ID, result.resetAt); err != nil {
			slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
			return
		}

		// 更新 session window：优先使用 5h-reset 头精确计算，否则从 resetAt 反推
		windowEnd := result.resetAt
		if result.fiveHourReset != nil {
			windowEnd = *result.fiveHourReset
		}
		windowStart := windowEnd.Add(-5 * time.Hour)
		if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, &windowStart, &windowEnd, "rejected"); err != nil {
			slog.Warn("rate_limit_update_session_window_failed", "account_id", account.ID, "error", err)
		}

		slog.Info("anthropic_account_rate_limited", "account_id", account.ID, "reset_at", result.resetAt, "reset_in", time.Until(result.resetAt).Truncate(time.Second))
		return
	}

	// 3. 尝试从响应头解析重置时间（Anthropic 聚合头，向后兼容）
	resetTimestamp := headers.Get("anthropic-ratelimit-unified-reset")

	// 4. 如果响应头没有，尝试从响应体解析（OpenAI usage_limit_reached, Gemini）
	if resetTimestamp == "" {
		switch account.Platform {
		case PlatformOpenAI:
			// 尝试解析 OpenAI 的 usage_limit_reached 错误
			if resetAt := parseOpenAIRateLimitResetTime(responseBody); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				s.notifyAccountSchedulingBlocked(account, resetTime, "429")
				if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetTime); err != nil {
					slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
					return
				}
				slog.Info("account_rate_limited", "account_id", account.ID, "platform", account.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		case PlatformGemini, PlatformAntigravity:
			// 尝试解析 Gemini 格式（用于其他平台）
			if resetAt := ParseGeminiRateLimitResetTime(responseBody); resetAt != nil {
				resetTime := time.Unix(*resetAt, 0)
				s.notifyAccountSchedulingBlocked(account, resetTime, "429")
				if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetTime); err != nil {
					slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
					return
				}
				slog.Info("account_rate_limited", "account_id", account.ID, "platform", account.Platform, "reset_at", resetTime, "reset_in", time.Until(resetTime).Truncate(time.Second))
				return
			}
		}

		// Anthropic 平台：没有限流重置时间的 429 可能是非真实限流（如 Extra usage required），
		// 不标记账号限流状态，直接透传错误给客户端
		if account.Platform == PlatformAnthropic {
			slog.Warn("rate_limit_429_no_reset_time_skipped",
				"account_id", account.ID,
				"platform", account.Platform,
				"reason", "no rate limit reset time in headers, likely not a real rate limit")
			return
		}

		// 其他平台：没有重置时间，使用可配置的秒级默认回避，避免误伤长时间不可调度。
		s.apply429FallbackRateLimit(ctx, account, "no_reset_time")
		return
	}

	// 解析Unix时间戳
	ts, err := strconv.ParseInt(resetTimestamp, 10, 64)
	if err != nil {
		slog.Warn("rate_limit_reset_parse_failed", "reset_timestamp", resetTimestamp, "error", err)
		s.apply429FallbackRateLimit(ctx, account, "reset_parse_failed")
		return
	}

	resetAt := time.Unix(ts, 0)

	// 标记限流状态
	s.notifyAccountSchedulingBlocked(account, resetAt, "429")
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
		slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
		return
	}

	// 根据重置时间反推5h窗口
	windowEnd := resetAt
	windowStart := resetAt.Add(-5 * time.Hour)
	if err := s.accountRepo.UpdateSessionWindow(ctx, account.ID, &windowStart, &windowEnd, "rejected"); err != nil {
		slog.Warn("rate_limit_update_session_window_failed", "account_id", account.ID, "error", err)
	}

	slog.Info("account_rate_limited", "account_id", account.ID, "reset_at", resetAt)
}

func (s *RateLimitService) apply429FallbackRateLimit(ctx context.Context, account *Account, reason string) {
	cooldown, enabled := s.get429FallbackCooldown(ctx, account)
	if !enabled {
		slog.Info("rate_limit_429_fallback_ignored", "account_id", account.ID, "platform", account.Platform, "reason", reason)
		return
	}

	resetAt := time.Now().Add(cooldown)
	slog.Warn("rate_limit_429_fallback_used", "account_id", account.ID, "platform", account.Platform, "reason", reason, "using_default", cooldown.String())
	s.notifyAccountSchedulingBlocked(account, resetAt, "429_fallback")
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
		slog.Warn("rate_limit_set_failed", "account_id", account.ID, "error", err)
	}
}

func (s *RateLimitService) get429FallbackCooldown(ctx context.Context, account *Account) (time.Duration, bool) {
	if s.settingService != nil {
		settings, err := s.settingService.GetRateLimit429CooldownSettings(ctx)
		if err == nil && settings != nil {
			if !settings.Enabled {
				return 0, false
			}
			seconds := clampRateLimit429CooldownSeconds(settings.CooldownSeconds)
			return time.Duration(seconds) * time.Second, true
		}
		slog.Warn("rate_limit_429_settings_read_failed", "account_id", account.ID, "error", err)
	}

	seconds := defaultRateLimit429CooldownSeconds
	seconds = clampRateLimit429CooldownSeconds(seconds)
	return time.Duration(seconds) * time.Second, true
}

func clampRateLimit429CooldownSeconds(seconds int) int {
	if seconds < 1 {
		return 1
	}
	if seconds > maxRateLimit429CooldownSeconds {
		return maxRateLimit429CooldownSeconds
	}
	return seconds
}

// calculateOpenAI429ResetTime 从 OpenAI 429 响应头计算正确的重置时间
// 返回 nil 表示无法从响应头中确定重置时间
func calculateOpenAI429ResetTime(headers http.Header) *time.Time {
	snapshot := ParseCodexRateLimitHeaders(headers)
	if snapshot == nil {
		return nil
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		return nil
	}

	now := time.Now()

	// 判断哪个限制被触发（used_percent >= 100）
	is7dExhausted := normalized.Used7dPercent != nil && *normalized.Used7dPercent >= 100
	is5hExhausted := normalized.Used5hPercent != nil && *normalized.Used5hPercent >= 100

	// 优先使用被触发限制的重置时间
	if is7dExhausted && normalized.Reset7dSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset7dSeconds) * time.Second)
		slog.Info("openai_429_7d_limit_exhausted", "reset_after_seconds", *normalized.Reset7dSeconds, "reset_at", resetAt)
		return &resetAt
	}
	if is5hExhausted && normalized.Reset5hSeconds != nil {
		resetAt := now.Add(time.Duration(*normalized.Reset5hSeconds) * time.Second)
		slog.Info("openai_429_5h_limit_exhausted", "reset_after_seconds", *normalized.Reset5hSeconds, "reset_at", resetAt)
		return &resetAt
	}

	// 都未达到100%但收到429，使用较长的重置时间
	var maxResetSecs int
	if normalized.Reset7dSeconds != nil && *normalized.Reset7dSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset7dSeconds
	}
	if normalized.Reset5hSeconds != nil && *normalized.Reset5hSeconds > maxResetSecs {
		maxResetSecs = *normalized.Reset5hSeconds
	}
	if maxResetSecs > 0 {
		resetAt := now.Add(time.Duration(maxResetSecs) * time.Second)
		slog.Info("openai_429_using_max_reset", "max_reset_seconds", maxResetSecs, "reset_at", resetAt)
		return &resetAt
	}

	return nil
}

func (s *RateLimitService) calculateOpenAI429ResetTime(headers http.Header) *time.Time {
	return calculateOpenAI429ResetTime(headers)
}
