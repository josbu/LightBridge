package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	UsagePricingFailureProtocolGateway       = "gateway"
	UsagePricingFailureProtocolOpenAIGateway = "openai_gateway"
)

var (
	// ErrUsagePricingPending indicates that usage was durably recorded for later
	// pricing instead of being silently accepted as a zero-cost request.
	ErrUsagePricingPending = errors.New("usage pricing is pending")

	// ErrUsagePricingFailureRecorderUnavailable is returned when a production
	// billing repository cannot persist an unpriced request. This is deliberately
	// fail-closed: losing the recovery record would make reconciliation impossible.
	ErrUsagePricingFailureRecorderUnavailable = errors.New("usage pricing failure recorder is unavailable")
)

// UsagePricingFailure is the durable recovery payload for a request whose cost
// could not be resolved after the upstream response completed.
//
// The payload intentionally contains pricing inputs rather than a zero-valued
// CostBreakdown. A later reconciler can re-run pricing with updated model data
// without reconstructing mutable request state from unrelated tables.
type UsagePricingFailure struct {
	RequestID          string `json:"request_id"`
	RequestPayloadHash string `json:"request_payload_hash,omitempty"`
	Protocol           string `json:"protocol"`
	Platform           string `json:"platform,omitempty"`

	UserID         int64  `json:"user_id"`
	APIKeyID       int64  `json:"api_key_id"`
	AccountID      int64  `json:"account_id"`
	GroupID        *int64 `json:"group_id,omitempty"`
	SubscriptionID *int64 `json:"subscription_id,omitempty"`
	BillingType    int8   `json:"billing_type"`

	RequestedModel  string   `json:"requested_model,omitempty"`
	BillingModel    string   `json:"billing_model,omitempty"`
	UpstreamModel   string   `json:"upstream_model,omitempty"`
	BillingModels   []string `json:"billing_models,omitempty"`
	ServiceTier     string   `json:"service_tier,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`

	InputTokens           int    `json:"input_tokens"`
	OutputTokens          int    `json:"output_tokens"`
	CacheCreationTokens   int    `json:"cache_creation_tokens"`
	CacheReadTokens       int    `json:"cache_read_tokens"`
	CacheCreation5mTokens int    `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int    `json:"cache_creation_1h_tokens"`
	ImageOutputTokens     int    `json:"image_output_tokens"`
	ImageCount            int    `json:"image_count"`
	ImageSize             string `json:"image_size,omitempty"`

	RateMultiplier        float64 `json:"rate_multiplier"`
	ImageRateMultiplier   float64 `json:"image_rate_multiplier"`
	AccountRateMultiplier float64 `json:"account_rate_multiplier"`
	LongContextThreshold  int     `json:"long_context_threshold,omitempty"`
	LongContextMultiplier float64 `json:"long_context_multiplier,omitempty"`

	InboundEndpoint   string `json:"inbound_endpoint,omitempty"`
	UpstreamEndpoint  string `json:"upstream_endpoint,omitempty"`
	ChannelID         int64  `json:"channel_id,omitempty"`
	ModelMappingChain string `json:"model_mapping_chain,omitempty"`

	PricingError string    `json:"pricing_error"`
	CreatedAt    time.Time `json:"created_at"`
}

// Normalize makes persistence deterministic and removes duplicate model
// candidates while preserving their first-seen priority.
func (f *UsagePricingFailure) Normalize() {
	if f == nil {
		return
	}
	f.RequestID = strings.TrimSpace(f.RequestID)
	f.RequestPayloadHash = strings.TrimSpace(f.RequestPayloadHash)
	f.Protocol = strings.TrimSpace(f.Protocol)
	f.Platform = strings.TrimSpace(f.Platform)
	f.RequestedModel = strings.TrimSpace(f.RequestedModel)
	f.BillingModel = strings.TrimSpace(f.BillingModel)
	f.UpstreamModel = strings.TrimSpace(f.UpstreamModel)
	f.ServiceTier = strings.TrimSpace(f.ServiceTier)
	f.ReasoningEffort = strings.TrimSpace(f.ReasoningEffort)
	f.ImageSize = strings.TrimSpace(f.ImageSize)
	f.InboundEndpoint = strings.TrimSpace(f.InboundEndpoint)
	f.UpstreamEndpoint = strings.TrimSpace(f.UpstreamEndpoint)
	f.ModelMappingChain = strings.TrimSpace(f.ModelMappingChain)
	f.PricingError = strings.TrimSpace(f.PricingError)
	f.BillingModels = uniqueUsageBillingModels(f.BillingModels...)
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	} else {
		f.CreatedAt = f.CreatedAt.UTC()
	}
}

// UsagePricingFailureRepository is an optional extension implemented by the
// production UsageBillingRepository. Keeping it separate avoids widening every
// in-memory billing stub while still requiring durable persistence in production.
type UsagePricingFailureRepository interface {
	RecordPricingFailure(ctx context.Context, failure *UsagePricingFailure) error
}

func persistUsagePricingFailure(ctx context.Context, repo UsageBillingRepository, failure *UsagePricingFailure) error {
	if failure == nil {
		return errors.New("usage pricing failure is nil")
	}
	failure.Normalize()
	if failure.RequestID == "" {
		return ErrUsageBillingRequestIDRequired
	}
	if failure.Protocol == "" {
		return errors.New("usage pricing failure protocol is required")
	}
	if strings.TrimSpace(failure.PricingError) == "" {
		return errors.New("usage pricing failure error is required")
	}

	recorder, ok := repo.(UsagePricingFailureRepository)
	if !ok || recorder == nil {
		return ErrUsagePricingFailureRecorderUnavailable
	}

	pricingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	return recorder.RecordPricingFailure(pricingCtx, failure)
}

func newUsagePricingPendingError(pricingErr, persistenceErr error) error {
	if pricingErr == nil {
		pricingErr = errors.New("unknown pricing error")
	}
	pricingPendingErr := fmt.Errorf("%w: %v", ErrUsagePricingPending, pricingErr)
	if persistenceErr == nil {
		return pricingPendingErr
	}
	return errors.Join(
		pricingPendingErr,
		fmt.Errorf("persist pending usage: %w", persistenceErr),
	)
}

func uniqueUsageBillingModels(models ...string) []string {
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, model)
	}
	return result
}
