# v0.1.8 (2026-06-13)

## ✨ Features

- **LightBridge Connect**: Deep integration with New API instances
  - Real-time quota monitoring and auto-sync (every 5 minutes)
  - Smart alert system with multi-channel notifications (Dashboard/Email/Webhook)
  - Auto-disable on quota exhaustion
  - AES-256-GCM encrypted token storage
  - Complete audit logs for all operations

## 🗄️ Database

- Add `accounts.lightbridge_connect` JSONB field
- Add `lightbridge_connect_quota_logs` table for audit
- Add `lightbridge_connect_alerts` table for alert history
- Migration: `151_lightbridge_connect.sql`

## 🎨 UI/UX

- New `LightBridgeConnectConfig` component for account configuration
- Enhanced `CreateAccountModal` with LightBridge Connect support
- Auto-detection for New API instances
- Full i18n support (English/Chinese)

## 🔧 Backend

- New `LightBridgeConnectService` for core business logic
- New `LightBridgeConnectSyncService` for background sync
- 4 new API endpoints for quota management
- Complete Wire dependency injection setup

## 📚 Documentation

- Comprehensive deployment guide
- User manual
- Troubleshooting guide
- Integration test scripts
