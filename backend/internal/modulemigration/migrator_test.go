package modulemigration

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestSub2APIOpenAIAccountDetectionHonorsExplicitProvider(t *testing.T) {
	cases := []struct {
		name string
		row  sourceRow
		want bool
	}{
		{
			name: "explicit openai provider",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"provider":      "openai",
				"api_key":       "sk-openai",
			},
			want: true,
		},
		{
			name: "explicit claude provider stays compatible",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"provider":      "claude",
				"api_key":       "sk-ant-legacy",
			},
			want: false,
		},
		{
			name: "explicit gemini provider stays compatible",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"platform":      "gemini",
				"api_key":       "AIza-legacy",
			},
			want: false,
		},
		{
			name: "implicit openai by token shape",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"api_key":       "sk-implicit",
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := accountFromRow(SourceSub2API, tc.row)
			if got := isOpenAIAccount(record, tc.row); got != tc.want {
				t.Fatalf("isOpenAIAccount() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeCompatibleAccountPreservesLegacyProvider(t *testing.T) {
	row := sourceRow{
		"__source_kind": SourceSub2API,
		"id":            "42",
		"provider":      "claude",
		"api_key":       "sk-ant-legacy",
	}
	record := accountFromRow(SourceSub2API, row)

	if ok := normalizeCompatibleAccount(&record, row); !ok {
		t.Fatal("normalizeCompatibleAccount() returned false")
	}
	if record.Platform != "anthropic" {
		t.Fatalf("Platform = %q, want anthropic", record.Platform)
	}
	if record.Type != "apikey" {
		t.Fatalf("Type = %q, want apikey", record.Type)
	}
	if record.Credentials["api_key"] != "sk-ant-legacy" {
		t.Fatalf("api_key = %q, want legacy key", record.Credentials["api_key"])
	}
	migration, ok := record.Extra["module_migration"].(map[string]any)
	if !ok {
		t.Fatalf("module_migration missing or wrong type: %#v", record.Extra["module_migration"])
	}
	if migration["compatibility_mode"] != true {
		t.Fatalf("compatibility_mode = %#v, want true", migration["compatibility_mode"])
	}
	if migration["provider_id"] != "anthropic" {
		t.Fatalf("provider_id = %#v, want anthropic", migration["provider_id"])
	}
}

func TestSub2APIProviderSpecificOAuthTypeDoesNotCollapseToGemini(t *testing.T) {
	cases := []struct {
		name         string
		row          sourceRow
		wantPlatform string
		wantType     string
		wantOpenAI   bool
	}{
		{
			name: "claude oauth type infers anthropic oauth",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "claude-oauth",
				"type":          "claude_oauth",
				"refresh_token": "claude-rt",
			},
			wantPlatform: "anthropic",
			wantType:     "oauth",
		},
		{
			name: "anthropic oauth type infers anthropic oauth",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "anthropic-oauth",
				"type":          "anthropic-oauth",
				"access_token":  "anthropic-at",
			},
			wantPlatform: "anthropic",
			wantType:     "oauth",
		},
		{
			name: "antigravity oauth type infers antigravity oauth",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "antigravity-oauth",
				"type":          "antigravity_oauth",
				"refresh_token": "ag-rt",
			},
			wantPlatform: "antigravity",
			wantType:     "oauth",
		},
		{
			name: "gemini oauth type stays gemini oauth",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "gemini-oauth",
				"type":          "gemini_oauth",
				"refresh_token": "gemini-rt",
			},
			wantPlatform: "gemini",
			wantType:     "oauth",
		},
		{
			name: "openai oauth type goes to openai module",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "openai-oauth",
				"type":          "openai_oauth",
				"refresh_token": "openai-rt",
			},
			wantOpenAI: true,
		},
		{
			name: "gemini api type becomes gemini apikey",
			row: sourceRow{
				"__source_kind": SourceSub2API,
				"id":            "gemini-api",
				"type":          "gemini",
				"api_key":       "AIza-legacy",
			},
			wantPlatform: "gemini",
			wantType:     "apikey",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := accountFromRow(SourceSub2API, tc.row)
			if got := isOpenAIAccount(record, tc.row); got != tc.wantOpenAI {
				t.Fatalf("isOpenAIAccount() = %v, want %v", got, tc.wantOpenAI)
			}
			if tc.wantOpenAI {
				normalizeOpenAIAccount(&record, tc.row)
				if record.Platform != "module" {
					t.Fatalf("Platform = %q, want module", record.Platform)
				}
				if record.Type != "oauth" {
					t.Fatalf("Type = %q, want oauth", record.Type)
				}
				return
			}
			if ok := normalizeCompatibleAccount(&record, tc.row); !ok {
				t.Fatal("normalizeCompatibleAccount() returned false")
			}
			if record.Platform != tc.wantPlatform {
				t.Fatalf("Platform = %q, want %q", record.Platform, tc.wantPlatform)
			}
			if record.Type != tc.wantType {
				t.Fatalf("Type = %q, want %q", record.Type, tc.wantType)
			}
			migration, ok := record.Extra["module_migration"].(map[string]any)
			if !ok {
				t.Fatalf("module_migration missing or wrong type: %#v", record.Extra["module_migration"])
			}
			if migration["provider_id"] != tc.wantPlatform {
				t.Fatalf("provider_id = %#v, want %q", migration["provider_id"], tc.wantPlatform)
			}
		})
	}
}

