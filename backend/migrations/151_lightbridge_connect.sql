-- Migration 151: LightBridge Connect - New API Integration
-- Adds support for deep integration with New API instances

-- Add lightbridge_connect configuration to accounts table
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS lightbridge_connect JSONB;

-- Add comment for documentation
COMMENT ON COLUMN accounts.lightbridge_connect IS 'LightBridge Connect configuration for deep integration with external services (e.g., New API)';

-- Example lightbridge_connect structure:
-- {
--   "type": "new-api",
--   "instance_url": "https://api.example.com",
--   "system_token": "encrypted_token",
--   "user_id": 123,
--   "username": "user@example.com",
--   "quota": {
--     "balance": 1000000,
--     "used": 500000,
--     "last_sync_at": "2026-06-13T19:00:00Z",
--     "currency": "CNY"
--   },
--   "alert": {
--     "enabled": true,
--     "threshold": 10000,
--     "channels": ["email", "webhook", "dashboard"],
--     "auto_disable_on_low": true
--   },
--   "webhook_url": "https://webhook.example.com/notify",
--   "sync_interval": 300,
--   "last_verified_at": "2026-06-13T18:00:00Z"
-- }

-- Create table for quota change logs (optional but recommended for audit)
CREATE TABLE IF NOT EXISTS lightbridge_connect_quota_logs (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  balance_before BIGINT,
  balance_after BIGINT,
  change_amount BIGINT,
  sync_type VARCHAR(20) NOT NULL DEFAULT 'auto', -- 'auto', 'manual', 'alert', 'init'
  sync_success BOOLEAN NOT NULL DEFAULT TRUE,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_lbc_quota_logs_account_created
  ON lightbridge_connect_quota_logs(account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lbc_quota_logs_sync_type
  ON lightbridge_connect_quota_logs(sync_type, created_at DESC);

-- Add GIN index on lightbridge_connect for efficient JSON queries
CREATE INDEX IF NOT EXISTS idx_accounts_lightbridge_connect
  ON accounts USING GIN (lightbridge_connect);

-- Create table for LightBridge Connect alerts
CREATE TABLE IF NOT EXISTS lightbridge_connect_alerts (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  alert_type VARCHAR(50) NOT NULL, -- 'quota_low', 'quota_exhausted', 'sync_failed', 'token_invalid'
  severity VARCHAR(20) NOT NULL DEFAULT 'warning', -- 'info', 'warning', 'critical'
  message TEXT NOT NULL,
  metadata JSONB,
  acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create indexes for alerts
CREATE INDEX IF NOT EXISTS idx_lbc_alerts_account_created
  ON lightbridge_connect_alerts(account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lbc_alerts_unacknowledged
  ON lightbridge_connect_alerts(account_id, acknowledged, created_at DESC)
  WHERE NOT acknowledged;

CREATE INDEX IF NOT EXISTS idx_lbc_alerts_type_severity
  ON lightbridge_connect_alerts(alert_type, severity, created_at DESC);

-- Add comment
COMMENT ON TABLE lightbridge_connect_quota_logs IS 'Audit log for LightBridge Connect quota synchronization';
COMMENT ON TABLE lightbridge_connect_alerts IS 'Alert records for LightBridge Connect monitoring';
