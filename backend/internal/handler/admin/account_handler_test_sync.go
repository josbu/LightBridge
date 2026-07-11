package admin

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

// TestAccountRequest represents the request body for testing an account
type TestAccountRequest struct {
	ModelID string `json:"model_id"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
}

type SyncFromCRSRequest struct {
	BaseURL            string   `json:"base_url" binding:"required"`
	Username           string   `json:"username" binding:"required"`
	Password           string   `json:"password" binding:"required"`
	SyncProxies        *bool    `json:"sync_proxies"`
	SelectedAccountIDs []string `json:"selected_account_ids"`
}

type PreviewFromCRSRequest struct {
	BaseURL  string `json:"base_url" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Test handles testing account connectivity with SSE streaming
// POST /api/v1/admin/accounts/:id/test
func (h *AccountHandler) Test(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req TestAccountRequest
	// Allow empty body, model_id is optional
	_ = c.ShouldBindJSON(&req)

	// Use AccountTestService to test the account with SSE streaming
	if err := h.accountTestService.TestAccountConnection(c, accountID, req.ModelID, req.Prompt, req.Mode); err != nil {
		// Error already sent via SSE, just log
		return
	}

	if h.rateLimitService != nil {
		if _, err := h.rateLimitService.RecoverAccountAfterSuccessfulTest(c.Request.Context(), accountID); err != nil {
			_ = c.Error(err)
		}
	}
}

// RecoverState handles unified recovery of recoverable account runtime state.
// POST /api/v1/admin/accounts/:id/recover-state
func (h *AccountHandler) RecoverState(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if h.rateLimitService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Rate limit service unavailable")
		return
	}

	if _, err := h.rateLimitService.RecoverAccountState(c.Request.Context(), accountID, service.AccountRecoveryOptions{
		InvalidateToken: true,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// VerifyAuthenticity 对一个 Claude/Anthropic 账号执行主动真伪探针。
// POST /api/v1/admin/accounts/:id/verify-authenticity
//
// 原理：构造一个带伪造 thinking signature 的多轮请求发给该账号——
// 真 Claude 会校验签名并返回 400（genuine），套壳/中转假冒会忽略签名并返回 2xx（counterfeit）。
// 探针 max_tokens=1 + 伪造签名（多数在计费前 400），单次成本趋零。
// 结论写回 Account.Extra，并返回刷新后的账号。
func (h *AccountHandler) VerifyAuthenticity(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	if h.accountTestService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account test service unavailable")
		return
	}

	result, err := h.accountTestService.ProbeClaudeAuthenticity(c, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// key 级增量合并进 Extra，不影响其它运行态键。
	if err := h.adminService.UpdateAccountExtra(c.Request.Context(), accountID, result.ExtraMap()); err != nil {
		// 持久化失败不阻断返回结论，但仍上报错误。
		_ = c.Error(err)
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		// 探针已成功；读取最新账号失败时直接返回结论。
		response.Success(c, gin.H{
			"account": nil,
			"result":  result,
		})
		return
	}

	response.Success(c, gin.H{
		"account": h.buildAccountResponseWithRuntime(c.Request.Context(), account),
		"result":  result,
	})
}

// SyncFromCRS handles syncing accounts from claude-relay-service (CRS)
// POST /api/v1/admin/accounts/sync/crs
func (h *AccountHandler) SyncFromCRS(c *gin.Context) {
	var req SyncFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Default to syncing proxies (can be disabled by explicitly setting false)
	syncProxies := true
	if req.SyncProxies != nil {
		syncProxies = *req.SyncProxies
	}

	result, err := h.crsSyncService.SyncFromCRS(c.Request.Context(), service.SyncFromCRSInput{
		BaseURL:            req.BaseURL,
		Username:           req.Username,
		Password:           req.Password,
		SyncProxies:        syncProxies,
		SelectedAccountIDs: req.SelectedAccountIDs,
	})
	if err != nil {
		// Provide detailed error message for CRS sync failures
		response.InternalError(c, "CRS sync failed: "+err.Error())
		return
	}

	response.Success(c, result)
}

// PreviewFromCRS handles previewing accounts from CRS before sync
// POST /api/v1/admin/accounts/sync/crs/preview
func (h *AccountHandler) PreviewFromCRS(c *gin.Context) {
	var req PreviewFromCRSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.crsSyncService.PreviewFromCRS(c.Request.Context(), service.SyncFromCRSInput{
		BaseURL:  req.BaseURL,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		response.InternalError(c, "CRS preview failed: "+err.Error())
		return
	}

	response.Success(c, result)
}

// refreshSingleAccount refreshes credentials for a single OAuth account.
// Returns (updatedAccount, warning, error) where warning is used for Antigravity ProjectIDMissing scenario.
func (h *AccountHandler) refreshSingleAccount(ctx context.Context, account *service.Account) (*service.Account, string, error) {
	if !account.IsOAuth() {
		return nil, "", infraerrors.BadRequest("NOT_OAUTH", "cannot refresh non-OAuth account")
	}

	var newCredentials map[string]any

	if refreshed, handled, err := h.adminService.RefreshModuleProviderAccount(ctx, account); handled {
		if err != nil {
			return nil, "", err
		}
		return refreshed, "", nil
	} else if account.IsOpenAI() {
		tokenInfo, err := h.openaiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			// 刷新失败但 access_token 可能仍有效，尝试设置隐私
			h.adminService.EnsureOpenAIPrivacy(ctx, account)
			return nil, "", err
		}

		newCredentials = h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
	} else if account.IsPureGemini() {
		tokenInfo, err := h.geminiOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh credentials: %w", err)
		}

		newCredentials = h.geminiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
	} else if account.IsAntigravity() {
		tokenInfo, err := h.antigravityOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", err
		}

		newCredentials = h.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}

		// 特殊处理 project_id：如果新值为空但旧值非空，保留旧值
		// 这确保了即使 LoadCodeAssist 失败，project_id 也不会丢失
		if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
			if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
				newCredentials["project_id"] = oldProjectID
			}
		}

		// 如果 project_id 获取失败，更新凭证但不标记为 error
		if tokenInfo.ProjectIDMissing {
			updatedAccount, updateErr := h.adminService.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
				Credentials: newCredentials,
			})
			if updateErr != nil {
				return nil, "", fmt.Errorf("failed to update credentials: %w", updateErr)
			}
			h.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)
			return updatedAccount, "missing_project_id_temporary", nil
		}

		// 成功获取到 project_id，如果之前是 missing_project_id 错误则清除
		if account.Status == service.StatusError && strings.Contains(account.ErrorMessage, "missing_project_id:") {
			if _, clearErr := h.adminService.ClearAccountError(ctx, account.ID); clearErr != nil {
				return nil, "", fmt.Errorf("failed to clear account error: %w", clearErr)
			}
		}
	} else {
		return nil, "", infraerrors.BadRequest("UNSUPPORTED_OAUTH_REFRESH", "unsupported OAuth account refresh provider")
	}

	updatedAccount, err := h.adminService.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		return nil, "", err
	}

	// 刷新成功后，清除 token 缓存，确保下次请求使用新 token
	if h.tokenCacheInvalidator != nil {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(ctx, updatedAccount); invalidateErr != nil {
			log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", updatedAccount.ID, invalidateErr)
		}
	}

	// OpenAI OAuth: 刷新成功后检查并设置 privacy_mode
	h.adminService.EnsureOpenAIPrivacy(ctx, updatedAccount)
	// Antigravity OAuth: 刷新成功后检查并设置 privacy_mode
	h.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)

	return updatedAccount, "", nil
}