func TestNormalizeCompatibleAccountSkipsRowsWithoutProviderHint(t *testing.T) {
	row := sourceRow{
		"__source_kind": SourceSub2API,
		"id":            "oauth-without-provider",
		"type":          "oauth",
		"refresh_token": "rt-without-provider",
	}
	record := accountFromRow(SourceSub2API, row)

	if ok := normalizeCompatibleAccount(&record, row); ok {
		t.Fatalf("normalizeCompatibleAccount() = true, want false; platform=%q type=%q", record.Platform, record.Type)
	}
}

func TestSourceRowDeleted(t *testing.T) {
	cases := []struct {
		name string
		row  sourceRow
		want bool
	}{
		{name: "missing deleted_at is active", row: sourceRow{}, want: false},
		{name: "nil deleted_at is active", row: sourceRow{"deleted_at": nil}, want: false},
		{name: "empty string deleted_at is active", row: sourceRow{"deleted_at": " "}, want: false},
		{name: "timestamp string deleted_at is deleted", row: sourceRow{"deleted_at": "2026-06-08T00:00:00Z"}, want: true},
		{name: "timestamp bytes deleted_at is deleted", row: sourceRow{"deleted_at": []byte("2026-06-08T00:00:00Z")}, want: true},
		{name: "time value deleted_at is deleted", row: sourceRow{"deleted_at": 1}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceRowDeleted(tc.row); got != tc.want {
				t.Fatalf("sourceRowDeleted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkipDeletedCompatibleAccountDetectsGeminiOAuthTombstone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Migrator{
		target: db,
		opts: Options{
			SourceKind: SourceSub2API,
		},
	}
	record := accountRecord{
		LegacyID: "gemini-oauth-42",
		Platform: "gemini",
		Type:     "oauth",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*) FROM accounts
WHERE platform = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NOT NULL`)).
		WithArgs("gemini", SourceSub2API, "gemini-oauth-42").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	skip, err := m.skipDeletedCompatibleAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("skipDeletedCompatibleAccount() error = %v", err)
	}
	if !skip {
		t.Fatal("skipDeletedCompatibleAccount() = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSkipDeletedOpenAIAccountDetectsModuleTombstone(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Migrator{
		target: db,
		opts: Options{
			SourceKind: SourceLightBridge,
		},
	}
	record := accountRecord{LegacyID: "7"}

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*) FROM accounts
WHERE extra->>'provider_id' = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NOT NULL`)).
		WithArgs(openAIProviderID, SourceLightBridge, "7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	skip, err := m.skipDeletedOpenAIAccount(context.Background(), record)
	if err != nil {
		t.Fatalf("skipDeletedOpenAIAccount() error = %v", err)
	}
	if !skip {
		t.Fatal("skipDeletedOpenAIAccount() = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// TestUpsertOpenAIAccountConvertsInPlaceForSameDatabase verifies that the
// in-place upgrade auto-migration converts a legacy openai account in place
// (UPDATE the same row) instead of inserting a duplicate module account. This
// is the regression guard for upgrades producing an extra OpenAI-form copy of
// every account and re-adding accounts on each boot.
func TestUpsertOpenAIAccountConvertsInPlaceForSameDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Migrator{
		target: db,
		opts:   Options{SourceKind: SourceLightBridge, SameDatabase: true},
	}
	record := accountRecord{
		LegacyID:    "42",
		Name:        "acc",
		Type:        "oauth",
		Credentials: map[string]any{"access_token": "t"},
		Extra:       map[string]any{"provider_id": openAIProviderID},
		Concurrency: 3,
		Priority:    50,
		Status:      "active",
		Schedulable: true,
	}

	// No already-migrated module account exists yet.
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id FROM accounts
WHERE extra->>'provider_id' = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NULL
ORDER BY id ASC LIMIT 1`)).
		WithArgs(openAIProviderID, SourceLightBridge, "42").
		WillReturnError(sql.ErrNoRows)

	// Expect an in-place UPDATE targeting the source row id (42), NOT an INSERT.
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE accounts
SET name = $1, notes = NULLIF($2, ''), platform = 'module', type = $3,
    credentials = $4, extra = $5, proxy_id = COALESCE($6, proxy_id), concurrency = $7, load_factor = $8,
    priority = $9, status = $10, schedulable = $11, updated_at = NOW()
WHERE id = $12 AND platform = 'openai' AND deleted_at IS NULL`)).
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), int64(42),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := m.upsertOpenAIAccount(context.Background(), record); err != nil {
		t.Fatalf("upsertOpenAIAccount() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// TestUpsertOpenAIAccountSkipsNonNumericLegacyIDForSameDatabase verifies that a
// same-database migration skips (rather than inserts a duplicate) when the
// legacy id is not the numeric account id and therefore cannot be converted in
// place.
func TestUpsertOpenAIAccountSkipsNonNumericLegacyIDForSameDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	defer func() { _ = db.Close() }()

	m := &Migrator{
		target: db,
		opts:   Options{SourceKind: SourceLightBridge, SameDatabase: true},
	}
	record := accountRecord{
		LegacyID:    "user@example.com",
		Name:        "acc",
		Type:        "oauth",
		Credentials: map[string]any{"access_token": "t"},
		Extra:       map[string]any{"provider_id": openAIProviderID},
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id FROM accounts
WHERE extra->>'provider_id' = $1
  AND extra->'module_migration'->>'source' = $2
  AND extra->'module_migration'->>'legacy_account_id' = $3::text
  AND deleted_at IS NULL
ORDER BY id ASC LIMIT 1`)).
		WithArgs(openAIProviderID, SourceLightBridge, "user@example.com").
		WillReturnError(sql.ErrNoRows)
	// No UPDATE and no INSERT expected.

	if err := m.upsertOpenAIAccount(context.Background(), record); err != nil {
		t.Fatalf("upsertOpenAIAccount() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
