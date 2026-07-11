package admin

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/WilliamWang1721/LightBridge/internal/domain"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/antigravity"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/claude"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/geminicli"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/openai"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

// GetAvailableModels handles getting available models for an account
// GET /api/v1/admin/accounts/:id/models
func (h *AccountHandler) GetAvailableModels(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if h.modelCatalogService != nil {
		if modelIDs, _, catalogErr := h.modelCatalogService.ListAccountModels(c.Request.Context(), account); catalogErr == nil && len(modelIDs) > 0 {
			response.Success(c, genericModelsFromIDs(modelIDs))
			return
		}
	}

	// Handle OpenAI accounts
	if account.IsOpenAI() {
		// OpenAI 自动透传会绕过常规模型改写：本地的默认模型列表对透传账号没有意义
		// （上游可能是任意第三方端点），应改为从上游实时拉取真实支持的模型列表。
		// 上游拉取失败时回落默认列表，保证测试连接功能不至于完全不可用。
		if account.IsOpenAIPassthroughEnabled() {
			if models, ok := h.upstreamModelsOrNil(c, account); ok {
				response.Success(c, models)
				return
			}
			response.Success(c, openai.DefaultModels)
			return
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			response.Success(c, openai.DefaultModels)
			return
		}

		// Return mapped models
		var models []openai.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range openai.DefaultModels {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, openai.Model{
					ID:          requestedModel,
					Object:      "model",
					Type:        "model",
					DisplayName: requestedModel,
				})
			}
		}
		response.Success(c, models)
		return
	}

	if account.IsGrok() {
		response.Success(c, xai.DefaultModels())
		return
	}

	// Handle Gemini accounts（仅原生 Gemini；Antigravity 在下方独立分支处理）
	if account.IsPureGemini() {
		// For OAuth accounts: return default Gemini models
		if account.IsOAuth() {
			response.Success(c, geminicli.DefaultModels)
			return
		}

		// For API Key accounts: return models based on model_mapping
		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			response.Success(c, geminicli.DefaultModels)
			return
		}

		var models []geminicli.Model
		for requestedModel := range mapping {
			var found bool
			for _, dm := range geminicli.DefaultModels {
				if dm.ID == requestedModel {
					models = append(models, dm)
					found = true
					break
				}
			}
			if !found {
				models = append(models, geminicli.Model{
					ID:          requestedModel,
					Type:        "model",
					DisplayName: requestedModel,
					CreatedAt:   "",
				})
			}
		}
		response.Success(c, models)
		return
	}

	// Handle Antigravity accounts: return Claude + Gemini models
	if account.IsAntigravity() {
		// 直接复用 antigravity.DefaultModels()，与 /v1/models 端点保持同步
		response.Success(c, antigravity.DefaultModels())
		return
	}

	// Handle Claude/Anthropic accounts
	// For OAuth and Setup-Token accounts: return default models
	if account.IsOAuth() {
		response.Success(c, claude.DefaultModels)
		return
	}

	// For API Key accounts: return models based on model_mapping
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		// No mapping configured, return default models
		response.Success(c, claude.DefaultModels)
		return
	}

	// Return mapped models (keys of the mapping are the available model IDs)
	var models []claude.Model
	for requestedModel := range mapping {
		// Try to find display info from default models
		var found bool
		for _, dm := range claude.DefaultModels {
			if dm.ID == requestedModel {
				models = append(models, dm)
				found = true
				break
			}
		}
		// If not found in defaults, create a basic entry
		if !found {
			models = append(models, claude.Model{
				ID:          requestedModel,
				Type:        "model",
				DisplayName: requestedModel,
				CreatedAt:   "",
			})
		}
	}

	response.Success(c, models)
}

