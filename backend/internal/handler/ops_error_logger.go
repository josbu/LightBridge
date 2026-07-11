package handler

import (
	"context"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/service"
)

const (
	opsModelKey                  = "ops_model"
	opsStreamKey                 = "ops_stream"
	opsAccountIDKey              = "ops_account_id"
	opsRoutingCapacityLimitedKey = "ops_routing_capacity_limited"

	opsUpstreamModelKey = "ops_upstream_model"
	opsRequestTypeKey   = "ops_request_type"

	// 错误过滤匹配常量 — shouldSkipOpsErrorLog 和错误分类共用
	opsErrContextCanceled            = "context canceled"
	opsErrNoAvailableAccounts        = "no available accounts"
	opsErrInvalidAPIKey              = "invalid_api_key"
	opsErrAPIKeyRequired             = "api_key_required"
	opsErrInsufficientBalance        = "insufficient balance"
	opsErrInsufficientAccountBalance = "insufficient account balance"
	opsErrInsufficientQuota          = "insufficient_quota"

	// 上游错误码常量 — 错误分类 (normalizeOpsErrorType / classifyOpsPhase / classifyOpsIsBusinessLimited)
	opsCodeInsufficientBalance   = "INSUFFICIENT_BALANCE"
	opsCodeUsageLimitExceeded    = "USAGE_LIMIT_EXCEEDED"
	opsCodeSubscriptionNotFound  = "SUBSCRIPTION_NOT_FOUND"
	opsCodeSubscriptionInvalid   = "SUBSCRIPTION_INVALID"
	opsCodeUserInactive          = "USER_INACTIVE"
	opsCodeInvalidAPIKey         = "INVALID_API_KEY"
	opsCodeAPIKeyRequired        = "API_KEY_REQUIRED"
	opsCodeAPIKeyExpired         = "API_KEY_EXPIRED"
	opsCodeAPIKeyDisabled        = "API_KEY_DISABLED"
	opsCodeUserNotFound          = "USER_NOT_FOUND"
	opsCodeAPIKeyQuotaExhausted  = "API_KEY_QUOTA_EXHAUSTED"
	opsCodeAPIKeyQueryDeprecated = "api_key_in_query_deprecated"
	opsCodeGroupDeleted          = "GROUP_DELETED"
	opsCodeGroupDisabled         = "GROUP_DISABLED"
)

const (
	opsErrorLogTimeout      = 5 * time.Second
	opsErrorLogDrainTimeout = 10 * time.Second
	opsErrorLogBatchWindow  = 200 * time.Millisecond

	opsErrorLogMinWorkerCount = 4
	opsErrorLogMaxWorkerCount = 32

	opsErrorLogQueueSizePerWorker = 128
	opsErrorLogMinQueueSize       = 256
	opsErrorLogMaxQueueSize       = 8192
	opsErrorLogBatchSize          = 32
)

type opsErrorLogJob struct {
	ops   *service.OpsService
	entry *service.OpsInsertErrorLogInput
}

var (
	opsErrorLogOnce  sync.Once
	opsErrorLogQueue chan opsErrorLogJob

	opsErrorLogStopOnce  sync.Once
	opsErrorLogWorkersWg sync.WaitGroup
	opsErrorLogMu        sync.RWMutex
	opsErrorLogStopping  bool
	opsErrorLogQueueLen  atomic.Int64
	opsErrorLogEnqueued  atomic.Int64
	opsErrorLogDropped   atomic.Int64
	opsErrorLogProcessed atomic.Int64
	opsErrorLogSanitized atomic.Int64

	opsErrorLogLastDropLogAt atomic.Int64

	opsErrorLogShutdownCh   = make(chan struct{})
	opsErrorLogShutdownOnce sync.Once
	opsErrorLogDrained      atomic.Bool
)

func startOpsErrorLogWorkers() {
	opsErrorLogMu.Lock()
	defer opsErrorLogMu.Unlock()

	if opsErrorLogStopping {
		return
	}

	workerCount, queueSize := opsErrorLogConfig()
	opsErrorLogQueue = make(chan opsErrorLogJob, queueSize)
	opsErrorLogQueueLen.Store(0)

	opsErrorLogWorkersWg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer opsErrorLogWorkersWg.Done()
			for {
				job, ok := <-opsErrorLogQueue
				if !ok {
					return
				}
				opsErrorLogQueueLen.Add(-1)
				batch := make([]opsErrorLogJob, 0, opsErrorLogBatchSize)
				batch = append(batch, job)

				timer := time.NewTimer(opsErrorLogBatchWindow)
			batchLoop:
				for len(batch) < opsErrorLogBatchSize {
					select {
					case nextJob, ok := <-opsErrorLogQueue:
						if !ok {
							if !timer.Stop() {
								select {
								case <-timer.C:
								default:
								}
							}
							flushOpsErrorLogBatch(batch)
							return
						}
						opsErrorLogQueueLen.Add(-1)
						batch = append(batch, nextJob)
					case <-timer.C:
						break batchLoop
					}
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				flushOpsErrorLogBatch(batch)
			}
		}()
	}
}

