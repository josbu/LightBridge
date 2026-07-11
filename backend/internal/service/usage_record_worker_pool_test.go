package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUsageRecordWorkerPool_SubmitEnqueued(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
	})
	t.Cleanup(pool.Stop)

	done := make(chan struct{})
	mode := pool.Submit(func(ctx context.Context) {
		close(done)
	})
	require.Equal(t, UsageRecordSubmitModeEnqueued, mode)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task not executed")
	}

	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SubmittedTasks == 1 && stats.SuccessfulTasks == 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageRecordWorkerPool_LegacyDropPolicyFallsBackSynchronously(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
	})
	t.Cleanup(pool.Stop)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})
	var fallbackExecuted atomic.Bool

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))
	mode := pool.Submit(func(ctx context.Context) {
		fallbackExecuted.Store(true)
	})
	require.Equal(t, UsageRecordSubmitModeSync, mode)
	require.True(t, fallbackExecuted.Load())

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SyncFallbackQueueFull >= 1 && stats.DroppedQueueFull == 0
	}, time.Second, 10*time.Millisecond)
}

func TestUsageRecordWorkerPool_OverflowSync(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicySync,
		OverflowSamplePercent: 0,
	})
	t.Cleanup(pool.Stop)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})
	var syncExecuted atomic.Bool

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))

	mode := pool.Submit(func(ctx context.Context) {
		syncExecuted.Store(true)
	})
	require.Equal(t, UsageRecordSubmitModeSync, mode)
	require.True(t, syncExecuted.Load())

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		return pool.Stats().SyncFallbackTasks >= 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageRecordWorkerPool_LegacySamplePolicyNeverDrops(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicySample,
		OverflowSamplePercent: 1,
	})
	t.Cleanup(pool.Stop)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})
	var synchronousExecutions atomic.Int32

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))

	for i := 0; i < 2; i++ {
		mode := pool.Submit(func(ctx context.Context) {
			synchronousExecutions.Add(1)
		})
		require.Equal(t, UsageRecordSubmitModeSync, mode)
	}
	require.EqualValues(t, 2, synchronousExecutions.Load())

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SyncFallbackQueueFull >= 2 && stats.DroppedQueueFull == 0
	}, time.Second, 10*time.Millisecond)
}

func TestUsageRecordWorkerPool_SubmitAfterStopRunsSynchronously(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
	})

	pool.Stop()
	var executed atomic.Bool
	mode := pool.Submit(func(ctx context.Context) {
		executed.Store(true)
	})
	require.Equal(t, UsageRecordSubmitModeSync, mode)
	require.True(t, executed.Load())
	stats := pool.Stats()
	require.GreaterOrEqual(t, stats.SyncFallbackPoolStopped, uint64(1))
	require.Equal(t, uint64(0), stats.DroppedPoolStopped)
}

func TestUsageRecordWorkerPool_AutoScaleUpAndDown(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           2,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   4,
		AutoScaleUpPercent:    40,
		AutoScaleDownPercent:  10,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     20 * time.Millisecond,
		AutoScaleCooldown:     20 * time.Millisecond,
	})
	t.Cleanup(pool.Stop)

	block := make(chan struct{})

	// 填满运行槽位 + 队列，触发扩容阈值。
	for i := 0; i < 8; i++ {
		require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency >= 3
	}, 2*time.Second, 20*time.Millisecond)

	close(block)

	require.Eventually(t, func() bool {
		return pool.Stats().CompletedTasks >= 8
	}, 2*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestUsageRecordWorkerPool_AutoScaleDownRequiresLowRunningUtilization(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           2,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   2,
		AutoScaleUpPercent:    80,
		AutoScaleDownPercent:  50,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     20 * time.Millisecond,
		AutoScaleCooldown:     20 * time.Millisecond,
	})
	t.Cleanup(pool.Stop)

	block := make(chan struct{})
	for i := 0; i < 2; i++ {
		require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	// 虽然 waiting=0，但 running 利用率为 100%，不应缩容。
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 2, pool.Stats().MaxConcurrency)

	close(block)
	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestUsageRecordWorkerPool_SubmitNilReceiverAndNilTask(t *testing.T) {
	var nilPool *UsageRecordWorkerPool
	require.Equal(t, UsageRecordSubmitModeDropped, nilPool.Submit(func(ctx context.Context) {}))

	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	t.Cleanup(pool.Stop)

	require.Equal(t, UsageRecordSubmitModeDropped, pool.Submit(nil))
}

