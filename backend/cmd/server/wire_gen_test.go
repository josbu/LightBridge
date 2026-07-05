package main

import (
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/handler"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // contentModerationService
		nil, // quotaFlusher
		nil, // lightBridgeConnectSync
		nil, // aistudioProxyManager
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}
