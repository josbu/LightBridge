//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usagePricingFailureRecorderStub struct {
	UsageBillingRepository
	failure *UsagePricingFailure
	err     error
	ctxErr  error
}

func (s *usagePricingFailureRecorderStub) RecordPricingFailure(ctx context.Context, failure *UsagePricingFailure) error {
	s.failure = failure
	s.ctxErr = ctx.Err()
	return s.err
}

func TestUsagePricingFailureNormalizeDeduplicatesModelsAndUsesUTC(t *testing.T) {
	failure := &UsagePricingFailure{
		RequestID:     " request-1 ",
		Protocol:      " gateway ",
		BillingModels: []string{"gpt-5", " GPT-5 ", "", "gpt-5-mini"},
		PricingError:  " missing pricing ",
		CreatedAt:     time.Date(2026, 7, 11, 8, 0, 0, 0, time.FixedZone("TPE", 8*60*60)),
	}

	failure.Normalize()

	require.Equal(t, "request-1", failure.RequestID)
	require.Equal(t, "gateway", failure.Protocol)
	require.Equal(t, []string{"gpt-5", "gpt-5-mini"}, failure.BillingModels)
	require.Equal(t, "missing pricing", failure.PricingError)
	require.Equal(t, time.UTC, failure.CreatedAt.Location())
}

func TestPersistUsagePricingFailureRequiresDurableRecorder(t *testing.T) {
	failure := &UsagePricingFailure{
		RequestID:    "request-1",
		Protocol:     UsagePricingFailureProtocolGateway,
		PricingError: "missing pricing",
	}

	err := persistUsagePricingFailure(context.Background(), nil, failure)
	require.ErrorIs(t, err, ErrUsagePricingFailureRecorderUnavailable)
}

func TestPersistUsagePricingFailureUsesDetachedContext(t *testing.T) {
	recorder := &usagePricingFailureRecorderStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := persistUsagePricingFailure(ctx, recorder, &UsagePricingFailure{
		RequestID:    "request-1",
		Protocol:     UsagePricingFailureProtocolGateway,
		PricingError: "missing pricing",
	})

	require.NoError(t, err)
	require.NoError(t, recorder.ctxErr)
	require.NotNil(t, recorder.failure)
}

func TestNewUsagePricingPendingErrorPreservesSentinels(t *testing.T) {
	persistenceErr := errors.New("database unavailable")
	err := newUsagePricingPendingError(errors.New("pricing missing"), persistenceErr)
	require.ErrorIs(t, err, ErrUsagePricingPending)
	require.ErrorIs(t, err, persistenceErr)
	require.Contains(t, err.Error(), "database unavailable")
}
