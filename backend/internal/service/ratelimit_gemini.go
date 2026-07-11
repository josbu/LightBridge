package service

import (
	"context"
	"log/slog"
	"time"
)

// PreCheckUsage proactively checks local quota before dispatching a request.
// Returns false when the account should be skipped.
func (s *RateLimitService) PreCheckUsage(ctx context.Context, account *Account, requestedModel string) (bool, error) {
	if account == nil || !account.IsPureGemini() {
		return true, nil
	}
	if s.usageRepo == nil || s.geminiQuotaService == nil {
		return true, nil
	}

	quota, ok := s.geminiQuotaService.QuotaForAccount(ctx, account)
	if !ok {
		return true, nil
	}

	now := time.Now()
	modelClass := geminiModelClassFromName(requestedModel)

	// 1) Daily quota precheck (RPD; resets at PST midnight)
	{
		var limit int64
		if quota.SharedRPD > 0 {
			limit = quota.SharedRPD
		} else {
			switch modelClass {
			case geminiModelFlash:
				limit = quota.FlashRPD
			default:
				limit = quota.ProRPD
			}
		}

		if limit > 0 {
			start := geminiDailyWindowStart(now)
			totals, ok := s.getGeminiUsageTotals(account.ID, start, now)
			if !ok {
				stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, now, 0, 0, account.ID, 0, nil, nil, nil)
				if err != nil {
					return true, err
				}
				totals = geminiAggregateUsage(stats)
				s.setGeminiUsageTotals(account.ID, start, now, totals)
			}

			var used int64
			if quota.SharedRPD > 0 {
				used = totals.ProRequests + totals.FlashRequests
			} else {
				switch modelClass {
				case geminiModelFlash:
					used = totals.FlashRequests
				default:
					used = totals.ProRequests
				}
			}

			if used >= limit {
				resetAt := geminiDailyResetTime(now)
				// NOTE:
				// - This is a local precheck to reduce upstream 429s.
				// - Do NOT mark the account as rate-limited here; rate_limit_reset_at should reflect real upstream 429s.
				slog.Info("gemini_precheck_daily_quota_reached", "account_id", account.ID, "used", used, "limit", limit, "reset_at", resetAt)
				return false, nil
			}
		}
	}

	// 2) Minute quota precheck (RPM; fixed window current minute)
	{
		var limit int64
		if quota.SharedRPM > 0 {
			limit = quota.SharedRPM
		} else {
			switch modelClass {
			case geminiModelFlash:
				limit = quota.FlashRPM
			default:
				limit = quota.ProRPM
			}
		}

		if limit > 0 {
			start := now.Truncate(time.Minute)
			stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, now, 0, 0, account.ID, 0, nil, nil, nil)
			if err != nil {
				return true, err
			}
			totals := geminiAggregateUsage(stats)

			var used int64
			if quota.SharedRPM > 0 {
				used = totals.ProRequests + totals.FlashRequests
			} else {
				switch modelClass {
				case geminiModelFlash:
					used = totals.FlashRequests
				default:
					used = totals.ProRequests
				}
			}

			if used >= limit {
				resetAt := start.Add(time.Minute)
				// Do not persist "rate limited" status from local precheck. See note above.
				slog.Info("gemini_precheck_minute_quota_reached", "account_id", account.ID, "used", used, "limit", limit, "reset_at", resetAt)
				return false, nil
			}
		}
	}

	return true, nil
}

// PreCheckUsageBatch performs quota precheck for multiple accounts in one request.
// Returned map value=false means the account should be skipped.
func (s *RateLimitService) PreCheckUsageBatch(ctx context.Context, accounts []*Account, requestedModel string) (map[int64]bool, error) {
	result := make(map[int64]bool, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		result[account.ID] = true
	}

	if len(accounts) == 0 || requestedModel == "" {
		return result, nil
	}
	if s.usageRepo == nil || s.geminiQuotaService == nil {
		return result, nil
	}

	modelClass := geminiModelClassFromName(requestedModel)
	now := time.Now()
	dailyStart := geminiDailyWindowStart(now)
	minuteStart := now.Truncate(time.Minute)

	type quotaAccount struct {
		account *Account
		quota   GeminiQuota
	}
	quotaAccounts := make([]quotaAccount, 0, len(accounts))
	for _, account := range accounts {
		if account == nil || !account.IsPureGemini() {
			continue
		}
		quota, ok := s.geminiQuotaService.QuotaForAccount(ctx, account)
		if !ok {
			continue
		}
		quotaAccounts = append(quotaAccounts, quotaAccount{
			account: account,
			quota:   quota,
		})
	}
	if len(quotaAccounts) == 0 {
		return result, nil
	}

	// 1) Daily precheck (cached + batch DB fallback)
	dailyTotalsByID := make(map[int64]GeminiUsageTotals, len(quotaAccounts))
	dailyMissIDs := make([]int64, 0, len(quotaAccounts))
	for _, item := range quotaAccounts {
		limit := geminiDailyLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}
		accountID := item.account.ID
		if totals, ok := s.getGeminiUsageTotals(accountID, dailyStart, now); ok {
			dailyTotalsByID[accountID] = totals
			continue
		}
		dailyMissIDs = append(dailyMissIDs, accountID)
	}
	if len(dailyMissIDs) > 0 {
		totalsBatch, err := s.getGeminiUsageTotalsBatch(ctx, dailyMissIDs, dailyStart, now)
		if err != nil {
			return result, err
		}
		for _, accountID := range dailyMissIDs {
			totals := totalsBatch[accountID]
			dailyTotalsByID[accountID] = totals
			s.setGeminiUsageTotals(accountID, dailyStart, now, totals)
		}
	}
	for _, item := range quotaAccounts {
		limit := geminiDailyLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}
		accountID := item.account.ID
		used := geminiUsedRequests(item.quota, modelClass, dailyTotalsByID[accountID], true)
		if used >= limit {
			resetAt := geminiDailyResetTime(now)
			slog.Info("gemini_precheck_daily_quota_reached_batch", "account_id", accountID, "used", used, "limit", limit, "reset_at", resetAt)
			result[accountID] = false
		}
	}

	// 2) Minute precheck (batch DB)
	minuteIDs := make([]int64, 0, len(quotaAccounts))
	for _, item := range quotaAccounts {
		accountID := item.account.ID
		if !result[accountID] {
			continue
		}
		if geminiMinuteLimit(item.quota, modelClass) <= 0 {
			continue
		}
		minuteIDs = append(minuteIDs, accountID)
	}
	if len(minuteIDs) == 0 {
		return result, nil
	}

	minuteTotalsByID, err := s.getGeminiUsageTotalsBatch(ctx, minuteIDs, minuteStart, now)
	if err != nil {
		return result, err
	}
	for _, item := range quotaAccounts {
		accountID := item.account.ID
		if !result[accountID] {
			continue
		}

		limit := geminiMinuteLimit(item.quota, modelClass)
		if limit <= 0 {
			continue
		}

		used := geminiUsedRequests(item.quota, modelClass, minuteTotalsByID[accountID], false)
		if used >= limit {
			resetAt := minuteStart.Add(time.Minute)
			slog.Info("gemini_precheck_minute_quota_reached_batch", "account_id", accountID, "used", used, "limit", limit, "reset_at", resetAt)
			result[accountID] = false
		}
	}

	return result, nil
}

