package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *RateLimitService) persistOpenAICodexSnapshot(ctx context.Context, account *Account, headers http.Header) {
	if s == nil || s.accountRepo == nil || account == nil || headers == nil {
		return
	}
	snapshot := ParseCodexRateLimitHeaders(headers)
	if snapshot == nil {
		return
	}
	updates := buildCodexUsageExtraUpdates(snapshot, time.Now())
	if len(updates) == 0 {
		return
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("openai_codex_snapshot_persist_failed", "account_id", account.ID, "error", err)
	}
}

// parseOpenAIRateLimitResetTime 解析 OpenAI 格式的 429 响应，返回重置时间的 Unix 时间戳
// OpenAI 的 usage_limit_reached 错误格式：
//
//	{
//	  "error": {
//	    "message": "The usage limit has been reached",
//	    "type": "usage_limit_reached",
//	    "resets_at": 1769404154,
//	    "resets_in_seconds": 133107
//	  }
//	}
func parseOpenAIRateLimitResetTime(body []byte) *int64 {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		return nil
	}

	// 检查是否为 usage_limit_reached 或 rate_limit_exceeded 类型
	errType, _ := errObj["type"].(string)
	if errType != "usage_limit_reached" && errType != "rate_limit_exceeded" {
		return nil
	}

	// 优先使用 resets_at（Unix 时间戳）
	if resetsAt, ok := errObj["resets_at"].(float64); ok {
		ts := int64(resetsAt)
		return &ts
	}
	if resetsAt, ok := errObj["resets_at"].(string); ok {
		if ts, err := strconv.ParseInt(resetsAt, 10, 64); err == nil {
			return &ts
		}
	}

	// 如果没有 resets_at，尝试使用 resets_in_seconds
	if resetsInSeconds, ok := errObj["resets_in_seconds"].(float64); ok {
		ts := time.Now().Unix() + int64(resetsInSeconds)
		return &ts
	}
	if resetsInSeconds, ok := errObj["resets_in_seconds"].(string); ok {
		if sec, err := strconv.ParseInt(resetsInSeconds, 10, 64); err == nil {
			ts := time.Now().Unix() + sec
			return &ts
		}
	}

	return nil
}

func parseOpenAIRateLimitPlanType(body []byte) string {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		return ""
	}

	errType, _ := errObj["type"].(string)
	if errType != "usage_limit_reached" && errType != "rate_limit_exceeded" {
		return ""
	}

	planType, _ := errObj["plan_type"].(string)
	return strings.ToLower(strings.TrimSpace(planType))
}

func persistOpenAI429PlanType(ctx context.Context, repo AccountRepository, account *Account, body []byte) {
	if repo == nil || account == nil || account.Platform != PlatformOpenAI {
		return
	}

	planType := parseOpenAIRateLimitPlanType(body)
	if planType == "" {
		return
	}

	current := strings.TrimSpace(account.GetCredential("plan_type"))
	if strings.EqualFold(current, planType) {
		return
	}

	if _, err := repo.BulkUpdate(ctx, []int64{account.ID}, AccountBulkUpdate{
		Credentials: map[string]any{"plan_type": planType},
	}); err != nil {
		slog.Warn("openai_429_plan_type_sync_failed", "account_id", account.ID, "plan_type", planType, "error", err)
		return
	}

	if account.Credentials == nil {
		account.Credentials = make(map[string]any, 1)
	}
	account.Credentials["plan_type"] = planType
	slog.Info("openai_429_plan_type_synced", "account_id", account.ID, "previous_plan_type", current, "plan_type", planType)
}
