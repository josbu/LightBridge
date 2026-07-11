package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunParallelCleanupPhaseCompletesAllSteps(t *testing.T) {
	var calls atomic.Int32
	completed := runParallelCleanupPhase("test", time.Second, []cleanupStep{
		{name: "one", fn: func() error { calls.Add(1); return nil }},
		{name: "two", fn: func() error { calls.Add(1); return errors.New("expected test error") }},
	})

	require.True(t, completed)
	require.EqualValues(t, 2, calls.Load())
}

func TestRunParallelCleanupPhaseEnforcesTimeout(t *testing.T) {
	block := make(chan struct{})
	started := time.Now()
	completed := runParallelCleanupPhase("blocked", 30*time.Millisecond, []cleanupStep{
		{name: "blocked-step", fn: func() error { <-block; return nil }},
	})
	elapsed := time.Since(started)
	close(block)

	require.False(t, completed)
	require.Less(t, elapsed, 500*time.Millisecond)
}

func TestRunSequentialCleanupPhaseStopsAfterTimeout(t *testing.T) {
	block := make(chan struct{})
	var secondCalled atomic.Bool
	completed := runSequentialCleanupPhase("sequential", 30*time.Millisecond, []cleanupStep{
		{name: "blocked", fn: func() error { <-block; return nil }},
		{name: "second", fn: func() error { secondCalled.Store(true); return nil }},
	})
	close(block)

	require.False(t, completed)
	require.False(t, secondCalled.Load())
}
