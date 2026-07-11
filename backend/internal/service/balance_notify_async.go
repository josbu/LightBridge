package service

import "log/slog"

const defaultBalanceNotifyAsyncLimit = 8

// submitNotificationTask caps notification goroutines without making balance
// and quota alerts lossy. When every async slot is busy, the caller executes
// the task synchronously instead of spawning another goroutine or dropping it.
func (s *BalanceNotifyService) submitNotificationTask(name string, task func()) {
	if task == nil {
		return
	}
	run := func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic in notification task", "task", name, "recover", recovered)
			}
		}()
		task()
	}

	if s == nil || s.asyncSlots == nil {
		run()
		return
	}

	select {
	case s.asyncSlots <- struct{}{}:
		go func() {
			defer func() { <-s.asyncSlots }()
			run()
		}()
	default:
		// Backpressure is safer than either an unbounded goroutine fan-out or a
		// silently lost financial notification.
		run()
	}
}
