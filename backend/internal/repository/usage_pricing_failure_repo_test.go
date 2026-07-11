package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingRepositoryRecordPricingFailureUpsertsRecoveryLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, 7, 11, 1, 2, 3, 0, time.UTC)
	failure := &service.UsagePricingFailure{
		RequestID:      "request-1",
		Protocol:       service.UsagePricingFailureProtocolGateway,
		Platform:       service.PlatformAnthropic,
		UserID:         11,
		APIKeyID:       22,
		AccountID:      33,
		BillingModel:   "unknown-model",
		BillingModels:  []string{"unknown-model"},
		InputTokens:    100,
		OutputTokens:   20,
		RateMultiplier: 1.2,
		PricingError:   "pricing not found",
		CreatedAt:      now,
	}

	mock.ExpectExec("INSERT INTO usage_pricing_failures").
		WithArgs(
			"request-1",
			int64(22),
			int64(11),
			int64(33),
			service.UsagePricingFailureProtocolGateway,
			service.PlatformAnthropic,
			sqlmock.AnyArg(),
			"pricing not found",
			now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := &usageBillingRepository{db: db}
	require.NoError(t, repo.RecordPricingFailure(context.Background(), failure))
	require.NoError(t, mock.ExpectationsWereMet())
}