func TestUsageRecordWorkerPool_AutoScaleDisabledKeepsFixedConcurrency(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           2,
		QueueSize:             4,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicyDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   4,
		AutoScaleUpPercent:    10,
		AutoScaleDownPercent:  1,
		AutoScaleUpStep:       2,
		AutoScaleDownStep:     2,
		AutoScaleInterval:     10 * time.Millisecond,
		AutoScaleCooldown:     10 * time.Millisecond,
	})
	t.Cleanup(pool.Stop)

	require.Equal(t, 2, pool.Stats().MaxConcurrency)

	block := make(chan struct{})
	for i := 0; i < 4; i++ {
		require.Equal(t, UsageRecordSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	time.Sleep(120 * time.Millisecond)
	require.Equal(t, 2, pool.Stats().MaxConcurrency)
	close(block)
}

func TestUsageRecordWorkerPool_OptionsFromConfig_AutoScaleDisabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.UsageRecord.WorkerCount = 64
	cfg.Gateway.UsageRecord.QueueSize = 128
	cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 7
	cfg.Gateway.UsageRecord.OverflowPolicy = config.UsageRecordOverflowPolicyDrop
	cfg.Gateway.UsageRecord.OverflowSamplePercent = 0
	cfg.Gateway.UsageRecord.AutoScaleEnabled = false
	cfg.Gateway.UsageRecord.AutoScaleMinWorkers = 1
	cfg.Gateway.UsageRecord.AutoScaleMaxWorkers = 512

	opts := usageRecordPoolOptionsFromConfig(cfg)
	require.False(t, opts.AutoScaleEnabled)
	require.Equal(t, 64, opts.WorkerCount)
	require.Equal(t, 64, opts.AutoScaleMinWorkers)
	require.Equal(t, 64, opts.AutoScaleMaxWorkers)
	require.Equal(t, 7*time.Second, opts.TaskTimeout)
}

func TestUsageRecordWorkerPool_StringHelpers(t *testing.T) {
	require.Equal(t, "enqueued", UsageRecordSubmitModeEnqueued.String())
	stats := UsageRecordWorkerPoolStats{RunningWorkers: 2, WaitingTasks: 3, SubmittedTasks: 5, DroppedTasks: 1}
	require.Contains(t, stats.String(), "running=2")
	require.Contains(t, stats.String(), "waiting=3")
}

func TestNewUsageRecordWorkerPool_FromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.UsageRecord.WorkerCount = 3
	cfg.Gateway.UsageRecord.QueueSize = 16
	cfg.Gateway.UsageRecord.TaskTimeoutSeconds = 2
	cfg.Gateway.UsageRecord.OverflowPolicy = config.UsageRecordOverflowPolicyDrop
	cfg.Gateway.UsageRecord.AutoScaleEnabled = false

	pool := NewUsageRecordWorkerPool(cfg)
	t.Cleanup(pool.Stop)

	stats := pool.Stats()
	require.Equal(t, 3, stats.MaxConcurrency)
}

func TestUsageRecordWorkerPool_OptionsFromConfig_NilConfig(t *testing.T) {
	opts := usageRecordPoolOptionsFromConfig(nil)
	require.Equal(t, defaultUsageRecordWorkerCount, opts.WorkerCount)
	require.Equal(t, defaultUsageRecordQueueSize, opts.QueueSize)
	require.Equal(t, time.Duration(defaultUsageRecordTaskTimeoutSeconds)*time.Second, opts.TaskTimeout)
	require.Equal(t, defaultUsageRecordOverflowPolicy, opts.OverflowPolicy)
	require.Equal(t, defaultUsageRecordOverflowSampleRatio, opts.OverflowSamplePercent)
	require.True(t, opts.AutoScaleEnabled)
	require.Equal(t, defaultUsageRecordAutoScaleMinWorkers, opts.AutoScaleMinWorkers)
	require.Equal(t, defaultUsageRecordAutoScaleMaxWorkers, opts.AutoScaleMaxWorkers)
}

