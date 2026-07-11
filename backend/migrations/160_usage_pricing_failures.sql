-- Durable recovery ledger for requests that completed upstream but could not be priced.
-- These rows must never be treated as successful zero-cost usage.
CREATE TABLE IF NOT EXISTS usage_pricing_failures (
    id              BIGSERIAL PRIMARY KEY,
    request_id      TEXT NOT NULL,
    api_key_id      BIGINT NOT NULL,
    user_id         BIGINT NOT NULL,
    account_id      BIGINT NOT NULL,
    protocol        VARCHAR(32) NOT NULL,
    platform        VARCHAR(32),
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    payload         JSONB NOT NULL,
    pricing_error   TEXT NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 1,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ,
    CONSTRAINT usage_pricing_failures_status_check
        CHECK (status IN ('pending', 'resolved', 'ignored')),
    CONSTRAINT usage_pricing_failures_attempts_check
        CHECK (attempts > 0),
    CONSTRAINT usage_pricing_failures_request_unique
        UNIQUE (request_id, api_key_id, protocol)
);

CREATE INDEX IF NOT EXISTS idx_usage_pricing_failures_pending
    ON usage_pricing_failures (last_seen_at, id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_usage_pricing_failures_created_at
    ON usage_pricing_failures (first_seen_at DESC);
