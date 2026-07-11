//go:build wireinject
// +build wireinject

package main

import (
	"net/http"

	"github.com/WilliamWang1721/LightBridge/ent"
	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/handler"
	"github.com/WilliamWang1721/LightBridge/internal/payment"
	"github.com/WilliamWang1721/LightBridge/internal/repository"
	"github.com/WilliamWang1721/LightBridge/internal/server"
	"github.com/WilliamWang1721/LightBridge/internal/server/middleware"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/WilliamWang1721/LightBridge/internal/service/aistudio_proxy"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

type Application struct {
	Server  *http.Server
	Cleanup func()
}

func initializeApplication(buildInfo handler.BuildInfo) (*Application, error) {
	wire.Build(
		// Infrastructure layer ProviderSets
		config.ProviderSet,

		// Business layer ProviderSets
		repository.ProviderSet,
		service.ProviderSet,
		payment.ProviderSet,
		middleware.ProviderSet,
		handler.ProviderSet,

		// Server layer ProviderSet
		server.ProviderSet,

		// Privacy client factory for OpenAI training opt-out
		providePrivacyClientFactory,

		// BuildInfo provider
		provideServiceBuildInfo,

		// Cleanup function provider
		provideCleanup,

		// Application struct
		wire.Struct(new(Application), "Server", "Cleanup"),
	)
	return nil, nil
}

func providePrivacyClientFactory() service.PrivacyClientFactory {
	return repository.CreatePrivacyReqClient
}

func provideServiceBuildInfo(buildInfo handler.BuildInfo) service.BuildInfo {
	return service.BuildInfo{
		Version:   buildInfo.Version,
		BuildType: buildInfo.BuildType,
	}
}

func provideCleanup(
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
	return newApplicationCleanup(
		entClient,
		rdb,
		featureRuntime,
		schedulerSnapshot,
		tokenRefresh,
		accountExpiry,
		subscriptionExpiry,
		idempotencyCleanup,
		pricing,
		emailQueue,
		billingCache,
		usageRecordWorkerPool,
		subscriptionService,
		openaiOAuth,
		grokOAuth,
		geminiOAuth,
		antigravityOAuth,
		openAIGateway,
		quotaFlusher,
		aistudioProxyManager,
	)
}
