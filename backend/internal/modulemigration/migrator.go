package modulemigration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/modules"
)

const (
	SourceLightBridge = "lightbridge"
	SourceSub2API     = "sub2api"

	openAIProviderID = "openai"
)

type Options struct {
	SourceKind                string
	SourceDriver              string
	SourceDSN                 string
	TargetDriver              string
	TargetDSN                 string
	OpenAIModulePackage       string
	OpenAIModulePublicKeyPath string
	ModuleDataDir             string
	DryRun                    bool
	InstallOpenAIModule       bool
	EnableOpenAIModule        bool
	// SameDatabase indicates that the source and target point at the same
	// database (the in-place upgrade auto-migration). When true, legacy OpenAI
	// accounts are converted in place instead of being copied into a new
	// `platform='module'` row — otherwise the original `platform='openai'` row
	// would be left behind as a duplicate and would re-trigger the migration on
	// every startup.
	SameDatabase bool
}

type Report struct {
	SourceKind                 string   `json:"source_kind"`
	DryRun                     bool     `json:"dry_run"`
	OpenAIModuleID             string   `json:"openai_module_id,omitempty"`
	OpenAIModuleVersion        string   `json:"openai_module_version,omitempty"`
	OpenAIModuleStatus         string   `json:"openai_module_status,omitempty"`
	ProxiesScanned             int      `json:"proxies_scanned"`
	ProxiesMigrated            int      `json:"proxies_migrated"`
	AccountsScanned            int      `json:"accounts_scanned"`
	AccountsMigrated           int      `json:"accounts_migrated"`
	OpenAIAccountsMigrated     int      `json:"openai_accounts_migrated"`
	CompatibleAccountsMigrated int      `json:"compatible_accounts_migrated"`
	AccountsSkipped            int      `json:"accounts_skipped"`
	Warnings                   []string `json:"warnings,omitempty"`
}

type Migrator struct {
	source *sql.DB
	target *sql.DB
	opts   Options
	now    func() time.Time
}

type sourceRow map[string]any

type proxyRecord struct {
	LegacyID int64
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
	Status   string
}

type accountRecord struct {
	LegacyID    string
	Name        string
	Notes       string
	Platform    string
	ProviderID  string
	Type        string
	Credentials map[string]any
	Extra       map[string]any
	ProxyID     *int64
	Concurrency int
	LoadFactor  *int
	Priority    int
	Status      string
	Schedulable bool
	ExpiresAt   any
}

func Run(ctx context.Context, opts Options) (*Report, error) {
	source, err := sql.Open(opts.SourceDriver, opts.SourceDSN)
	if err != nil {
		return nil, fmt.Errorf("open source db: %w", err)
	}
	defer func() { _ = source.Close() }()

	target, err := sql.Open(opts.TargetDriver, opts.TargetDSN)
	if err != nil {
		return nil, fmt.Errorf("open target db: %w", err)
	}
	defer func() { _ = target.Close() }()

	m := &Migrator{
		source: source,
		target: target,
		opts:   normalizeOptions(opts),
		now:    func() time.Time { return time.Now().UTC() },
	}
	return m.Run(ctx)
}

func normalizeOptions(opts Options) Options {
	opts.SourceKind = strings.ToLower(strings.TrimSpace(opts.SourceKind))
	opts.SourceDriver = strings.TrimSpace(opts.SourceDriver)
	opts.TargetDriver = strings.TrimSpace(opts.TargetDriver)
	opts.OpenAIModulePackage = strings.TrimSpace(opts.OpenAIModulePackage)
	opts.OpenAIModulePublicKeyPath = strings.TrimSpace(opts.OpenAIModulePublicKeyPath)
	opts.ModuleDataDir = strings.TrimSpace(opts.ModuleDataDir)
	if opts.SourceKind == "" {
		opts.SourceKind = SourceLightBridge
	}
	if opts.ModuleDataDir == "" {
		opts.ModuleDataDir = "data"
	}
	// Treat identical source/target as the same database so OpenAI accounts are
	// converted in place rather than duplicated into a parallel module account.
	if !opts.SameDatabase && opts.SourceDriver == opts.TargetDriver &&
		opts.SourceDSN != "" && opts.SourceDSN == opts.TargetDSN {
		opts.SameDatabase = true
	}
	return opts
}

