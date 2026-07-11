//go:build unit

package service

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitNotificationTaskRunsSynchronouslyWithoutExecutor(t *testing.T) {
	svc := &BalanceNotifyService{}
	callerReturned := false
	svc.submitNotificationTask("sync_fallback", func() {
		if callerReturned {
			t.Fatal("task ran after submitNotificationTask returned")
		}
	})
	callerReturned = true
}

func TestSubmitNotificationTaskRunsSynchronouslyWhenSlotsAreFull(t *testing.T) {
	svc := &BalanceNotifyService{asyncSlots: make(chan struct{}, 1)}
	svc.asyncSlots <- struct{}{}

	callerReturned := false
	svc.submitNotificationTask("backpressure", func() {
		if callerReturned {
			t.Fatal("saturated executor spawned an additional goroutine")
		}
	})
	callerReturned = true
}

func TestSubmitNotificationTaskReleasesAsyncSlot(t *testing.T) {
	svc := &BalanceNotifyService{asyncSlots: make(chan struct{}, 1)}
	done := make(chan struct{})

	svc.submitNotificationTask("release_slot", func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification task did not run")
	}

	deadline := time.After(time.Second)
	for len(svc.asyncSlots) != 0 {
		select {
		case <-deadline:
			t.Fatal("notification executor did not release its slot")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestSubmitNotificationTaskRecoversPanicsAndContinues(t *testing.T) {
	svc := &BalanceNotifyService{}
	var ran atomic.Bool

	svc.submitNotificationTask("panic", func() {
		panic("expected test panic")
	})
	svc.submitNotificationTask("after_panic", func() {
		ran.Store(true)
	})

	if !ran.Load() {
		t.Fatal("notification executor did not continue after a recovered panic")
	}
}
