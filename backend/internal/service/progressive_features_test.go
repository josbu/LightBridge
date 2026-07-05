package service

import (
	"context"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/stretchr/testify/require"
)

type progressiveSettingRepoStub struct {
	values map[string]string
}

func (r *progressiveSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	if value, ok := r.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (r *progressiveSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *progressiveSettingRepoStub) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return nil
}

func (r *progressiveSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *progressiveSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *progressiveSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *progressiveSettingRepoStub) Delete(_ context.Context, key string) error {
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
