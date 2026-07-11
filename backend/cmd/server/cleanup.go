package main

import (
	"context"
	"log"
	"time"

	"github.com/WilliamWang1721/LightBridge/ent"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/WilliamWang1721/LightBridge/internal/service/aistudio_proxy"
	"github.com/redis/go-redis/v9"
)

const (
	backgroundCleanupTimeout   = 10 * time.Second
	billingDrainTimeout        = 30 * time.Second
	stateFlushTimeout          = 10 * time.Second
	longRunningCleanupTimeout  = 10 * time.Second
	infrastructureCloseTimeout = 5 * time.Second
)

type cleanupStep struct {
	name string
	fn   func() error
}

type cleanupResult struct {
	name string
	err  error
}

// runParallelCleanupPhase executes a cleanup phase with a real deadline. A
// timed-out step is not waited on indefinitely; callers can continue draining
// more critical phases. The returned value indicates whether every step
// completed before the deadline.
func runParallelCleanupPhase(phase string, timeout time.Duration, steps []cleanupStep) bool {
	if len(steps) == 0 {
		return true
	}
	if timeout <= 0 {
		timeout = time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results := make(chan cleanupResult, len(steps))
	for i := range steps {
		step := steps[i]
		go func() {
			var err error
			if step.fn != nil {
				err = step.fn()
			}
			results <- cleanupResult{name: step.name, err: err}
		}()
	}

	completed := 0
	for completed < len(steps) {
		select {
		case result := <-results:
			completed++
			if result.err != nil {
				log.Printf("[Cleanup:%s] %s failed: %v", phase, result.name, result.err)
			} else {
				log.Printf("[Cleanup:%s] %s succeeded", phase, result.name)
			}
		case <-ctx.Done():
			log.Printf("[Cleanup:%s] timed out after %s (%d/%d completed)", phase, timeout, completed, len(steps))
			return false
		}
	}
	return true
}

func runSequentialCleanupPhase(phase string, timeout time.Duration, steps []cleanupStep) bool {
	for i := range steps {
		step := steps[i]
		if !runParallelCleanupPhase(phase+":"+step.name, timeout, []cleanupStep{step}) {
			return false
		}
	}
	return true
}

func newApplicationCleanup(
	entClient *ent.Client,
	rdb *redis.Client,
	featureRuntime *service.FeatureRuntimeManager,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	accountExpiry *service.AccountExpiryService,
	subscriptionExpiry *service.SubscriptionExpiryService,
	idempotencyCleanup *service.IdempotencyCleanupService,
	pricing *service.PricingService,
	emailQueue *service.EmailQueueService,
	billingCache *service.BillingCacheService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	subscriptionService *service.SubscriptionService,
	openaiOAuth *service.OpenAIOAuthService,
	grokOAuth *service.GrokOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	antigravityOAuth *service.AntigravityOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	quotaFlusher *service.UserPlatformQuotaUsageFlusher,
	aistudioProxyManager *aistudio_proxy.Manager,
) func() {
	return func() {
		// Phase 1 stops producers and periodic workers. Pricing, quota, billing
		// cache and email remain alive because queued usage tasks may still need
		// them while the billing pool drains in phase 2.
		backgroundCompleted := runParallelCleanupPhase("background", backgroundCleanupTimeout, []cleanupStep{
			{"FeatureRuntimeManager", func() error {
				if featureRuntime == nil {
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), backgroundCleanupTimeout)
				defer cancel()
				return featureRuntime.Shutdown(ctx)
			}},
			{"SchedulerSnapshotService", func() error {
				if schedulerSnapshot != nil {
					schedulerSnapshot.Stop()
				}
				return nil
			}},
			{"IdempotencyCleanupService", func() error {
				if idempotencyCleanup != nil {
					idempotencyCleanup.Stop()
				}
				return nil
			}},
			{"TokenRefreshService", func() error {
				if tokenRefresh != nil {
					tokenRefresh.Stop()
				}
				return nil
			}},
			{"AccountExpiryService", func() error {
				if accountExpiry != nil {
					accountExpiry.Stop()
				}
				return nil
			}},
			{"SubscriptionExpiryService", func() error {
				if subscriptionExpiry != nil {
					subscriptionExpiry.Stop()
				}
				return nil
			}},
			{"OpenAIOAuthService", func() error {
				if openaiOAuth != nil {
					openaiOAuth.Stop()
				}
				return nil
			}},
			{"GrokOAuthService", func() error {
				if grokOAuth != nil {
					grokOAuth.Stop()
				}
				return nil
			}},
			{"GeminiOAuthService", func() error {
				if geminiOAuth != nil {
					geminiOAuth.Stop()
				}
				return nil
			}},
			{"AntigravityOAuthService", func() error {
				if antigravityOAuth != nil {
					antigravityOAuth.Stop()
				}
				return nil
			}},
			{"OpenAIWSPool", func() error {
				if openAIGateway != nil {
					openAIGateway.CloseOpenAIWSPool()
				}
				return nil
			}},
			{"AistudioProxyManager", func() error {
				if aistudioProxyManager != nil {
					aistudioProxyManager.StopAll()
				}
				return nil
			}},
		})

		billingCompleted := runParallelCleanupPhase("billing-drain", billingDrainTimeout, []cleanupStep{
			{"UsageRecordWorkerPool", func() error {
				if usageRecordWorkerPool != nil {
					usageRecordWorkerPool.Stop()
				}
				return nil
			}},
		})

		stateCompleted := runParallelCleanupPhase("state-flush", stateFlushTimeout, []cleanupStep{
			{"UserPlatformQuotaUsageFlusher", func() error {
				if quotaFlusher != nil {
					quotaFlusher.Stop()
				}
				return nil
			}},
			{"BillingCacheService", func() error {
				if billingCache != nil {
					billingCache.Stop()
				}
				return nil
			}},
			{"PricingService", func() error {
				if pricing != nil {
					pricing.Stop()
				}
				return nil
			}},
			{"SubscriptionService", func() error {
				if subscriptionService != nil {
					subscriptionService.Stop()
				}
				return nil
			}},
			{"EmailQueueService", func() error {
				if emailQueue != nil {
					emailQueue.Stop()
				}
				return nil
			}},
		})

		// If any application step is still running, do not close Redis/Ent under
		// that goroutine. Returning lets the process terminate and the OS close
		// descriptors without creating a use-after-close race during shutdown.
		if !backgroundCompleted || !billingCompleted || !stateCompleted {
			log.Printf("[Cleanup] application cleanup incomplete; skipping explicit infrastructure close")
			return
		}

		infraCompleted := runSequentialCleanupPhase("infrastructure", infrastructureCloseTimeout, []cleanupStep{
			{"Redis", func() error {
				if rdb == nil {
					return nil
				}
				return rdb.Close()
			}},
			{"Ent", func() error {
				if entClient == nil {
					return nil
				}
				return entClient.Close()
			}},
		})
		if infraCompleted {
			log.Printf("[Cleanup] all cleanup phases completed")
		}
	}
}
