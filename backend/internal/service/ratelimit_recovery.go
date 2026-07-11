package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// ClearRateLimit 清除账号的限流状态
func (s *RateLimitService) ClearRateLimit(ctx context.Context, accountID int64) error {
	if err := s.accountRepo.ClearRateLimit(ctx, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.ClearAntigravityQuotaScopes(ctx, accountID); err != nil {
		return err
	}
	if err := s.accountRepo.ClearModelRateLimits(ctx, accountID); err != nil {
		return err
	}
	// 清除限流时一并清理临时不可调度状态，避免周限/窗口重置后仍被本地临时状态阻断。
	if err := s.accountRepo.ClearTempUnschedulable(ctx, accountID); err != nil {
		return err
	}
	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.DeleteTempUnsched(ctx, accountID); err != nil {
			slog.Warn("temp_unsched_cache_delete_failed", "account_id", accountID, "error", err)
		}
	}
	s.ResetOpenAI403Counter(ctx, accountID)
	s.notifyAccountSchedulingBlockCleared(accountID)
	return nil
}

func (s *RateLimitService) ResetOpenAI403Counter(ctx context.Context, accountID int64) {
	if s == nil || s.openAI403CounterCache == nil || accountID <= 0 {
		return
	}
	if err := s.openAI403CounterCache.ResetOpenAI403Count(ctx, accountID); err != nil {
		slog.Warn("openai_403_reset_failed", "account_id", accountID, "error", err)
	}
}

// RecoverAccountState 按需恢复账号的可恢复运行时状态。
func (s *RateLimitService) RecoverAccountState(ctx context.Context, accountID int64, options AccountRecoveryOptions) (*SuccessfulTestRecoveryResult, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := &SuccessfulTestRecoveryResult{}
	if account.Status == StatusError {
		if err := s.accountRepo.ClearError(ctx, accountID); err != nil {
			return nil, err
		}
		result.ClearedError = true
		if options.InvalidateToken && s.tokenCacheInvalidator != nil && account.IsOAuth() {
			if invalidateErr := s.tokenCacheInvalidator.InvalidateToken(ctx, account); invalidateErr != nil {
				slog.Warn("recover_account_state_invalidate_token_failed", "account_id", accountID, "error", invalidateErr)
			}
		}
	}

	if hasRecoverableRuntimeState(account) {
		if err := s.ClearRateLimit(ctx, accountID); err != nil {
			return nil, err
		}
		result.ClearedRateLimit = true
	}
	if result.ClearedError || result.ClearedRateLimit {
		s.ResetOpenAI403Counter(ctx, accountID)
		if result.ClearedError && !result.ClearedRateLimit {
			s.notifyAccountSchedulingBlockCleared(accountID)
		}
	}

	return result, nil
}

// RecoverAccountAfterSuccessfulTest 将一次成功测试视为正常请求，
// 按需恢复 error / rate-limit / overload / temp-unsched / model-rate-limit 等运行时状态。
func (s *RateLimitService) RecoverAccountAfterSuccessfulTest(ctx context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error) {
	return s.RecoverAccountState(ctx, accountID, AccountRecoveryOptions{})
}

func (s *RateLimitService) ClearTempUnschedulable(ctx context.Context, accountID int64) error {
	if err := s.accountRepo.ClearTempUnschedulable(ctx, accountID); err != nil {
		return err
	}
	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.DeleteTempUnsched(ctx, accountID); err != nil {
			slog.Warn("temp_unsched_cache_delete_failed", "account_id", accountID, "error", err)
		}
	}
	// 同时清除模型级别限流
	if err := s.accountRepo.ClearModelRateLimits(ctx, accountID); err != nil {
		slog.Warn("clear_model_rate_limits_on_temp_unsched_reset_failed", "account_id", accountID, "error", err)
	}
	s.notifyAccountSchedulingBlockCleared(accountID)
	return nil
}

func hasRecoverableRuntimeState(account *Account) bool {
	if account == nil {
		return false
	}
	if account.RateLimitedAt != nil || account.RateLimitResetAt != nil || account.OverloadUntil != nil || account.TempUnschedulableUntil != nil {
		return true
	}
	if len(account.Extra) == 0 {
		return false
	}
	return hasNonEmptyMapValue(account.Extra, "model_rate_limits") ||
		hasNonEmptyMapValue(account.Extra, "antigravity_quota_scopes")
}

func hasNonEmptyMapValue(extra map[string]any, key string) bool {
	raw, ok := extra[key]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case map[string]any:
		return len(typed) > 0
	case map[string]string:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return true
	}
}

func (s *RateLimitService) GetTempUnschedStatus(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	now := time.Now().Unix()
	if s.tempUnschedCache != nil {
		state, err := s.tempUnschedCache.GetTempUnsched(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if state != nil && state.UntilUnix > now {
			return state, nil
		}
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.TempUnschedulableUntil == nil {
		return nil, nil
	}
	if account.TempUnschedulableUntil.Unix() <= now {
		return nil, nil
	}

	state := &TempUnschedState{
		UntilUnix: account.TempUnschedulableUntil.Unix(),
	}

	if account.TempUnschedulableReason != "" {
		var parsed TempUnschedState
		if err := json.Unmarshal([]byte(account.TempUnschedulableReason), &parsed); err == nil {
			if parsed.UntilUnix == 0 {
				parsed.UntilUnix = state.UntilUnix
			}
			state = &parsed
		} else {
			state.ErrorMessage = account.TempUnschedulableReason
		}
	}

	if s.tempUnschedCache != nil {
		if err := s.tempUnschedCache.SetTempUnsched(ctx, accountID, state); err != nil {
			slog.Warn("temp_unsched_cache_set_failed", "account_id", accountID, "error", err)
		}
	}

	return state, nil
}
