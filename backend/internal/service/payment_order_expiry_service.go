package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const expiryCheckTimeout = 30 * time.Second

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc *PaymentService
	settingSvc *SettingService
	interval   time.Duration
	mu         sync.Mutex
	stopCh     chan struct{}
	cancel     context.CancelFunc
	running    bool
	wg         sync.WaitGroup
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, settingSvc *SettingService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		settingSvc: settingSvc,
		interval:   interval,
	}
}

func (s *PaymentOrderExpiryService) Start() {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	runCtx, cancel := context.WithCancel(context.Background())
	s.stopCh = stopCh
	s.cancel = cancel
	s.running = true
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce(runCtx)
		for {
			select {
			case <-ticker.C:
				s.runOnce(runCtx)
			case <-stopCh:
				return
			}
		}
	}()
}

func (s *PaymentOrderExpiryService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	cancel := s.cancel
	s.running = false
	s.stopCh = nil
	s.cancel = nil
	s.mu.Unlock()

	close(stopCh)
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *PaymentOrderExpiryService) SyncFeatureState(ctx context.Context) {
	if s == nil {
		return
	}
	enabled := true
	if s.settingSvc != nil {
		enabled = s.settingSvc.IsProgressiveFeatureEnabled(ctx, ProgressiveFeaturePayment)
	}
	if enabled {
		s.Start()
		return
	}
	s.Stop()
}

func (s *PaymentOrderExpiryService) runOnce(parentCtx context.Context) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	reconcileCtx, cancel := context.WithTimeout(parentCtx, expiryCheckTimeout)
	recovered, err := s.paymentSvc.ReconcilePendingWxpayOrders(reconcileCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending wxpay orders", "error", err)
	} else if recovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid wxpay orders", "count", recovered)
	}

	expireCtx, cancel := context.WithTimeout(parentCtx, expiryCheckTimeout)
	defer cancel()
	expired, err := s.paymentSvc.ExpireTimedOutOrders(expireCtx)
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
		return
	}
	if expired > 0 {
		slog.Info("[PaymentOrderExpiry] expired timed-out orders", "count", expired)
	}
}
