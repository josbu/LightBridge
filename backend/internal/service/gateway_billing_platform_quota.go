package service

import (
	"context"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
)

type userPlatformQuotaPersistencePath uint8

const (
	userPlatformQuotaPersistenceMain userPlatformQuotaPersistencePath = iota
	userPlatformQuotaPersistenceLegacy
)

// persistUserPlatformQuotaUsage keeps the Redis enforcement view and its
// database mirror in one place for both the atomic and legacy billing paths.
//
// Redis is updated synchronously so the next preflight sees the new usage. When
// the snapshot flusher is disabled, the database mirror is also written in the
// current usage worker with a bounded detached context. This avoids both a
// duplicated implementation and a per-request goroutine that could outlive
// shutdown or run forever after the request context was cancelled.
func persistUserPlatformQuotaUsage(
	ctx context.Context,
	p *postUsageBillingParams,
	deps *billingDeps,
	path userPlatformQuotaPersistencePath,
) {
	if p == nil || p.Cost == nil || deps == nil || deps.billingCacheService == nil || deps.userPlatformQuotaRepo == nil {
		return
	}
	if p.IsSubscriptionBill || p.Platform == "" || p.Cost.ActualCost <= 0 || p.User == nil {
		return
	}
	if !deps.billingCacheService.HasUserPlatformQuotaLimit(ctx, p.User.ID, p.Platform) {
		return
	}

	deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, p.Cost.ActualCost)
	if deps.cfg != nil && deps.cfg.Database.UserPlatformQuotaFlusherEnabled {
		return
	}

	dbCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	if err := deps.userPlatformQuotaRepo.IncrementUsageWithReset(
		dbCtx,
		p.User.ID,
		p.Platform,
		p.Cost.ActualCost,
		time.Now().UTC(),
	); err != nil {
		if path == userPlatformQuotaPersistenceLegacy {
			userPlatformQuotaDBIncrLegacyErrorTotal.Add(1)
		} else {
			userPlatformQuotaDBIncrErrorTotal.Add(1)
		}
		logger.LegacyPrintf(
			"service.gateway",
			"ALERT: persist user platform quota DB failed path=%s user=%d platform=%s cost=%f: %v",
			path.String(),
			p.User.ID,
			p.Platform,
			p.Cost.ActualCost,
			err,
		)
	}
}

func (p userPlatformQuotaPersistencePath) String() string {
	if p == userPlatformQuotaPersistenceLegacy {
		return "legacy"
	}
	return "main"
}