// SyncUpstreamModels handles syncing live supported models from an account's upstream.
// POST /api/v1/admin/accounts/:id/models/sync-upstream
func (h *AccountHandler) SyncUpstreamModels(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if h.accountTestService == nil {
		response.InternalError(c, "Account test service is not configured")
		return
	}

	models, err := h.accountTestService.FetchUpstreamSupportedModels(c.Request.Context(), account)
	if err != nil {
		if h.modelCatalogService != nil {
			h.modelCatalogService.RecordSyncFailure(c.Request.Context(), account, service.ModelCatalogSourceUpstream, err)
		}
		var syncErr *service.UpstreamModelSyncError
		if errors.As(err, &syncErr) {
			switch syncErr.Kind {
			case service.UpstreamModelSyncErrorConfiguration, service.UpstreamModelSyncErrorUnsupported:
				response.BadRequest(c, syncErr.SafeMessage())
			default:
				slog.Warn("sync_upstream_models_failed", "account_id", accountID, "kind", syncErr.Kind)
				response.Error(c, http.StatusBadGateway, syncErr.SafeMessage())
			}
			return
		}

		slog.Warn("sync_upstream_models_failed", "account_id", accountID)
		response.Error(c, http.StatusBadGateway, "Failed to sync upstream models from upstream")
		return
	}

	var syncState *service.AccountModelSyncState
	if h.modelCatalogService != nil {
		state, normalized, saveErr := h.modelCatalogService.ReplaceAccountModelsFromSync(
			c.Request.Context(),
			account,
			models,
			service.ModelCatalogSourceUpstream,
		)
		if saveErr != nil {
			response.ErrorFrom(c, saveErr)
			return
		}
		syncState = state
		models = normalized
	}

	response.Success(c, gin.H{"models": models, "sync_state": syncState})
}

func genericModelsFromIDs(modelIDs []string) []gin.H {
	models := make([]gin.H, 0, len(modelIDs))
	for _, id := range modelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, gin.H{
			"id":           id,
			"type":         "model",
			"object":       "model",
			"display_name": id,
		})
	}
	return models
}

// upstreamModelsOrNil 尝试从 OpenAI 透传账号的上游实时拉取真实支持的模型列表，
// 并转换为与默认列表一致的 []openai.Model。仅当 accountTestService 已配置、上游
// 拉取成功且返回非空时返回 (models, true)；否则返回 (nil, false)，由调用方回落到
// openai.DefaultModels，保证上游暂时不可用时仍能展示 / 测试连接。
func (h *AccountHandler) upstreamModelsOrNil(c *gin.Context, account *service.Account) ([]openai.Model, bool) {
	if h.accountTestService == nil {
		return nil, false
	}
	ids, err := h.accountTestService.FetchUpstreamSupportedModels(c.Request.Context(), account)
	if err != nil || len(ids) == 0 {
		return nil, false
	}
	models := make([]openai.Model, 0, len(ids))
	for _, id := range ids {
		models = append(models, openai.Model{
			ID:          id,
			Object:      "model",
			Type:        "model",
			DisplayName: id,
		})
	}
	return models, true
}

// SetPrivacy handles setting privacy for a single OpenAI/Antigravity OAuth account
// POST /api/v1/admin/accounts/:id/set-privacy
func (h *AccountHandler) SetPrivacy(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Only OAuth accounts support privacy setting")
		return
	}
	var mode string
	switch account.EffectivePlatform() {
	case service.PlatformOpenAI:
		mode = h.adminService.ForceOpenAIPrivacy(c.Request.Context(), account)
	case service.PlatformAntigravity:
		mode = h.adminService.ForceAntigravityPrivacy(c.Request.Context(), account)
	default:
		response.BadRequest(c, "Only OpenAI and Antigravity OAuth accounts support privacy setting")
		return
	}
	if mode == "" {
		response.BadRequest(c, "Cannot set privacy: missing access_token")
		return
	}
	// 从 DB 重新读取以确保返回最新状态
	updated, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		// 隐私已设置成功但读取失败，回退到内存更新
		if account.Extra == nil {
			account.Extra = make(map[string]any)
		}
		account.Extra["privacy_mode"] = mode
		response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updated))
}