func flushOpsErrorLogBatch(batch []opsErrorLogJob) {
	if len(batch) == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[OpsErrorLogger] worker panic: %v\n%s", r, debug.Stack())
		}
	}()

	grouped := make(map[*service.OpsService][]*service.OpsInsertErrorLogInput, len(batch))
	var processed int64
	for _, job := range batch {
		if job.ops == nil || job.entry == nil {
			continue
		}
		grouped[job.ops] = append(grouped[job.ops], job.entry)
		processed++
	}
	if processed == 0 {
		return
	}

	for opsSvc, entries := range grouped {
		if opsSvc == nil || len(entries) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), opsErrorLogTimeout)
		_ = opsSvc.RecordErrorBatch(ctx, entries)
		cancel()
	}
	opsErrorLogProcessed.Add(processed)
}

func enqueueOpsErrorLog(ops *service.OpsService, entry *service.OpsInsertErrorLogInput) {
	if ops == nil || entry == nil {
		return
	}
	select {
	case <-opsErrorLogShutdownCh:
		return
	default:
	}

	opsErrorLogMu.RLock()
	stopping := opsErrorLogStopping
	opsErrorLogMu.RUnlock()
	if stopping {
		return
	}

	opsErrorLogOnce.Do(startOpsErrorLogWorkers)

	opsErrorLogMu.RLock()
	defer opsErrorLogMu.RUnlock()
	if opsErrorLogStopping || opsErrorLogQueue == nil {
		return
	}

	select {
	case opsErrorLogQueue <- opsErrorLogJob{ops: ops, entry: entry}:
		opsErrorLogQueueLen.Add(1)
		opsErrorLogEnqueued.Add(1)
	default:
		// Queue is full; drop to avoid blocking request handling.
		opsErrorLogDropped.Add(1)
		maybeLogOpsErrorLogDrop()
	}
}

func StopOpsErrorLogWorkers() bool {
	opsErrorLogStopOnce.Do(func() {
		opsErrorLogShutdownOnce.Do(func() {
			close(opsErrorLogShutdownCh)
		})
		opsErrorLogDrained.Store(stopOpsErrorLogWorkers())
	})
	return opsErrorLogDrained.Load()
}

func stopOpsErrorLogWorkers() bool {
	opsErrorLogMu.Lock()
	opsErrorLogStopping = true
	ch := opsErrorLogQueue
	if ch != nil {
		close(ch)
	}
	opsErrorLogQueue = nil
	opsErrorLogMu.Unlock()

	if ch == nil {
		opsErrorLogQueueLen.Store(0)
		return true
	}

	done := make(chan struct{})
	go func() {
		opsErrorLogWorkersWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		opsErrorLogQueueLen.Store(0)
		return true
	case <-time.After(opsErrorLogDrainTimeout):
		return false
	}
}

func OpsErrorLogQueueLength() int64 {
	return opsErrorLogQueueLen.Load()
}

func OpsErrorLogQueueCapacity() int {
	opsErrorLogMu.RLock()
	ch := opsErrorLogQueue
	opsErrorLogMu.RUnlock()
	if ch == nil {
		return 0
	}
	return cap(ch)
}

func OpsErrorLogDroppedTotal() int64 {
	return opsErrorLogDropped.Load()
}

func OpsErrorLogEnqueuedTotal() int64 {
	return opsErrorLogEnqueued.Load()
}

func OpsErrorLogProcessedTotal() int64 {
	return opsErrorLogProcessed.Load()
}

func OpsErrorLogSanitizedTotal() int64 {
	return opsErrorLogSanitized.Load()
}

func maybeLogOpsErrorLogDrop() {
	now := time.Now().Unix()

	for {
		last := opsErrorLogLastDropLogAt.Load()
		if last != 0 && now-last < 60 {
			return
		}
		if opsErrorLogLastDropLogAt.CompareAndSwap(last, now) {
			break
		}
	}

	queued := opsErrorLogQueueLen.Load()
	queueCap := OpsErrorLogQueueCapacity()

	log.Printf(
		"[OpsErrorLogger] queue is full; dropping logs (queued=%d cap=%d enqueued_total=%d dropped_total=%d processed_total=%d sanitized_total=%d)",
		queued,
		queueCap,
		opsErrorLogEnqueued.Load(),
		opsErrorLogDropped.Load(),
		opsErrorLogProcessed.Load(),
		opsErrorLogSanitized.Load(),
	)
}

func opsErrorLogConfig() (workerCount int, queueSize int) {
	workerCount = runtime.GOMAXPROCS(0) * 2
	if workerCount < opsErrorLogMinWorkerCount {
		workerCount = opsErrorLogMinWorkerCount
	}
	if workerCount > opsErrorLogMaxWorkerCount {
		workerCount = opsErrorLogMaxWorkerCount
	}

	queueSize = workerCount * opsErrorLogQueueSizePerWorker
	if queueSize < opsErrorLogMinQueueSize {
		queueSize = opsErrorLogMinQueueSize
	}
	if queueSize > opsErrorLogMaxQueueSize {
		queueSize = opsErrorLogMaxQueueSize
	}

	return workerCount, queueSize
}
