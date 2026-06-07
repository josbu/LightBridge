package modulemigration

import (
	"testing"
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
