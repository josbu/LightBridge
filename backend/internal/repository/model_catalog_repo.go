package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/service"
)

type modelCatalogRepository struct {
	db *sql.DB
}

func NewModelCatalogRepository(db *sql.DB) service.ModelCatalogRepository {
	return &modelCatalogRepository{db: db}
}

func (r *modelCatalogRepository) ReplaceAccountModels(ctx context.Context, accountID int64, platform, source string, modelIDs []string, usageModes []string) (*service.AccountModelSyncState, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("model catalog repository is not configured")
	}
	source = normalizeCatalogSource(source)
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = service.PlatformAnthropic
	}
	usageJSON, err := json.Marshal(usageModes)
	if err != nil {
		return nil, fmt.Errorf("marshal usage modes: %w", err)
	}
	batchID := fmt.Sprintf("%d-%d", accountID, time.Now().UnixNano())

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin model catalog tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM account_model_catalog WHERE account_id = $1 AND source = $2`,
		accountID, source,
	); err != nil {
		return nil, fmt.Errorf("clear account model catalog: %w", err)
	}

	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO account_model_catalog (
				account_id, model_id, platform, source, display_name, usage_modes,
				last_seen_at, sync_batch_id, sync_status, sync_error, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7, $8, NULL, NOW())
			ON CONFLICT (account_id, model_id, source) DO UPDATE SET
				platform = EXCLUDED.platform,
				display_name = EXCLUDED.display_name,
				usage_modes = EXCLUDED.usage_modes,
				last_seen_at = EXCLUDED.last_seen_at,
				sync_batch_id = EXCLUDED.sync_batch_id,
				sync_status = EXCLUDED.sync_status,
				sync_error = NULL,
				updated_at = NOW()`,
			accountID, modelID, platform, source, modelID, usageJSON, batchID, service.ModelCatalogSyncStatusOK,
		); err != nil {
			return nil, fmt.Errorf("upsert account model catalog: %w", err)
		}
	}

	state := &service.AccountModelSyncState{
		AccountID:    accountID,
		Source:       source,
		Status:       service.ModelCatalogSyncStatusOK,
		ModelCount:   len(modelIDs),
		SyncBatchID:  batchID,
		LastSyncedAt: modelCatalogPtrTime(time.Now()),
		UpdatedAt:    time.Now(),
	}
	if err := upsertModelSyncState(ctx, tx, state); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit model catalog tx: %w", err)
	}
	return state, nil
}

func (r *modelCatalogRepository) RecordAccountSyncFailure(ctx context.Context, accountID int64, source, message string) (*service.AccountModelSyncState, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("model catalog repository is not configured")
	}
	state := &service.AccountModelSyncState{
		AccountID:    accountID,
		Source:       normalizeCatalogSource(source),
		Status:       service.ModelCatalogSyncStatusError,
		ErrorMessage: trimForStorage(message, 1000),
		SyncBatchID:  fmt.Sprintf("%d-%d", accountID, time.Now().UnixNano()),
		LastSyncedAt: modelCatalogPtrTime(time.Now()),
		UpdatedAt:    time.Now(),
	}
	if err := upsertModelSyncState(ctx, r.db, state); err != nil {
		return nil, err
	}
	return state, nil
}

func (r *modelCatalogRepository) ListByAccount(ctx context.Context, accountID int64) ([]service.AccountModelCatalogEntry, *service.AccountModelSyncState, error) {
	entries, states, err := r.ListByAccounts(ctx, []int64{accountID})
	if err != nil {
		return nil, nil, err
	}
	return entries, states[accountID], nil
}

func (r *modelCatalogRepository) ListByAccounts(ctx context.Context, accountIDs []int64) ([]service.AccountModelCatalogEntry, map[int64]*service.AccountModelSyncState, error) {
	if r == nil || r.db == nil || len(accountIDs) == 0 {
		return nil, map[int64]*service.AccountModelSyncState{}, nil
	}
	ids := uniqueInt64s(accountIDs)
	args := make([]any, 0, len(ids))
	placeholders := make([]string, 0, len(ids))
	for i, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	query := fmt.Sprintf(
		`SELECT id, account_id, model_id, platform, source, display_name, usage_modes,
				last_seen_at, COALESCE(sync_batch_id, ''), sync_status, COALESCE(sync_error, ''),
				created_at, updated_at
		 FROM account_model_catalog
		 WHERE account_id IN (%s)
		 ORDER BY LOWER(model_id), account_id, source`,
		strings.Join(placeholders, ","),
	)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list account model catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]service.AccountModelCatalogEntry, 0)
	for rows.Next() {
		var entry service.AccountModelCatalogEntry
		var usageRaw []byte
		if err := rows.Scan(
			&entry.ID,
			&entry.AccountID,
			&entry.ModelID,
			&entry.Platform,
			&entry.Source,
			&entry.DisplayName,
			&usageRaw,
			&entry.LastSeenAt,
			&entry.SyncBatchID,
			&entry.SyncStatus,
			&entry.SyncError,
			&entry.CreatedAt,
			&entry.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan account model catalog: %w", err)
		}
		_ = json.Unmarshal(usageRaw, &entry.UsageModes)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate account model catalog: %w", err)
	}

	states, err := r.listSyncStates(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	return entries, states, nil
}

func (r *modelCatalogRepository) listSyncStates(ctx context.Context, accountIDs []int64) (map[int64]*service.AccountModelSyncState, error) {
	out := make(map[int64]*service.AccountModelSyncState, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(accountIDs))
	placeholders := make([]string, 0, len(accountIDs))
	for i, id := range accountIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	query := fmt.Sprintf(
		`SELECT account_id, source, status, model_count, COALESCE(sync_batch_id, ''),
				last_synced_at, COALESCE(error_message, ''), updated_at
		 FROM account_model_sync_state
		 WHERE account_id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list account model sync states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var state service.AccountModelSyncState
		var lastSynced sql.NullTime
		if err := rows.Scan(
			&state.AccountID,
			&state.Source,
			&state.Status,
			&state.ModelCount,
			&state.SyncBatchID,
			&lastSynced,
			&state.ErrorMessage,
			&state.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan account model sync state: %w", err)
		}
		if lastSynced.Valid {
			state.LastSyncedAt = &lastSynced.Time
		}
		out[state.AccountID] = &state
	}
	return out, rows.Err()
}

type syncStateExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertModelSyncState(ctx context.Context, exec syncStateExecutor, state *service.AccountModelSyncState) error {
	if state == nil {
		return nil
	}
	_, err := exec.ExecContext(ctx,
		`INSERT INTO account_model_sync_state (
			account_id, source, status, model_count, sync_batch_id, last_synced_at, error_message, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (account_id) DO UPDATE SET
			source = EXCLUDED.source,
			status = EXCLUDED.status,
			model_count = EXCLUDED.model_count,
			sync_batch_id = EXCLUDED.sync_batch_id,
			last_synced_at = EXCLUDED.last_synced_at,
			error_message = EXCLUDED.error_message,
			updated_at = NOW()`,
		state.AccountID,
		normalizeCatalogSource(state.Source),
		state.Status,
		state.ModelCount,
		state.SyncBatchID,
		nullableTime(state.LastSyncedAt),
		nullableString(state.ErrorMessage),
	)
	if err != nil {
		return fmt.Errorf("upsert account model sync state: %w", err)
	}
	return nil
}

func normalizeCatalogSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return service.ModelCatalogSourceUpstream
	}
	return source
}

func uniqueInt64s(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func modelCatalogPtrTime(t time.Time) *time.Time {
	return &t
}

func nullableString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func trimForStorage(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
