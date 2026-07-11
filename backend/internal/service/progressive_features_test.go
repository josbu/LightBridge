package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/stretchr/testify/require"
)

type progressiveSettingRepoStub struct {
	mu     sync.RWMutex
	values map[string]string
}

func (r *progressiveSettingRepoStub) setValue(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
}

func (r *progressiveSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if value, ok := r.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (r *progressiveSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *progressiveSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setValue(key, value)
	return nil
}

func (r *progressiveSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *progressiveSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string]string{}
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *progressiveSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *progressiveSettingRepoStub) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

func TestSettingServiceIsProgressiveFeatureEnabledHonorsDefaults(t *testing.T) {
	svc := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{}}, &config.Config{})
	ctx := context.Background()

	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureChannelMonitor))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureRedeem))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureAvailableChannels))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment))
}

func TestSettingServiceIsProgressiveFeatureEnabledHonorsExplicitFlags(t *testing.T) {
	svc := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{
		SettingKeyChannelMonitorEnabled:    "false",
		SettingKeyAvailableChannelsEnabled: "true",
		SettingPaymentEnabled:              "true",
		SettingKeyRedeemEnabled:            "false",
	}}, &config.Config{})
	ctx := context.Background()

	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureChannelMonitor))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureAvailableChannels))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureRedeem))
}

func TestSettingServiceIsProgressiveFeatureEnabledRemovesDistributionOnlyFeaturesInPersonalMode(t *testing.T) {
	svc := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{
		SettingKeyDeploymentMode:           DeploymentModePersonal,
		SettingKeyRedeemEnabled:            "true",
		SettingKeyAnnouncementsEnabled:     "true",
		SettingKeyRiskControlEnabled:       "true",
		SettingKeyChannelMonitorEnabled:    "true",
		SettingKeyPrivacyFilterEnabled:     "true",
		SettingKeyChannelPricingEnabled:    "true",
		SettingKeyAvailableChannelsEnabled: "true",
	}}, &config.Config{})
	ctx := context.Background()

	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureRedeem))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureAnnouncements))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureRiskControl))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureSubscriptions))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureChannelMonitor))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePrivacyFilter))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureChannelPricing))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureAvailableChannels))
}

func TestProgressiveFeatureProfilesKeepCoreAndTrimHeavyFeatures(t *testing.T) {
	cfg := &config.Config{Features: config.FeaturesConfig{Profile: config.FeatureProfileMinimal}}
	svc := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{}}, cfg)
	ctx := context.Background()

	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureCoreGateway))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment) == false)
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureOpsMonitoring))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureBackup))
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureModuleRuntime))
}

func TestValidateProgressiveFeatureConfigRejectsUnknownAndCoreDisable(t *testing.T) {
	require.Error(t, ValidateProgressiveFeatureConfig(&config.Config{Features: config.FeaturesConfig{
		Profile:   config.FeatureProfileFull,
		Overrides: map[string]bool{"unknown": true},
	}}))
	require.Error(t, ValidateProgressiveFeatureConfig(&config.Config{Features: config.FeaturesConfig{
		Profile:   config.FeatureProfileFull,
		Overrides: map[string]bool{string(ProgressiveFeatureCoreBilling): false},
	}}))
}

type countingProgressiveSettingRepo struct {
	*progressiveSettingRepoStub
	multipleCalls atomic.Int32
}

func (r *countingProgressiveSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.multipleCalls.Add(1)
	return r.progressiveSettingRepoStub.GetMultiple(ctx, keys)
}

func TestProgressiveFeatureSnapshotBatchesAndInvalidatesSettings(t *testing.T) {
	repo := &countingProgressiveSettingRepo{progressiveSettingRepoStub: &progressiveSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "true",
	}}}
	svc := NewSettingService(repo, &config.Config{})
	ctx := context.Background()

	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment))
	require.True(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeatureChannelMonitor))
	require.Equal(t, int32(1), repo.multipleCalls.Load())

	repo.setValue(SettingPaymentEnabled, "false")
	svc.InvalidateProgressiveFeatureSnapshot()
	require.False(t, svc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment))
	require.Equal(t, int32(2), repo.multipleCalls.Load())
}
