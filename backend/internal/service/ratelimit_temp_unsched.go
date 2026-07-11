package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *RateLimitService) HandleTempUnschedulable(ctx context.Context, account *Account, statusCode int, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	return s.tryTempUnschedulable(ctx, account, statusCode, responseBody)
}

const upstreamModelNotFoundCooldown = 30 * time.Minute
const upstreamModelNotFoundReason = "upstream_404_model_not_found"
const tempUnschedBodyMaxBytes = 64 << 10
const tempUnschedMessageMaxBytes = 2048

func (s *RateLimitService) HandleUpstreamModelNotFound(ctx context.Context, account *Account, requestedModel string, statusCode int, responseBody []byte) bool {
	if s == nil || account == nil || s.accountRepo == nil {
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	if !isUpstreamModelNotFoundError(statusCode, responseBody) {
		return false
	}
	modelKey := modelRateLimitKeyForUpstreamModelNotFound(ctx, account, requestedModel)
	if modelKey == "" {
		return false
	}
	resetAt := time.Now().Add(upstreamModelNotFoundCooldown)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, modelKey, resetAt, upstreamModelNotFoundReason); err != nil {
		slog.Warn("upstream_model_not_found_set_model_rate_limit_failed", "account_id", account.ID, "model", modelKey, "error", err)
		return true
	}
	slog.Info("upstream_model_not_found_model_rate_limited", "account_id", account.ID, "model", modelKey, "reset_at", resetAt)
	return true
}

func modelRateLimitKeyForUpstreamModelNotFound(ctx context.Context, account *Account, requestedModel string) string {
	modelKey := strings.TrimSpace(requestedModel)
	if account == nil || modelKey == "" {
		return modelKey
	}
	if account.IsAntigravity() {
		if resolved := strings.TrimSpace(resolveFinalAntigravityModelKey(ctx, account, modelKey)); resolved != "" {
			return resolved
		}
		return modelKey
	}
	if mapped := strings.TrimSpace(account.GetMappedModel(modelKey)); mapped != "" {
		return mapped
	}
	return modelKey
}

func (s *RateLimitService) tryTempUnschedulable(ctx context.Context, account *Account, statusCode int, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if !account.IsTempUnschedulableEnabled() {
		return false
	}
	// 401 首次命中可临时不可调度（给 token 刷新窗口）；
	// 若历史上已因 401 进入过临时不可调度，则本次应升级为 error（返回 false 交由默认错误逻辑处理）。
	// Antigravity 跳过：其 401 由 applyErrorPolicy 的 temp_unschedulable_rules 自行控制，无需升级逻辑。
	if statusCode == http.StatusUnauthorized && !account.IsAntigravity() {
		reason := account.TempUnschedulableReason
		// 缓存可能没有 reason，从 DB 回退读取
		if reason == "" {
			if dbAcc, err := s.accountRepo.GetByID(ctx, account.ID); err == nil && dbAcc != nil {
				reason = dbAcc.TempUnschedulableReason
			}
		}
		if wasTempUnschedByStatusCode(reason, statusCode) {
			slog.Info("401_escalated_to_error", "account_id", account.ID,
				"reason", "previous temp-unschedulable was also 401")
			return false
		}
	}
	rules := account.GetTempUnschedulableRules()
	if len(rules) == 0 {
		return false
	}
	if statusCode <= 0 || len(responseBody) == 0 {
		return false
	}

	body := responseBody
	if len(body) > tempUnschedBodyMaxBytes {
		body = body[:tempUnschedBodyMaxBytes]
	}
	bodyLower := strings.ToLower(string(body))

	for idx, rule := range rules {
		if rule.ErrorCode != statusCode || len(rule.Keywords) == 0 {
			continue
		}
		matchedKeyword := matchTempUnschedKeyword(bodyLower, rule.Keywords)
		if matchedKeyword == "" {
			continue
		}

		if s.triggerTempUnschedulable(ctx, account, rule, idx, statusCode, matchedKeyword, responseBody) {
			return true
		}
	}

	return false
}

func wasTempUnschedByStatusCode(reason string, statusCode int) bool {
	if statusCode <= 0 {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}

	var state TempUnschedState
	if err := json.Unmarshal([]byte(reason), &state); err != nil {
		return false
	}
	return state.StatusCode == statusCode
}

func matchTempUnschedKeyword(bodyLower string, keywords []string) string {
	if bodyLower == "" {
		return ""
	}
	for _, keyword := range keywords {
		k := strings.TrimSpace(keyword)
		if k == "" {
			continue
		}
		if strings.Contains(bodyLower, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func (s *RateLimitService) triggerTempUnschedulable(ctx context.Context, account *Account, rule TempUnschedulableRule, ruleIndex int, statusCode int, matchedKeyword string, responseBody []byte) bool {
	if account == nil {
		return false
	}
	if rule.DurationMinutes <= 0 {
		return false
	}

	now := time.Now()
	until := now.Add(time.Duration(rule.DurationMinutes) * time.Minute)

	state := &TempUnschedState{
		UntilUnix:       until.Unix(),
		TriggeredAtUnix: now.Unix(),
		StatusCode:      statusCode,
		MatchedKeyword:  matchedKeyword,
		RuleIndex:       ruleIndex,
		ErrorMessage:    truncateTempUnschedMessage(responseBody, tempUnschedMessageMaxBytes),
	}

	reason := ""
	if raw, err := json.Marshal(state); err == nil {
		reason = string(raw)
	}
	if reason == "" {
		reason = strings.TrimSpace(state.ErrorMessage)
	}

	s.notifyAccountSchedulingBlocked(account, until, "temp_unschedulable")
	if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, reason); err != nil {
		slog.Warn("temp_unsched_set_failed", "account_id", account.ID, "error", err)
		return false
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, account.ID, state); err != nil {
			slog.Warn("temp_unsched_cache_set_failed", "account_id", account.ID, "error", err)
		}
	}

	slog.Info("account_temp_unschedulable", "account_id", account.ID, "until", until, "rule_index", ruleIndex, "status_code", statusCode)
	return true
}

func truncateTempUnschedMessage(body []byte, maxBytes int) string {
	if maxBytes <= 0 || len(body) == 0 {
		return ""
	}
	if len(body) > maxBytes {
		body = body[:maxBytes]
	}
	return strings.TrimSpace(string(body))
}
