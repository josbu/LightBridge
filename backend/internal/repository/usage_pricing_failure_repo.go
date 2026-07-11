package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/WilliamWang1721/LightBridge/internal/service"
)

// RecordPricingFailure implements service.UsagePricingFailureRepository.
// Repeated observations of the same request update the recovery payload and
// increment attempts instead of creating duplicate financial work items.
func (r *usageBillingRepository) RecordPricingFailure(ctx context.Context, failure *service.UsagePricingFailure) error {
	if failure == nil {
		return errors.New("usage pricing failure is nil")
	}
	if r == nil || r.db == nil {
		return errors.New("usage billing repository db is nil")
	}

	failure.Normalize()
	payload, err := json.Marshal(failure)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO usage_pricing_failures (
			request_id,
			api_key_id,
			user_id,
			account_id,
			protocol,
			platform,
			status,
			payload,
			pricing_error,
			attempts,
			first_seen_at,
			last_seen_at
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), 'pending', $7::jsonb, $8, 1, $9, $9)
		ON CONFLICT (request_id, api_key_id, protocol) DO UPDATE SET
			platform = EXCLUDED.platform,
			status = 'pending',
			payload = EXCLUDED.payload,
			pricing_error = EXCLUDED.pricing_error,
			attempts = usage_pricing_failures.attempts + 1,
			last_seen_at = EXCLUDED.last_seen_at,
			resolved_at = NULL
	`,
		failure.RequestID,
		failure.APIKeyID,
		failure.UserID,
		failure.AccountID,
		failure.Protocol,
		failure.Platform,
		payload,
		failure.PricingError,
		failure.CreatedAt,
	)
	return err
}
