package service

import (
	"context"
	"errors"
	"testing"
)

type repairOpenAIOAuthAccountRepo struct {
	AccountRepository
	accounts  []Account
	updateErr map[int64]error
	updated   []Account
}

func (r *repairOpenAIOAuthAccountRepo) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func (r *repairOpenAIOAuthAccountRepo) Update(_ context.Context, account *Account) error {
	if err, ok := r.updateErr[account.ID]; ok {
		return err
	}
	r.updated = append(r.updated, *account)
	for i := range r.accounts {
		if r.accounts[i].ID == account.ID {
			r.accounts[i] = *account
			break
		}
	}
	return nil
}

func TestAdminService_RepairMisclassifiedOpenAIOAuthAccounts(t *testing.T) {
	repo := &repairOpenAIOAuthAccountRepo{
		accounts: []Account{
			{
				ID:          1,
				Name:        "corrupted openai oauth",
				Platform:    PlatformGemini,
				SubPlatform: SubPlatformAntigravity,
				Type:        AccountTypeOAuth,
				Credentials: map[string]any{
					"refresh_token":      "openai-rt",
					"access_token":       "openai-at",
					"chatgpt_account_id": "chatgpt-account",
				},
				Extra: map[string]any{"keep": "value"},
			},
			{
				ID:       2,
				Name:     "real gemini oauth",
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"refresh_token": "gemini-rt",
					"oauth_type":    "code_assist",
					"project_id":    "project-123",
					"plan_type":     "Pro",
				},
			},
			{
				ID:       3,
				Name:     "gemini api key",
				Platform: PlatformGemini,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"chatgpt_account_id": "ignored-because-not-oauth",
				},
			},
			{
				ID:          4,
				Name:        "antigravity oauth",
				Platform:    PlatformGemini,
				SubPlatform: SubPlatformAntigravity,
				Type:        AccountTypeOAuth,
				Credentials: map[string]any{
					"refresh_token": "antigravity-rt",
					"oauth_type":    "code_assist",
				},
			},
			{
				ID:       5,
				Name:     "corrupted openai oauth split metadata",
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"refresh_token": "openai-rt-2",
					"access_token":  "openai-at-2",
				},
				Extra: map[string]any{
					"plan_type": "K12",
				},
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.RepairMisclassifiedOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("RepairMisclassifiedOpenAIOAuthAccounts returned error: %v", err)
	}

	if result.Scanned != 4 || result.Candidates != 2 || result.Repaired != 2 || result.Skipped != 2 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.RepairedIDs) != 2 || result.RepairedIDs[0] != 1 || result.RepairedIDs[1] != 5 {
		t.Fatalf("unexpected repaired ids: %#v", result.RepairedIDs)
	}
	if len(repo.updated) != 2 {
		t.Fatalf("expected two updated accounts, got %d", len(repo.updated))
	}

	updated := repo.updated[0]
	if updated.Platform != PlatformOpenAI || updated.SubPlatform != "" || updated.Type != AccountTypeOAuth {
		t.Fatalf("account was not repaired to OpenAI OAuth: %+v", updated)
	}
	if updated.Extra["keep"] != "value" {
		t.Fatalf("existing extra was not preserved: %#v", updated.Extra)
	}
	repairAudit, ok := updated.Extra[accountExtraKeyOpenAIOAuthPlatformRepair].(map[string]any)
	if !ok {
		t.Fatalf("repair audit was not recorded: %#v", updated.Extra)
	}
	if repairAudit["previous_platform"] != PlatformGemini || repairAudit["previous_sub_platform"] != SubPlatformAntigravity {
		t.Fatalf("unexpected repair audit: %#v", repairAudit)
	}

	splitMetadataUpdated := repo.updated[1]
	if splitMetadataUpdated.Platform != PlatformOpenAI || splitMetadataUpdated.Extra["plan_type"] != "K12" {
		t.Fatalf("split metadata account was not repaired correctly: %+v", splitMetadataUpdated)
	}
}

func TestAdminService_RepairMisclassifiedOpenAIOAuthAccountsCountsFailures(t *testing.T) {
	repo := &repairOpenAIOAuthAccountRepo{
		accounts: []Account{
			{
				ID:       7,
				Name:     "update fails",
				Platform: PlatformGemini,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"session_token": "openai-session",
					"plan_type":     "team",
					"access_token":  "openai-at",
				},
			},
		},
		updateErr: map[int64]error{7: errors.New("db down")},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.RepairMisclassifiedOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("RepairMisclassifiedOpenAIOAuthAccounts returned error: %v", err)
	}

	if result.Scanned != 1 || result.Candidates != 1 || result.Repaired != 0 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Items) != 1 || result.Items[0].Action != "failed" || result.Items[0].Error != "db down" {
		t.Fatalf("unexpected item result: %#v", result.Items)
	}
}

func TestAdminService_RepairMisclassifiedOpenAIOAuthAccountsIsIdempotent(t *testing.T) {
	repo := &repairOpenAIOAuthAccountRepo{
		accounts: []Account{
			{
				ID:       11,
				Name:     "already openai",
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"chatgpt_account_id": "chatgpt-account",
				},
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.RepairMisclassifiedOpenAIOAuthAccounts(context.Background())
	if err != nil {
		t.Fatalf("RepairMisclassifiedOpenAIOAuthAccounts returned error: %v", err)
	}

	if result.Scanned != 0 || result.Candidates != 0 || result.Repaired != 0 || len(repo.updated) != 0 {
		t.Fatalf("unexpected idempotent result: %+v updated=%#v", result, repo.updated)
	}
}