// Refresh handles refreshing account credentials
// POST /api/v1/admin/accounts/:id/refresh
func (h *AccountHandler) Refresh(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Get account
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}

	updatedAccount, warning, err := h.refreshSingleAccount(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if warning == "missing_project_id_temporary" {
		response.Success(c, gin.H{
			"message": "Token refreshed successfully, but project_id could not be retrieved (will retry automatically)",
			"warning": "missing_project_id_temporary",
		})
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), updatedAccount))
}

// ApplyOAuthCredentialsRequest is the payload for persisting re-authorized OAuth credentials.
type ApplyOAuthCredentialsRequest struct {
	Type        string         `json:"type" binding:"required,oneof=oauth setup-token"`
	Credentials map[string]any `json:"credentials" binding:"required"`
	Extra       map[string]any `json:"extra"`
}

// ApplyOAuthCredentials 将"重新授权"得到的新凭据原子落库。
// POST /api/v1/admin/accounts/:id/apply-oauth-credentials
//
// 与通用 PUT /:id (Update) 接口的关键区别：
//   - 仅接收 type / credentials / extra 三个字段（不接受 concurrency / rpm / quota_* 等可能误传的字段）
//   - Extra 走 UpdateAccountExtra(JSONB key 级合并)，**绝不**全量覆盖；
//     避免 base_rpm / window_cost_limit / max_sessions / quota_* / privacy_mode
//     等持久化配置在重新授权后丢失
//   - 内置 ClearError + InvalidateToken，避免前端额外两次调用，
//     并修复旧路径未失效 token 缓存导致重新授权后立即 401 的隐性 bug
//
// 与 /refresh 的区别：/refresh 用现有 refresh_token 换 access_token（无用户交互），
// 本接口承接前端完成完整 OAuth 流程后的落库步骤。
func (h *AccountHandler) ApplyOAuthCredentials(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req ApplyOAuthCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()

	// 预检查账号存在 + OAuth 类型（与 Refresh handler 语义一致，提供更友好的错误信息）。
	existing, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if !existing.IsOAuth() {
		response.ErrorFrom(c, infraerrors.BadRequest("NOT_OAUTH", "cannot apply oauth credentials to non-OAuth account"))
		return
	}

	updatedAccount, err := h.adminService.UpdateAccount(ctx, accountID, &service.UpdateAccountInput{
		Type:        req.Type,
		Credentials: req.Credentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 增量合并 Extra（JSONB key 级 merge，绝不覆盖 base_rpm / window_cost_limit /
	// max_sessions / quota_* / privacy_mode 等持久化键）。
	// best-effort：失败仅记日志；下方 ClearAccountError 会从 DB 重新读取最新 account，
	// 因此响应里的 extra 始终以 DB 为准——这里不需要手动维护内存快照。
	if len(req.Extra) > 0 {
		if extraErr := h.adminService.UpdateAccountExtra(ctx, accountID, req.Extra); extraErr != nil {
			extraKeys := make([]string, 0, len(req.Extra))
			for k := range req.Extra {
				extraKeys = append(extraKeys, k)
			}
			slog.Error("apply_oauth_credentials.update_extra_failed",
				"account_id", accountID,
				"extra_keys", extraKeys,
				"err", extraErr,
			)
		} else {
			mergedExtra := make(map[string]any, len(updatedAccount.Extra)+len(req.Extra))
			for k, v := range updatedAccount.Extra {
				mergedExtra[k] = v
			}
			for k, v := range req.Extra {
				mergedExtra[k] = v
			}
			updatedAccount.Extra = mergedExtra
		}
	}

	if refreshed, handled, refreshErr := h.adminService.RefreshModuleProviderAccount(ctx, updatedAccount); handled {
		if refreshErr != nil {
			response.ErrorFrom(c, refreshErr)
			return
		}
		if refreshed != nil {
			updatedAccount = refreshed
		}
	}

	if cleared, clearErr := h.adminService.ClearAccountError(ctx, accountID); clearErr != nil {
		slog.Warn("apply_oauth_credentials.clear_error_failed",
			"account_id", accountID,
			"err", clearErr,
		)
	} else if cleared != nil {
		updatedAccount = cleared
	}

	if h.tokenCacheInvalidator != nil && updatedAccount.IsOAuth() {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(ctx, updatedAccount); invalidateErr != nil {
			slog.Warn("apply_oauth_credentials.invalidate_token_failed",
				"account_id", accountID,
				"err", invalidateErr,
			)
		}
	}

	h.scheduleOAuthModelSync(updatedAccount)
	response.Success(c, h.buildAccountResponseWithRuntime(ctx, updatedAccount))
}
