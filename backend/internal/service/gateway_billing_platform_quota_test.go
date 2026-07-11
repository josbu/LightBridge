//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
)

type platformQuotaCacheStub struct {
	BillingCache
	entry     *UserPlatformQuotaCacheEntry
	getOK     bool
	incrCalls int
	markDirty bool
}

func (s *platformQuotaCacheStub) GetUserPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return s.entry, s.getOK, nil
}

func (s *platformQuotaCacheStub) IncrUserPlatformQuotaUsageCache(_ context.Context, _ int64, _ string, _ float64, _ time.Duration, markDirty bool) error {
	s.incrCalls++
	s.markDirty = markDirty
	return nil
}

type platformQuotaRepoStub struct {
	UserPlatformQuotaRepository
	calls       int
	sawDeadline bool
	err         error
}

func (s *platformQuotaRepoStub) IncrementUsageWithReset(ctx context.Context, _ int64, _ string, _ float64, _ time.Time) error {
	s.calls++
	_, s.sawDeadline = ctx.Deadline()
	return s.err
}

func float64Pointer(value float64) *float64 {
	return &value
}

func newPlatformQuotaPersistenceFixture(flusherEnabled bool) (*postUsageBillingParams, *billingDeps, *platformQuotaCacheStub, *platformQuotaRepoStub) {
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	cfg.Database.UserPlatformQuotaFlusherEnabled = flusherEnabled

	cache := &platformQuotaCacheStub{
		entry: &UserPlatformQuotaCacheEntry{DailyLimitUSD: float64Pointer(10)},
		getOK: true,
	}
	repo := &platformQuotaRepoStub{}
	billingCache := &BillingCacheService{cfg: cfg, cache: cache}
	params := &postUsageBillingParams{
		Cost:     &CostBreakdown{ActualCost: 0.25},
		User:     &User{ID: 42},
		Platform: "anthropic",
	}
	deps := &billingDeps{
		billingCacheService:   billingCache,
		userPlatformQuotaRepo: repo,
		cfg:                   cfg,
	}
	return params, deps, cache, repo
}

func TestPersistUserPlatformQuotaUsageWritesRedisAndDatabaseWithoutFlusher(t *testing.T) {
	params, deps, cache, repo := newPlatformQuotaPersistenceFixture(false)

	persistUserPlatformQuotaUsage(context.Background(), params, deps, userPlatformQuotaPersistenceMain)

	if cache.incrCalls != 1 {
		t.Fatalf("expected one Redis increment, got %d", cache.incrCalls)
	}
	if cache.markDirty {
		t.Fatal("direct database mode must not mark the flusher dirty set")
	}
	if repo.calls != 1 {
		t.Fatalf("expected one database mirror write, got %d", repo.calls)
	}
	if !repo.sawDeadline {
		t.Fatal("database mirror write must use a bounded context")
	}
}

func TestPersistUserPlatformQuotaUsageDefersDatabaseWriteToFlusher(t *testing.T) {
	params, deps, cache, repo := newPlatformQuotaPersistenceFixture(true)

	persistUserPlatformQuotaUsage(context.Background(), params, deps, userPlatformQuotaPersistenceMain)

	if cache.incrCalls != 1 || !cache.markDirty {
		t.Fatalf("expected a dirty Redis increment, calls=%d markDirty=%v", cache.incrCalls, cache.markDirty)
	}
	if repo.calls != 0 {
		t.Fatalf("flusher mode must not write the database inline, got %d calls", repo.calls)
	}
}

func TestPersistUserPlatformQuotaUsageSkipsUsersWithoutLimits(t *testing.T) {
	params, deps, cache, repo := newPlatformQuotaPersistenceFixture(false)
	cache.entry = &UserPlatformQuotaCacheEntry{}

	persistUserPlatformQuotaUsage(context.Background(), params, deps, userPlatformQuotaPersistenceMain)

	if cache.incrCalls != 0 || repo.calls != 0 {
		t.Fatalf("unlimited users must not create quota writes, redis=%d db=%d", cache.incrCalls, repo.calls)
	}
}

func TestPersistUserPlatformQuotaUsageCountsDatabaseFailuresByPath(t *testing.T) {
	params, deps, _, repo := newPlatformQuotaPersistenceFixture(false)
	repo.err = errors.New("database unavailable")
	mainBefore := userPlatformQuotaDBIncrErrorTotal.Load()
	legacyBefore := userPlatformQuotaDBIncrLegacyErrorTotal.Load()

	persistUserPlatformQuotaUsage(context.Background(), params, deps, userPlatformQuotaPersistenceLegacy)

	if userPlatformQuotaDBIncrErrorTotal.Load() != mainBefore {
		t.Fatal("legacy failure changed the main-path counter")
	}
	if userPlatformQuotaDBIncrLegacyErrorTotal.Load() != legacyBefore+1 {
		t.Fatal("legacy failure was not counted")
	}
}