func (m *Migrator) Run(ctx context.Context) (*Report, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	if err := m.source.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping source db: %w", err)
	}
	if err := m.target.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping target db: %w", err)
	}

	report := &Report{SourceKind: m.opts.SourceKind, DryRun: m.opts.DryRun}
	proxyMap, err := m.migrateProxies(ctx, report)
	if err != nil {
		return report, err
	}
	if err := m.migrateAccounts(ctx, proxyMap, report); err != nil {
		return report, err
	}
	if m.opts.InstallOpenAIModule {
		if err := m.installOpenAIModule(ctx, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func (m *Migrator) validate() error {
	switch m.opts.SourceKind {
	case SourceLightBridge, SourceSub2API:
	default:
		return fmt.Errorf("unsupported source kind %q", m.opts.SourceKind)
	}
	if m.opts.SourceDriver == "" || m.opts.SourceDSN == "" {
		return errors.New("source driver and dsn are required")
	}
	if m.opts.TargetDriver == "" || m.opts.TargetDSN == "" {
		return errors.New("target driver and dsn are required")
	}
	if m.opts.InstallOpenAIModule && !m.opts.DryRun && m.opts.OpenAIModulePackage == "" {
		return errors.New("openai module package is required when module installation is enabled")
	}
	return nil
}

func (m *Migrator) migrateProxies(ctx context.Context, report *Report) (map[int64]int64, error) {
	result := map[int64]int64{}
	if !m.tableExists(ctx, m.source, "proxies") {
		report.Warnings = append(report.Warnings, "source table proxies not found; account proxy links will be skipped")
		return result, nil
	}

	rows, err := readTable(ctx, m.source, "proxies")
	if err != nil {
		return nil, fmt.Errorf("read source proxies: %w", err)
	}
	for _, row := range rows {
		report.ProxiesScanned++
		record := proxyFromRow(row)
		if record.LegacyID == 0 || record.Host == "" || record.Port == 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip proxy with incomplete fields: legacy_id=%d host=%q port=%d", record.LegacyID, record.Host, record.Port))
			continue
		}
		if m.opts.DryRun {
			report.ProxiesMigrated++
			continue
		}
		id, err := m.upsertProxy(ctx, record)
		if err != nil {
			return nil, fmt.Errorf("migrate proxy %d: %w", record.LegacyID, err)
		}
		result[record.LegacyID] = id
		report.ProxiesMigrated++
	}
	return result, nil
}

func (m *Migrator) migrateAccounts(ctx context.Context, proxyMap map[int64]int64, report *Report) error {
	tableName, err := m.accountSourceTable(ctx)
	if err != nil {
		return err
	}
	rows, err := readTable(ctx, m.source, tableName)
	if err != nil {
		return fmt.Errorf("read source %s: %w", tableName, err)
	}
	for _, row := range rows {
		report.AccountsScanned++
		if sourceRowDeleted(row) {
			report.AccountsSkipped++
			continue
		}
		row["__source_kind"] = m.opts.SourceKind
		record := accountFromRow(m.opts.SourceKind, row)
		if isOpenAIAccount(record, row) {
			normalizeOpenAIAccount(&record, row)
			if oldProxyID := int64FromAny(row["proxy_id"]); oldProxyID != 0 {
				if newProxyID, ok := proxyMap[oldProxyID]; ok {
					record.ProxyID = &newProxyID
				}
			}
			if m.opts.DryRun {
				report.AccountsMigrated++
				report.OpenAIAccountsMigrated++
				continue
			}
			if skipped, err := m.skipDeletedOpenAIAccount(ctx, record); err != nil {
				return fmt.Errorf("check deleted openai account %s: %w", record.LegacyID, err)
			} else if skipped {
				report.AccountsSkipped++
				continue
			}
			if err := m.upsertOpenAIAccount(ctx, record); err != nil {
				return fmt.Errorf("migrate openai account %s: %w", record.LegacyID, err)
			}
			report.AccountsMigrated++
			report.OpenAIAccountsMigrated++
			continue
		}
		if m.opts.SourceKind != SourceSub2API {
			report.AccountsSkipped++
			continue
		}
		if !normalizeCompatibleAccount(&record, row) {
			report.AccountsSkipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skip Sub2API account %q because provider/platform could not be inferred", record.LegacyID))
			continue
		}
		if oldProxyID := int64FromAny(row["proxy_id"]); oldProxyID != 0 {
			if newProxyID, ok := proxyMap[oldProxyID]; ok {
				record.ProxyID = &newProxyID
			}
		}
		if m.opts.DryRun {
			report.AccountsMigrated++
			report.CompatibleAccountsMigrated++
			continue
		}
		if skipped, err := m.skipDeletedCompatibleAccount(ctx, record); err != nil {
			return fmt.Errorf("check deleted compatible account %s: %w", record.LegacyID, err)
		} else if skipped {
			report.AccountsSkipped++
			continue
		}
		if err := m.upsertCompatibleAccount(ctx, record); err != nil {
			return fmt.Errorf("migrate compatible account %s: %w", record.LegacyID, err)
		}
		report.AccountsMigrated++
		report.CompatibleAccountsMigrated++
	}
	return nil
}

func (m *Migrator) accountSourceTable(ctx context.Context) (string, error) {
	if m.tableExists(ctx, m.source, "accounts") {
		return "accounts", nil
	}
	if m.opts.SourceKind == SourceSub2API && m.tableExists(ctx, m.source, "tokens") {
		return "tokens", nil
	}
	return "", fmt.Errorf("source %s has no supported account table; expected accounts%s", m.opts.SourceKind, sub2APITokensHint(m.opts.SourceKind))
}

func sub2APITokensHint(sourceKind string) string {
	if sourceKind == SourceSub2API {
		return " or tokens"
	}
	return ""
}

func (m *Migrator) installOpenAIModule(ctx context.Context, report *Report) error {
	if m.opts.DryRun {
		report.OpenAIModuleID = openAIProviderID
		report.OpenAIModuleStatus = "dry_run"
		return nil
	}
	if _, err := os.Stat(m.opts.OpenAIModulePackage); err != nil {
		return fmt.Errorf("openai module package is not readable: %w", err)
	}
	store := newSQLModuleStore(m.target)
	var verifier modules.SignatureVerifier
	if m.opts.OpenAIModulePublicKeyPath != "" {
		loadedVerifier, err := modules.NewEd25519SignatureVerifierFromFile(m.opts.OpenAIModulePublicKeyPath)
		if err != nil {
			return fmt.Errorf("load openai module signing public key: %w", err)
		}
		verifier = loadedVerifier
	}
	installer := modules.NewPackageInstallerWithVerifier(m.opts.ModuleDataDir, store, verifier)
	installed, err := installer.InstallArchive(ctx, m.opts.OpenAIModulePackage)
	if err != nil {
		return fmt.Errorf("install openai provider module: %w", err)
	}
	if err := store.ApprovePermissions(ctx, installed.ID); err != nil {
		return fmt.Errorf("approve openai provider permissions: %w", err)
	}
	if m.opts.EnableOpenAIModule {
		now := m.now()
		installed.Status = modules.ModuleStatusEnabled
		installed.EnabledAt = &now
		if err := store.SaveInstalled(ctx, *installed); err != nil {
			return fmt.Errorf("enable openai provider module: %w", err)
		}
	}
	report.OpenAIModuleID = installed.ID
	report.OpenAIModuleVersion = installed.Version
	report.OpenAIModuleStatus = string(installed.Status)
	return nil
}

func (m *Migrator) upsertProxy(ctx context.Context, record proxyRecord) (int64, error) {
	var existingID int64
	err := m.target.QueryRowContext(ctx, `
SELECT id FROM proxies
WHERE name = $1 AND protocol = $2 AND host = $3 AND port = $4 AND deleted_at IS NULL
ORDER BY id ASC LIMIT 1`, record.Name, record.Protocol, record.Host, record.Port).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if record.Status == "" {
		record.Status = "active"
	}
	err = m.target.QueryRowContext(ctx, `
INSERT INTO proxies (name, protocol, host, port, username, password, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, NOW(), NOW())
RETURNING id`, record.Name, record.Protocol, record.Host, record.Port, record.Username, record.Password, record.Status).Scan(&existingID)
	return existingID, err
}

func (m *Migrator) upsertOpenAIAccount(ctx context.Context, record accountRecord) error {
	credentials, err := json.Marshal(record.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	extra, err := json.Marshal(record.Extra)
	if err != nil {
		return fmt.Errorf("marshal extra: %w", err)
	}
	legacyID := strings.TrimSpace(stringFromAny(record.LegacyID))
	var existingID int64
	err = m.target.QueryRowContext(ctx, `
SELECT id FROM accounts
WHERE extra->>'provider_id' = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NULL
ORDER BY id ASC LIMIT 1`, openAIProviderID, m.opts.SourceKind, legacyID).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		_, err = m.target.ExecContext(ctx, `
UPDATE accounts
SET name = $1, notes = NULLIF($2, ''), platform = 'module', type = $3,
    credentials = $4, extra = $5, proxy_id = $6, concurrency = $7, load_factor = $8,
    priority = $9, status = $10, schedulable = $11, updated_at = NOW()
WHERE id = $12`,
			record.Name, record.Notes, record.Type, string(credentials), string(extra), record.ProxyID,
			record.Concurrency, record.LoadFactor, record.Priority, record.Status, record.Schedulable, existingID)
		return err
	}

	// Same-database (in-place upgrade) migration: convert the legacy
	// `platform='openai'` account in place instead of inserting a second
	// `platform='module'` row. Inserting a copy would (a) leave the original
	// openai account behind as a duplicate, and (b) keep hasLegacyOpenAIAccounts
	// true, re-running this migration — and re-adding the copy — on every boot.
	// Converting in place also preserves the account id so API keys, groups and
	// usage records keep pointing at the same account.
	if m.opts.SameDatabase {
		if sourceID, ok := numericLegacyID(record.LegacyID); ok {
			_, err := m.target.ExecContext(ctx, `
UPDATE accounts
SET name = $1, notes = NULLIF($2, ''), platform = 'module', type = $3,
    credentials = $4, extra = $5, proxy_id = COALESCE($6, proxy_id), concurrency = $7, load_factor = $8,
    priority = $9, status = $10, schedulable = $11, updated_at = NOW()
WHERE id = $12 AND platform = 'openai' AND deleted_at IS NULL`,
				record.Name, record.Notes, record.Type, string(credentials), string(extra), record.ProxyID,
				record.Concurrency, record.LoadFactor, record.Priority, record.Status, record.Schedulable, sourceID)
			if err != nil {
				return err
			}
			// 0 rows affected means the source row is no longer a live
			// openai account (already converted, deleted, or remapped); skip
			// rather than insert a duplicate.
			return nil
		}
		// LegacyID is not the numeric account id (cannot safely convert in
		// place); skip to avoid creating a duplicate.
		return nil
	}

	_, err = m.target.ExecContext(ctx, `
INSERT INTO accounts (
  name, notes, platform, type, credentials, extra, proxy_id,
  concurrency, load_factor, priority, rate_multiplier, status, schedulable,
  created_at, updated_at
) VALUES (
  $1, NULLIF($2, ''), 'module', $3, $4, $5, $6,
  $7, $8, $9, 1.0, $10, $11, NOW(), NOW()
)`,
		record.Name, record.Notes, record.Type, string(credentials), string(extra), record.ProxyID,
		record.Concurrency, record.LoadFactor, record.Priority, record.Status, record.Schedulable)
	return err
}

func (m *Migrator) skipDeletedOpenAIAccount(ctx context.Context, record accountRecord) (bool, error) {
	legacyID := strings.TrimSpace(stringFromAny(record.LegacyID))
	return m.hasDeletedMigrationAccount(ctx, `
SELECT COUNT(*) FROM accounts
WHERE extra->>'provider_id' = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NOT NULL`, openAIProviderID, m.opts.SourceKind, legacyID)
}

func (m *Migrator) upsertCompatibleAccount(ctx context.Context, record accountRecord) error {
	credentials, err := json.Marshal(record.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	extra, err := json.Marshal(record.Extra)
	if err != nil {
		return fmt.Errorf("marshal extra: %w", err)
	}
	legacyID := strings.TrimSpace(stringFromAny(record.LegacyID))
	var existingID int64
	err = m.target.QueryRowContext(ctx, `
SELECT id FROM accounts
WHERE platform = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NULL
ORDER BY id ASC LIMIT 1`, record.Platform, m.opts.SourceKind, legacyID).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		_, err = m.target.ExecContext(ctx, `
UPDATE accounts
SET name = $1, notes = NULLIF($2, ''), platform = $3, type = $4,
    credentials = $5, extra = $6, proxy_id = $7, concurrency = $8, load_factor = $9,
    priority = $10, status = $11, schedulable = $12, updated_at = NOW()
WHERE id = $13`,
			record.Name, record.Notes, record.Platform, record.Type, string(credentials), string(extra), record.ProxyID,
			record.Concurrency, record.LoadFactor, record.Priority, record.Status, record.Schedulable, existingID)
		return err
	}
	_, err = m.target.ExecContext(ctx, `
INSERT INTO accounts (
  name, notes, platform, type, credentials, extra, proxy_id,
  concurrency, load_factor, priority, rate_multiplier, status, schedulable,
  created_at, updated_at
) VALUES (
  $1, NULLIF($2, ''), $3, $4, $5, $6, $7,
  $8, $9, $10, 1.0, $11, $12, NOW(), NOW()
)`,
		record.Name, record.Notes, record.Platform, record.Type, string(credentials), string(extra), record.ProxyID,
		record.Concurrency, record.LoadFactor, record.Priority, record.Status, record.Schedulable)
	return err
}

func (m *Migrator) skipDeletedCompatibleAccount(ctx context.Context, record accountRecord) (bool, error) {
	legacyID := strings.TrimSpace(stringFromAny(record.LegacyID))
	return m.hasDeletedMigrationAccount(ctx, `
SELECT COUNT(*) FROM accounts
WHERE platform = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NOT NULL`, record.Platform, m.opts.SourceKind, legacyID)
}

func (m *Migrator) hasDeletedMigrationAccount(ctx context.Context, query string, args ...any) (bool, error) {
	var count int
	err := m.target.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func numericLegacyID(legacyID string) (int64, bool) {
	trimmed := strings.TrimSpace(legacyID)
	if trimmed == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (m *Migrator) tableExists(ctx context.Context, db *sql.DB, tableName string) bool {
	var exists bool
	err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_schema = ANY (current_schemas(false)) AND table_name = $1
)`, tableName).Scan(&exists)
	if err == nil {
		return exists
	}
	err = db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM sqlite_master
  WHERE type = 'table' AND name = ?
)`, tableName).Scan(&exists)
	return err == nil && exists
}

func readTable(ctx context.Context, db *sql.DB, tableName string) ([]sourceRow, error) {
	if !safeIdentifier(tableName) {
		return nil, fmt.Errorf("unsafe table name %q", tableName)
	}
	rows, err := db.QueryContext(ctx, `SELECT * FROM `+tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result []sourceRow
	for rows.Next() {
		values := make([]any, len(columns))
		scan := make([]any, len(columns))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := sourceRow{}
		for i, column := range columns {
			row[column] = normalizeDBValue(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func proxyFromRow(row sourceRow) proxyRecord {
	return proxyRecord{
		LegacyID: int64FromAny(first(row, "id", "proxy_id")),
		Name:     stringFromAny(first(row, "name", "tag")),
		Protocol: strings.ToLower(defaultString(stringFromAny(first(row, "protocol", "type", "scheme")), "http")),
		Host:     stringFromAny(first(row, "host", "hostname", "server")),
		Port:     int(int64FromAny(first(row, "port"))),
		Username: stringFromAny(first(row, "username", "user")),
		Password: stringFromAny(first(row, "password", "pass")),
		Status:   defaultString(stringFromAny(first(row, "status")), "active"),
	}
}

func accountFromRow(sourceKind string, row sourceRow) accountRecord {
	credentials := jsonMapFromAny(first(row, "credentials"))
	extra := jsonMapFromAny(first(row, "extra"))
	if credentials == nil {
		credentials = map[string]any{}
	}
	if extra == nil {
		extra = map[string]any{}
	}
	return accountRecord{
		LegacyID:    legacyID(row),
		Name:        defaultString(stringFromAny(first(row, "name", "label", "remark")), defaultAccountName(sourceKind, row)),
		Notes:       stringFromAny(first(row, "notes", "note", "description")),
		Platform:    normalizeLegacyToken(strings.ToLower(stringFromAny(first(row, "platform", "provider")))),
		ProviderID:  normalizeLegacyToken(strings.ToLower(stringFromAny(first(row, "provider_id", "provider")))),
		Type:        normalizeAccountTypeString(strings.ToLower(stringFromAny(first(row, "auth_type", "account_type")))),
		Credentials: credentials,
		Extra:       extra,
		ProxyID:     int64PtrFromAny(first(row, "proxy_id")),
		Concurrency: defaultInt(int(int64FromAny(first(row, "concurrency", "max_concurrency"))), 3),
		LoadFactor:  intPtrFromAny(first(row, "load_factor")),
		Priority:    defaultInt(int(int64FromAny(first(row, "priority"))), 50),
		Status:      defaultString(stringFromAny(first(row, "status")), "active"),
		Schedulable: boolFromAny(first(row, "schedulable", "enabled"), true),
		ExpiresAt:   first(row, "expires_at"),
	}
}

func normalizeOpenAIAccount(record *accountRecord, row sourceRow) {
	record.Platform = "module"
	record.ProviderID = openAIProviderID
	record.Type = normalizeAccountTypeString(record.Type)
	if record.Type == "" {
		record.Type = inferAccountType(record.Credentials, row)
	}
	copyCredential(record.Credentials, row, "api_key", "api_key", "key", "token", "sk")
	copyCredential(record.Credentials, row, "access_token", "access_token")
	copyCredential(record.Credentials, row, "refresh_token", "refresh_token", "rt")
	copyCredential(record.Credentials, row, "id_token", "id_token")
	copyCredential(record.Credentials, row, "client_id", "client_id")
	copyCredential(record.Credentials, row, "base_url", "base_url", "api_base")
	copyLegacyFlag(record.Extra, row, "openai_passthrough")
	copyLegacyFlag(record.Extra, row, "codex_cli_only")
	copyLegacyFlag(record.Extra, row, "openai_ws_mode")
	copyLegacyFlag(record.Extra, row, "openai_oauth_responses_websockets_v2_enabled")
	copyLegacyFlag(record.Extra, row, "openai_oauth_responses_websockets_v2_mode")
	record.Extra["provider_id"] = openAIProviderID
	record.Extra["module_migration"] = map[string]any{
		"source":            stringFromAny(row["__source_kind"]),
		"legacy_account_id": record.LegacyID,
		"provider_id":       openAIProviderID,
		"migrated_at":       time.Now().UTC().Format(time.RFC3339),
	}
	if record.Type == "" {
		record.Type = inferAccountType(record.Credentials, row)
	}
}

func normalizeCompatibleAccount(record *accountRecord, row sourceRow) bool {
	provider, _ := normalizedProvider(row, *record)
	if provider == "" || provider == openAIProviderID {
		return false
	}
	record.Platform = provider
	record.ProviderID = provider
	record.Type = normalizeAccountTypeString(record.Type)
	if record.Type == "" {
		record.Type = inferAccountType(record.Credentials, row)
	}
	copyCredential(record.Credentials, row, "api_key", "api_key", "key", "token", "sk")
	copyCredential(record.Credentials, row, "access_token", "access_token")
	copyCredential(record.Credentials, row, "refresh_token", "refresh_token", "rt")
	copyCredential(record.Credentials, row, "id_token", "id_token")
	copyCredential(record.Credentials, row, "client_id", "client_id")
	copyCredential(record.Credentials, row, "base_url", "base_url", "api_base")
	record.Extra["module_migration"] = map[string]any{
		"source":             stringFromAny(row["__source_kind"]),
		"legacy_account_id":  record.LegacyID,
		"provider_id":        provider,
		"compatibility_mode": true,
		"migrated_at":        time.Now().UTC().Format(time.RFC3339),
	}
	return true
}

func isOpenAIAccount(record accountRecord, row sourceRow) bool {
	provider, explicit := normalizedProvider(row, record)
	if provider == openAIProviderID {
		return true
	}
	if explicit {
		return false
	}
	if strings.EqualFold(stringFromAny(first(row, "provider", "service")), "openai") {
		return true
	}
	if hasAny(row, "openai_passthrough", "codex_cli_only", "refresh_token", "access_token") {
		return true
	}
	if key := stringFromAny(first(row, "api_key", "key", "sk")); strings.HasPrefix(key, "sk-") {
		return true
	}
	credentialsJSON, _ := json.Marshal(record.Credentials)
	return strings.Contains(strings.ToLower(string(credentialsJSON)), "openai")
}

func normalizedProvider(row sourceRow, record accountRecord) (string, bool) {
	explicitRaw := strings.ToLower(strings.TrimSpace(defaultString(
		stringFromAny(first(row, "provider_id", "provider", "platform", "service", "vendor")),
		defaultString(record.ProviderID, record.Platform),
	)))
	if explicitRaw != "" {
		if provider, ok := providerFromLegacyValue(explicitRaw); ok {
			return provider, true
		}
		if normalizeAccountTypeString(explicitRaw) == "" {
			return normalizeLegacyToken(explicitRaw), true
		}
	}
	for _, raw := range []string{
		stringFromAny(first(row, "type")),
		stringFromAny(first(row, "auth_type", "account_type")),
	} {
		if provider, ok := providerFromLegacyValue(raw); ok {
			return provider, true
		}
	}
	return "", false
}

func providerFromLegacyValue(value string) (string, bool) {
	raw := normalizeLegacyToken(value)
	if raw == "" {
		return "", false
	}
	switch raw {
	case "openai", "chatgpt", "codex":
		return openAIProviderID, true
	case "openai_oauth", "chatgpt_oauth", "codex_oauth":
		return openAIProviderID, true
	case "anthropic", "claude", "claude_code", "anthropic_oauth", "claude_oauth", "claude_code_oauth":
		return "anthropic", true
	case "google", "gemini", "ai_studio", "code_assist", "google_oauth", "gemini_oauth", "ai_studio_oauth", "code_assist_oauth":
		return "gemini", true
	case "antigravity", "antigravity_oauth":
		return "antigravity", true
	default:
		return "", false
	}
}

func inferAccountType(credentials map[string]any, row sourceRow) string {
	for _, value := range []string{
		stringFromAny(first(row, "auth_type", "account_type")),
		stringFromAny(first(row, "type")),
		stringFromAny(credentials["auth_type"]),
		stringFromAny(credentials["type"]),
	} {
		if accountType := normalizeAccountTypeString(value); accountType != "" {
			return accountType
		}
	}
	if stringFromAny(first(row, "refresh_token", "rt")) != "" || stringFromAny(credentials["refresh_token"]) != "" || stringFromAny(credentials["access_token"]) != "" {
		return "oauth"
	}
	return "apikey"
}

func normalizeLegacyToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return strings.Trim(value, "_")
}

func normalizeAccountTypeString(value string) string {
	raw := normalizeLegacyToken(value)
	switch raw {
	case "apikey", "api_key", "key", "token", "pat":
		return "apikey"
	case "oauth", "oauth2", "oauth_token", "openai_oauth", "anthropic_oauth", "claude_oauth", "gemini_oauth", "google_oauth", "ai_studio_oauth", "code_assist_oauth", "antigravity_oauth":
		return "oauth"
	case "setup_token", "setup":
		return "setup-token"
	case "bedrock":
		return "bedrock"
	case "service_account", "google_service_account", "vertex_service_account":
		return "service_account"
	case "upstream":
		return "upstream"
	default:
		return ""
	}
}

func defaultAccountName(sourceKind string, row sourceRow) string {
	if email := stringFromAny(first(row, "email")); email != "" {
		return email
	}
	return "Migrated OpenAI " + sourceKind + " account"
}

func legacyID(row sourceRow) string {
	id := stringFromAny(first(row, "id", "account_id", "token_id"))
	if id != "" {
		return id
	}
	return stringFromAny(first(row, "name", "label", "email", "api_key", "refresh_token"))
}

func copyCredential(credentials map[string]any, row sourceRow, target string, sources ...string) {
	if stringFromAny(credentials[target]) != "" {
		return
	}
	if value := stringFromAny(first(row, sources...)); value != "" {
		credentials[target] = value
	}
}

func copyLegacyFlag(extra map[string]any, row sourceRow, key string) {
	if _, ok := extra[key]; ok {
		return
	}
	if value, ok := row[key]; ok && value != nil {
		extra[key] = value
	}
}

func first(row sourceRow, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
	}
	return nil
}

func hasAny(row sourceRow, keys ...string) bool {
	for _, key := range keys {
		if value := stringFromAny(row[key]); value != "" {
			return true
		}
	}
	return false
}

func normalizeDBValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func jsonMapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		return v
	case []byte:
		var result map[string]any
		if json.Unmarshal(v, &result) == nil {
			return result
		}
	case string:
		var result map[string]any
		if json.Unmarshal([]byte(v), &result) == nil {
			return result
		}
	}
	return nil
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n
	}
}

func int64PtrFromAny(value any) *int64 {
	n := int64FromAny(value)
	if n == 0 {
		return nil
	}
	return &n
}

func intPtrFromAny(value any) *int {
	n := int(int64FromAny(value))
	if n == 0 {
		return nil
	}
	return &n
}

func boolFromAny(value any, fallback bool) bool {
	switch v := value.(type) {
	case nil:
		return fallback
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case int64:
		return v != 0
	case int:
		return v != 0
	}
	return fallback
}

func sourceRowDeleted(row sourceRow) bool {
	value, ok := row["deleted_at"]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []byte:
		return strings.TrimSpace(string(v)) != ""
	default:
		return true
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func defaultInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func safeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}
