package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/stretchr/testify/require"
)

func waitForRuntimeCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, condition())
}

func TestFeatureRuntimeManagerDynamicLifecycle(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	settings := NewSettingService(repo, &config.Config{})
	manager, err := NewFeatureRuntimeManager(settings, &config.Config{})
	require.NoError(t, err)

	var starts atomic.Int32
	var pauses atomic.Int32
	var shutdowns atomic.Int32
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "payment-test",
		Feature: ProgressiveFeaturePayment,
		Start: func(context.Context) error {
			starts.Add(1)
			return nil
		},
		Pause: func(context.Context) error {
			pauses.Add(1)
			return nil
		},
		Shutdown: func(context.Context) error {
			shutdowns.Add(1)
			return nil
		},
	}))
	require.NoError(t, manager.Start(context.Background()))
	require.Equal(t, int32(1), starts.Load())

	repo.setValue(SettingPaymentEnabled, "false")
	settings.runOnUpdateCallbacks()
	waitForRuntimeCondition(t, func() bool { return pauses.Load() == 1 })

	repo.setValue(SettingPaymentEnabled, "true")
	settings.runOnUpdateCallbacks()
	waitForRuntimeCondition(t, func() bool { return starts.Load() == 2 })

	require.NoError(t, manager.Shutdown(context.Background()))
	require.Equal(t, int32(1), shutdowns.Load())
}

func TestFeatureRuntimeManagerBootFeatureDoesNotStartAfterSettingsUpdate(t *testing.T) {
	cfg := &config.Config{Features: config.FeaturesConfig{Profile: config.FeatureProfileMinimal}}
	settings := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{}}, cfg)
	manager, err := NewFeatureRuntimeManager(settings, cfg)
	require.NoError(t, err)

	var starts atomic.Int32
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:     "backup-test",
		Feature:  ProgressiveFeatureBackup,
		Start:    func(context.Context) error { starts.Add(1); return nil },
		Pause:    func(context.Context) error { return nil },
		Shutdown: func(context.Context) error { return nil },
	}))
	require.NoError(t, manager.Start(context.Background()))
	require.Zero(t, starts.Load())

	cfg.Features.Profile = config.FeatureProfileFull
	settings.InvalidateProgressiveFeatureSnapshot()
	settings.runOnUpdateCallbacks()
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, starts.Load(), "boot components require a process restart")
	state, ok := settings.ProgressiveFeatureState(context.Background(), ProgressiveFeatureBackup)
	require.True(t, ok)
	require.False(t, state.Enabled, "route and menu availability must match the process-lifetime worker state")
	require.True(t, state.ConfiguredEnabled)
	require.True(t, state.RequiresRestart)
	require.Equal(t, "restart_required_enable", state.Reason)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestFeatureRuntimeManagerFinalizesPausedComponents(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	settings := NewSettingService(repo, &config.Config{})
	manager, err := NewFeatureRuntimeManager(settings, &config.Config{})
	require.NoError(t, err)

	var shutdowns atomic.Int32
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:     "paused-finalize-test",
		Feature:  ProgressiveFeaturePayment,
		Start:    func(context.Context) error { return nil },
		Pause:    func(context.Context) error { return nil },
		Shutdown: func(context.Context) error { shutdowns.Add(1); return nil },
	}))
	require.NoError(t, manager.Start(context.Background()))
	repo.setValue(SettingPaymentEnabled, "false")
	settings.runOnUpdateCallbacks()
	waitForRuntimeCondition(t, func() bool { return !manager.Status()[0].Running })
	require.NoError(t, manager.Shutdown(context.Background()))
	require.Equal(t, int32(1), shutdowns.Load())
}

func TestFeatureRuntimeManagerRollsBackFailedStartAndRetriesDynamicComponent(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	settings := NewSettingService(repo, &config.Config{})
	manager, err := NewFeatureRuntimeManager(settings, &config.Config{})
	require.NoError(t, err)

	var starts atomic.Int32
	var pauses atomic.Int32
	var shutdowns atomic.Int32
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "retryable-start",
		Feature: ProgressiveFeaturePayment,
		Start: func(context.Context) error {
			if starts.Add(1) == 1 {
				return assertRuntimeError("first start failed")
			}
			return nil
		},
		Pause: func(context.Context) error {
			pauses.Add(1)
			return nil
		},
		Shutdown: func(context.Context) error {
			shutdowns.Add(1)
			return nil
		},
	}))

	require.Error(t, manager.Start(context.Background()))
	require.Equal(t, int32(1), starts.Load())
	require.Equal(t, int32(1), pauses.Load())
	status := manager.Status()
	require.Len(t, status, 1)
	require.False(t, status[0].Running)
	require.False(t, status[0].Started)
	require.False(t, status[0].CleanupRequired)

	settings.runOnUpdateCallbacks()
	waitForRuntimeCondition(t, func() bool { return starts.Load() == 2 && manager.Status()[0].Running })
	require.NoError(t, manager.Shutdown(context.Background()))
	require.Equal(t, int32(1), shutdowns.Load())
}