func (s *RateLimitService) getGeminiUsageTotalsBatch(ctx context.Context, accountIDs []int64, start, end time.Time) (map[int64]GeminiUsageTotals, error) {
	result := make(map[int64]GeminiUsageTotals, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	ids := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		ids = append(ids, accountID)
	}
	if len(ids) == 0 {
		return result, nil
	}

	if batchReader, ok := s.usageRepo.(geminiUsageTotalsBatchProvider); ok {
		stats, err := batchReader.GetGeminiUsageTotalsBatch(ctx, ids, start, end)
		if err != nil {
			return nil, err
		}
		for _, accountID := range ids {
			result[accountID] = stats[accountID]
		}
		return result, nil
	}

	for _, accountID := range ids {
		stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, start, end, 0, 0, accountID, 0, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		result[accountID] = geminiAggregateUsage(stats)
	}
	return result, nil
}

func geminiDailyLimit(quota GeminiQuota, modelClass geminiModelClass) int64 {
	if quota.SharedRPD > 0 {
		return quota.SharedRPD
	}
	switch modelClass {
	case geminiModelFlash:
		return quota.FlashRPD
	default:
		return quota.ProRPD
	}
}

func geminiMinuteLimit(quota GeminiQuota, modelClass geminiModelClass) int64 {
	if quota.SharedRPM > 0 {
		return quota.SharedRPM
	}
	switch modelClass {
	case geminiModelFlash:
		return quota.FlashRPM
	default:
		return quota.ProRPM
	}
}

func geminiUsedRequests(quota GeminiQuota, modelClass geminiModelClass, totals GeminiUsageTotals, daily bool) int64 {
	if daily {
		if quota.SharedRPD > 0 {
			return totals.ProRequests + totals.FlashRequests
		}
	} else {
		if quota.SharedRPM > 0 {
			return totals.ProRequests + totals.FlashRequests
		}
	}
	switch modelClass {
	case geminiModelFlash:
		return totals.FlashRequests
	default:
		return totals.ProRequests
	}
}

func (s *RateLimitService) getGeminiUsageTotals(accountID int64, windowStart, now time.Time) (GeminiUsageTotals, bool) {
	s.usageCacheMu.RLock()
	defer s.usageCacheMu.RUnlock()

	if s.usageCache == nil {
		return GeminiUsageTotals{}, false
	}

	entry, ok := s.usageCache[accountID]
	if !ok || entry == nil {
		return GeminiUsageTotals{}, false
	}
	if !entry.windowStart.Equal(windowStart) {
		return GeminiUsageTotals{}, false
	}
	if now.Sub(entry.cachedAt) >= geminiPrecheckCacheTTL {
		return GeminiUsageTotals{}, false
	}
	return entry.totals, true
}

func (s *RateLimitService) setGeminiUsageTotals(accountID int64, windowStart, now time.Time, totals GeminiUsageTotals) {
	s.usageCacheMu.Lock()
	defer s.usageCacheMu.Unlock()
	if s.usageCache == nil {
		s.usageCache = make(map[int64]*geminiUsageCacheEntry)
	}
	s.usageCache[accountID] = &geminiUsageCacheEntry{
		windowStart: windowStart,
		cachedAt:    now,
		totals:      totals,
	}
}

// GeminiCooldown returns the fallback cooldown duration for Gemini 429s based on tier.
func (s *RateLimitService) GeminiCooldown(ctx context.Context, account *Account) time.Duration {
	if account == nil {
		return 5 * time.Minute
	}
	if s.geminiQuotaService == nil {
		return 5 * time.Minute
	}
	return s.geminiQuotaService.CooldownForAccount(ctx, account)
}