func TestUsageRecordWorkerPool_NormalizeOptions_BoundsAndDefaults(t *testing.T) {
	opts := normalizeUsageRecordPoolOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           0,
		QueueSize:             0,
		TaskTimeout:           0,
		OverflowPolicy:        "invalid",
		OverflowSamplePercent: 300,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   0,
		AutoScaleMaxWorkers:   0,
		AutoScaleUpPercent:    0,
		AutoScaleDownPercent:  100,
		AutoScaleUpStep:       0,
		AutoScaleDownStep:     0,
		AutoScaleInterval:     0,
		AutoScaleCooldown:     -time.Second,
	})

	require.Equal(t, defaultUsageRecordWorkerCount, opts.WorkerCount)
	require.Equal(t, defaultUsageRecordQueueSize, opts.QueueSize)
	require.Equal(t, time.Duration(defaultUsageRecordTaskTimeoutSeconds)*time.Second, opts.TaskTimeout)
	require.Equal(t, defaultUsageRecordOverflowPolicy, opts.OverflowPolicy)
	require.Equal(t, 100, opts.OverflowSamplePercent)
	require.Equal(t, defaultUsageRecordAutoScaleMinWorkers, opts.AutoScaleMinWorkers)
	require.Equal(t, defaultUsageRecordAutoScaleMaxWorkers, opts.AutoScaleMaxWorkers)
	require.Equal(t, defaultUsageRecordAutoScaleUpPercent, opts.AutoScaleUpPercent)
	require.Equal(t, defaultUsageRecordAutoScaleDownPercent, opts.AutoScaleDownPercent)
	require.Equal(t, defaultUsageRecordAutoScaleUpStep, opts.AutoScaleUpStep)
	require.Equal(t, defaultUsageRecordAutoScaleDownStep, opts.AutoScaleDownStep)
	require.Equal(t, defaultUsageRecordAutoScaleInterval, opts.AutoScaleInterval)
	require.Equal(t, defaultUsageRecordAutoScaleCooldown, opts.AutoScaleCooldown)
}

func TestUsageRecordWorkerPool_NormalizeOptions_SampleAndAutoScaleDisabled(t *testing.T) {
	sampleOpts := normalizeUsageRecordPoolOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:           32,
		QueueSize:             128,
		TaskTimeout:           time.Second,
		OverflowPolicy:        config.UsageRecordOverflowPolicySample,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   64,
		AutoScaleMaxWorkers:   48,
		AutoScaleUpPercent:    30,
		AutoScaleDownPercent:  40,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     time.Second,
		AutoScaleCooldown:     time.Second,
	})
	require.Equal(t, config.UsageRecordOverflowPolicySync, sampleOpts.OverflowPolicy)
	require.Equal(t, 0, sampleOpts.OverflowSamplePercent)
	require.Equal(t, 64, sampleOpts.AutoScaleMinWorkers)
	require.Equal(t, 64, sampleOpts.AutoScaleMaxWorkers)
	require.Equal(t, 64, sampleOpts.WorkerCount)
	require.Equal(t, 15, sampleOpts.AutoScaleDownPercent)

	fixedOpts := normalizeUsageRecordPoolOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:      20,
		AutoScaleEnabled: false,
	})
	require.Equal(t, 20, fixedOpts.AutoScaleMinWorkers)
	require.Equal(t, 20, fixedOpts.AutoScaleMaxWorkers)
}

func TestUsageRecordWorkerPool_StatsAndStop_NilBranches(t *testing.T) {
	var nilPool *UsageRecordWorkerPool
	require.Equal(t, UsageRecordWorkerPoolStats{}, nilPool.Stats())
	require.NotPanics(t, func() { nilPool.Stop() })

	emptyPool := &UsageRecordWorkerPool{}
	require.Equal(t, UsageRecordWorkerPoolStats{}, emptyPool.Stats())
	require.NotPanics(t, func() { emptyPool.Stop() })
}

func TestUsageRecordWorkerPool_Execute_PanicAndTimeout(t *testing.T) {
	pool := &UsageRecordWorkerPool{taskTimeout: 30 * time.Millisecond}

	require.NotPanics(t, func() {
		pool.execute(func(ctx context.Context) {
			panic("boom")
		})
	})

	done := make(chan struct{})
	pool.execute(func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout context not cancelled")
	}
}

func TestUsageRecordWorkerPool_ResizeNoopBranch(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{
		WorkerCount:      1,
		QueueSize:        8,
		TaskTimeout:      time.Second,
		OverflowPolicy:   config.UsageRecordOverflowPolicyDrop,
		AutoScaleEnabled: false,
	})
	t.Cleanup(pool.Stop)

	// 目标值与当前值相同，应该直接返回。
	pool.resizePool(1, 1, 0, 0, 0, 8, "noop")
	require.Equal(t, 1, pool.Stats().MaxConcurrency)
}