// RefreshTier handles refreshing Google One tier for a single account
// POST /api/v1/admin/accounts/:id/refresh-tier
func (h *AccountHandler) RefreshTier(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	ctx := c.Request.Context()
	account, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	if !account.IsPureGemini() || account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "Only Gemini OAuth accounts support tier refresh")
		return
	}

	oauthType, _ := account.Credentials["oauth_type"].(string)
	if oauthType != "google_one" {
		response.BadRequest(c, "Only google_one OAuth accounts support tier refresh")
		return
	}

	tierID, extra, creds, err := h.geminiOAuthService.RefreshAccountGoogleOneTier(ctx, account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	_, updateErr := h.adminService.UpdateAccount(ctx, accountID, &service.UpdateAccountInput{
		Credentials: creds,
		Extra:       extra,
	})
	if updateErr != nil {
		response.ErrorFrom(c, updateErr)
		return
	}

	response.Success(c, gin.H{
		"tier_id":             tierID,
		"storage_info":        extra,
		"drive_storage_limit": extra["drive_storage_limit"],
		"drive_storage_usage": extra["drive_storage_usage"],
		"updated_at":          extra["drive_tier_updated_at"],
	})
}

// BatchRefreshTierRequest represents batch tier refresh request
type BatchRefreshTierRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

// BatchRefreshTier handles batch refreshing Google One tier
// POST /api/v1/admin/accounts/batch-refresh-tier
func (h *AccountHandler) BatchRefreshTier(c *gin.Context) {
	var req BatchRefreshTierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = BatchRefreshTierRequest{}
	}

	ctx := c.Request.Context()
	accounts := make([]*service.Account, 0)

	if len(req.AccountIDs) == 0 {
		allAccounts, _, err := h.adminService.ListAccounts(ctx, 1, 10000, "gemini", "oauth", "", "", 0, "", "name", "asc")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		for i := range allAccounts {
			acc := &allAccounts[i]
			oauthType, _ := acc.Credentials["oauth_type"].(string)
			if oauthType == "google_one" {
				accounts = append(accounts, acc)
			}
		}
	} else {
		fetched, err := h.adminService.GetAccountsByIDs(ctx, req.AccountIDs)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		for _, acc := range fetched {
			if acc == nil {
				continue
			}
			if !acc.IsPureGemini() || acc.Type != service.AccountTypeOAuth {
				continue
			}
			oauthType, _ := acc.Credentials["oauth_type"].(string)
			if oauthType != "google_one" {
				continue
			}
			accounts = append(accounts, acc)
		}
	}

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H

	for _, account := range accounts {
		acc := account // 闭包捕获
		g.Go(func() error {
			_, extra, creds, err := h.geminiOAuthService.RefreshAccountGoogleOneTier(gctx, acc)
			if err != nil {
				mu.Lock()
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      err.Error(),
				})
				mu.Unlock()
				return nil
			}

			_, updateErr := h.adminService.UpdateAccount(gctx, acc.ID, &service.UpdateAccountInput{
				Credentials: creds,
				Extra:       extra,
			})

			mu.Lock()
			if updateErr != nil {
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      updateErr.Error(),
				})
			} else {
				successCount++
			}
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	results := gin.H{
		"total":   len(accounts),
		"success": successCount,
		"failed":  failedCount,
		"errors":  errors,
	}

	response.Success(c, results)
}

// GetAntigravityDefaultModelMapping 获取 Antigravity 平台的默认模型映射
// GET /api/v1/admin/accounts/antigravity/default-model-mapping
func (h *AccountHandler) GetAntigravityDefaultModelMapping(c *gin.Context) {
	response.Success(c, domain.DefaultAntigravityModelMapping)
}

// sanitizeExtraBaseRPM 对 extra map 中的 base_rpm 值进行范围校验和归一化。
// 负值归零，超过 10000 截断为 10000。extra 为 nil 或不含 base_rpm 时无操作。
func sanitizeExtraBaseRPM(extra map[string]any) {
	if extra == nil {
		return
	}
	raw, ok := extra["base_rpm"]
	if !ok {
		return
	}
	v := service.ParseExtraInt(raw)
	if v < 0 {
		v = 0
	} else if v > 10000 {
		v = 10000
	}
	extra["base_rpm"] = v
}