func TestFeatureRuntimeManagerBlocksRetryWhenFailedStartCannotRollback(t *testing.T) {
	repo := &progressiveSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	settings := NewSettingService(repo, &config.Config{})
	manager, err := NewFeatureRuntimeManager(settings, &config.Config{})
	require.NoError(t, err)

	var starts atomic.Int32
	var shutdowns atomic.Int32
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "unclean-start",
		Feature: ProgressiveFeaturePayment,
		Start: func(context.Context) error {
			starts.Add(1)
			return assertRuntimeError("start failed after partial allocation")
		},
		Pause: func(context.Context) error {
			return assertRuntimeError("rollback failed")
		},
		Shutdown: func(context.Context) error {
			shutdowns.Add(1)
			return nil
		},
	}))

	require.Error(t, manager.Start(context.Background()))
	status := manager.Status()
	require.Len(t, status, 1)
	require.False(t, status[0].Running)
	require.True(t, status[0].Started)
	require.True(t, status[0].CleanupRequired)

	settings.runOnUpdateCallbacks()
	time.Sleep(75 * time.Millisecond)
	require.Equal(t, int32(1), starts.Load(), "unsafe partial startup must not be retried")
	require.NoError(t, manager.Shutdown(context.Background()))
	require.Equal(t, int32(1), shutdowns.Load())
	require.False(t, manager.Status()[0].CleanupRequired)
}

func TestFeatureRuntimeManagerRollsBackEntireFeatureGroupOnPartialStart(t *testing.T) {
	cfg := &config.Config{Features: config.FeaturesConfig{Profile: config.FeatureProfileFull}}
	cfg.Ops.Enabled = true
	settings := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{}}, cfg)
	manager, err := NewFeatureRuntimeManager(settings, cfg)
	require.NoError(t, err)

	var firstStarts atomic.Int32
	var firstPauses atomic.Int32
	var secondStarts atomic.Int32
	var thirdStarts atomic.Int32

	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "ops-first",
		Feature: ProgressiveFeatureOpsMonitoring,
		Start: func(context.Context) error {
			firstStarts.Add(1)
			return nil
		},
		Pause: func(context.Context) error {
			firstPauses.Add(1)
			return nil
		},
	}))
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "ops-second",
		Feature: ProgressiveFeatureOpsMonitoring,
		Start: func(context.Context) error {
			secondStarts.Add(1)
			return assertRuntimeError("second component failed")
		},
		Pause: func(context.Context) error { return nil },
	}))
	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:    "ops-third",
		Feature: ProgressiveFeatureOpsMonitoring,
		Start: func(context.Context) error {
			thirdStarts.Add(1)
			return nil
		},
		Pause: func(context.Context) error { return nil },
	}))

	require.Error(t, manager.Start(context.Background()))
	require.Equal(t, int32(1), firstStarts.Load())
	require.Equal(t, int32(1), firstPauses.Load(), "already-started siblings must be rolled back")
	require.Equal(t, int32(1), secondStarts.Load())
	require.Zero(t, thirdStarts.Load(), "later siblings must not start after the feature has failed")

	for _, status := range manager.Status() {
		require.False(t, status.Running, status.Name)
	}
	state, ok := settings.ProgressiveFeatureState(context.Background(), ProgressiveFeatureOpsMonitoring)
	require.True(t, ok)
	require.False(t, state.Enabled, "a partially started feature must not expose routes or menus")
	require.True(t, state.ConfiguredEnabled)
	require.True(t, state.RequiresRestart)
	require.Equal(t, "restart_required_enable", state.Reason)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestFeatureRuntimeManagerFreezesBootAvailabilityUntilRestart(t *testing.T) {
	cfg := &config.Config{Features: config.FeaturesConfig{Profile: config.FeatureProfileFull}}
	settings := NewSettingService(&progressiveSettingRepoStub{values: map[string]string{}}, cfg)
	manager, err := NewFeatureRuntimeManager(settings, cfg)
	require.NoError(t, err)

	require.NoError(t, manager.Register(FeatureRuntimeComponent{
		Name:     "backup-boot-state",
		Feature:  ProgressiveFeatureBackup,
		Start:    func(context.Context) error { return nil },
		Pause:    func(context.Context) error { return nil },
		Shutdown: func(context.Context) error { return nil },
	}))
	require.NoError(t, manager.Start(context.Background()))

	initial, ok := settings.ProgressiveFeatureState(context.Background(), ProgressiveFeatureBackup)
	require.True(t, ok)
	require.True(t, initial.Enabled)
	require.True(t, initial.ConfiguredEnabled)
	require.False(t, initial.RequiresRestart)

	cfg.Features.Profile = config.FeatureProfileMinimal
	settings.InvalidateProgressiveFeatureSnapshot()
	settings.runOnUpdateCallbacks()

	state, ok := settings.ProgressiveFeatureState(context.Background(), ProgressiveFeatureBackup)
	require.True(t, ok)
	require.True(t, state.Enabled, "boot contribution remains active for this process")
	require.False(t, state.ConfiguredEnabled)
	require.True(t, state.RequiresRestart)
	require.Equal(t, "restart_required_disable", state.Reason)
	require.NoError(t, manager.Shutdown(context.Background()))
}

type assertRuntimeError string

func (e assertRuntimeError) Error() string { return string(e) }
