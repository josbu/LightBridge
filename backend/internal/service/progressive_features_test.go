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

func TestProgressiveFeatureDatabaseOverridePrecedenceAndConstraints(t *testing.T) {
	cfg := &config.Config{Features: config.FeaturesConfig{
		Profile: config.FeatureProfileMinimal,
		Overrides: map[string]bool{
			string(ProgressiveFeaturePayment): false,
		},
	}}
	repo := &progressiveSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "false",
		progressiveFeatureOverrideKey(ProgressiveFeaturePayment):       "true",
		SettingKeyDeploymentMode:                                       DeploymentModePersonal,
		progressiveFeatureOverrideKey(ProgressiveFeatureRedeem):        "true",
		progressiveFeatureOverrideKey(ProgressiveFeatureOpsMonitoring): "true",
	}}
	svc := NewSettingService(repo, cfg)

	// A database decision supersedes profile, process override and legacy flags.
	require.True(t, svc.IsProgressiveFeatureEnabled(context.Background(), ProgressiveFeaturePayment))
	// Deployment and process prerequisites remain authoritative.
	require.False(t, svc.IsProgressiveFeatureEnabled(context.Background(), ProgressiveFeatureRedeem))
	require.False(t, svc.IsProgressiveFeatureEnabled(context.Background(), ProgressiveFeatureOpsMonitoring))
}

func TestProgressiveFeatureDatabaseOverrideStillHonorsDependencies(t *testing.T) {
	parent := ProgressiveFeature("test_parent")
	child := ProgressiveFeature("test_child")
	definitions := map[ProgressiveFeature]ProgressiveFeatureDefinition{
		parent: optionalFeature(parent, "parent", "parent_enabled", false, false),
		child: {
			ID: child, Label: "child", Tier: ProgressiveFeatureTierOptional,
			Activation:     ProgressiveFeatureActivationDynamic,
			MinimumProfile: config.FeatureProfileMinimal,
			Dependencies:   []ProgressiveFeature{parent},
		},
	}
	snapshot := buildProgressiveFeatureSnapshot(&config.Config{}, map[string]string{
		progressiveFeatureOverrideKey(parent): "false",
		progressiveFeatureOverrideKey(child):  "true",
	}, definitions)
	require.False(t, snapshot.states[child].Enabled)
	require.Equal(t, "dependency_disabled", snapshot.states[child].Reason)
}

func TestProgressiveFeatureRepositoryOverrideKeepsRequestPathServicesInSync(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "false",
		progressiveFeatureOverrideKey(ProgressiveFeaturePayment): "true",
	}}

	enabled, overridden := progressiveFeatureRepositoryOverride(
		context.Background(),
		repo,
		ProgressiveFeaturePayment,
	)
	require.True(t, overridden)
	require.True(t, enabled)

	_, overridden = progressiveFeatureRepositoryOverride(
		context.Background(),
		repo,
		ProgressiveFeaturePrivacyFilter,
	)
	require.False(t, overridden)
}

func TestSetProgressiveFeatureOverridePersistsResetsAndNotifies(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	var callbackCalls atomic.Int32
	svc.AddOnUpdateCallback(func() { callbackCalls.Add(1) })

	enabled := true
	require.NoError(t, svc.SetProgressiveFeatureOverride(context.Background(), ProgressiveFeaturePayment, &enabled))
	require.True(t, svc.IsProgressiveFeatureEnabled(context.Background(), ProgressiveFeaturePayment))
	require.Equal(t, int32(1), callbackCalls.Load())

	overview := svc.ProgressiveFeatureControlOverview(context.Background(), nil)
	var payment ProgressiveFeatureControlState
	for _, feature := range overview.Features {
		if feature.ID == ProgressiveFeaturePayment {
			payment = feature
			break
		}
	}
	require.NotNil(t, payment.Override)
	require.True(t, *payment.Override)
	require.True(t, payment.Controllable)

	require.NoError(t, svc.SetProgressiveFeatureOverride(context.Background(), ProgressiveFeaturePayment, nil))
	require.False(t, svc.IsProgressiveFeatureEnabled(context.Background(), ProgressiveFeaturePayment))
	require.Equal(t, int32(2), callbackCalls.Load())
	require.Error(t, svc.SetProgressiveFeatureOverride(context.Background(), ProgressiveFeatureCoreGateway, &enabled))
	require.Error(t, svc.SetProgressiveFeatureOverride(context.Background(), ProgressiveFeature("missing"), &enabled))
}
