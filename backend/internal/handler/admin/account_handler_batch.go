package admin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strconv"
	"sync"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/timezone"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

// GetStats handles getting account statistics
// GET /api/v1/admin/accounts/:id/stats
func (h *AccountHandler) GetStats(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Parse days parameter (default 30)
	days := 30
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}

	// Calculate time range
	now := timezone.Now()
	endTime := timezone.StartOfDay(now.AddDate(0, 0, 1))
	startTime := timezone.StartOfDay(now.AddDate(0, 0, -days+1))

	stats, err := h.accountUsageService.GetAccountUsageStats(c.Request.Context(), accountID, startTime, endTime)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, stats)
}

// ClearError handles clearing account error
// POST /api/v1/admin/accounts/:id/clear-error
func (h *AccountHandler) ClearError(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.ClearAccountError(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 清除错误后，同时清除 token 缓存，确保下次请求会获取最新的 token（触发刷新或从 DB 读取）
	// 这解决了管理员重置账号状态后，旧的失效 token 仍在缓存中导致立即再次 401 的问题
	if h.tokenCacheInvalidator != nil && account.IsOAuth() {
		if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(c.Request.Context(), account); invalidateErr != nil {
			log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", accountID, invalidateErr)
		}
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// BatchClearError handles batch clearing account errors
// POST /api/v1/admin/accounts/batch-clear-error
func (h *AccountHandler) BatchClearError(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}

	ctx := c.Request.Context()

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H

	// 注意：所有 goroutine 必须 return nil，避免 errgroup cancel 其他并发任务
	for _, id := range req.AccountIDs {
		accountID := id // 闭包捕获
		g.Go(func() error {
			account, err := h.adminService.ClearAccountError(gctx, accountID)
			if err != nil {
				mu.Lock()
				failedCount++
				errors = append(errors, gin.H{
					"account_id": accountID,
					"error":      err.Error(),
				})
				mu.Unlock()
				return nil
			}

			// 清除错误后，同时清除 token 缓存
			if h.tokenCacheInvalidator != nil && account.IsOAuth() {
				if invalidateErr := h.tokenCacheInvalidator.InvalidateToken(gctx, account); invalidateErr != nil {
					log.Printf("[WARN] Failed to invalidate token cache for account %d: %v", accountID, invalidateErr)
				}
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total":   len(req.AccountIDs),
		"success": successCount,
		"failed":  failedCount,
		"errors":  errors,
	})
}

// BatchRefresh handles batch refreshing account credentials
// POST /api/v1/admin/accounts/batch-refresh
func (h *AccountHandler) BatchRefresh(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}

	ctx := c.Request.Context()

	accounts, err := h.adminService.GetAccountsByIDs(ctx, req.AccountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 建立已获取账号的 ID 集合，检测缺失的 ID
	foundIDs := make(map[int64]bool, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			foundIDs[acc.ID] = true
		}
	}

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	var mu sync.Mutex
	var successCount, failedCount int
	var errors []gin.H
	var warnings []gin.H

	// 将不存在的账号 ID 标记为失败
	for _, id := range req.AccountIDs {
		if !foundIDs[id] {
			failedCount++
			errors = append(errors, gin.H{
				"account_id": id,
				"error":      "account not found",
			})
		}
	}

	// 注意：所有 goroutine 必须 return nil，避免 errgroup cancel 其他并发任务
	for _, account := range accounts {
		acc := account // 闭包捕获
		if acc == nil {
			continue
		}
		g.Go(func() error {
			_, warning, err := h.refreshSingleAccount(gctx, acc)
			mu.Lock()
			if err != nil {
				failedCount++
				errors = append(errors, gin.H{
					"account_id": acc.ID,
					"error":      err.Error(),
				})
			} else {
				successCount++
				if warning != "" {
					warnings = append(warnings, gin.H{
						"account_id": acc.ID,
						"warning":    warning,
					})
				}
			}
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total":    len(req.AccountIDs),
		"success":  successCount,
		"failed":   failedCount,
		"errors":   errors,
		"warnings": warnings,
	})
}

// BatchCreate handles batch creating accounts
// POST /api/v1/admin/accounts/batch
func (h *AccountHandler) BatchCreate(c *gin.Context) {
	var req struct {
		Accounts []CreateAccountRequest `json:"accounts" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.accounts.batch_create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		success := 0
		failed := 0
		results := make([]gin.H, 0, len(req.Accounts))
		// 收集需要异步设置隐私的 OAuth 账号
		var antigravityPrivacyAccounts []*service.Account
		var openaiPrivacyAccounts []*service.Account

		for _, item := range req.Accounts {
			if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
				failed++
				results = append(results, gin.H{
					"name":    item.Name,
					"success": false,
					"error":   "rate_multiplier must be >= 0",
				})
				continue
			}

			// base_rpm 输入校验：负值归零，超过 10000 截断
			sanitizeExtraBaseRPM(item.Extra)

			skipCheck := item.ConfirmMixedChannelRisk != nil && *item.ConfirmMixedChannelRisk

			account, err := h.adminService.CreateAccount(ctx, &service.CreateAccountInput{
				Name:                  item.Name,
				Notes:                 item.Notes,
				Platform:              item.Platform,
				Type:                  item.Type,
				Credentials:           item.Credentials,
				Extra:                 item.Extra,
				ProxyID:               item.ProxyID,
				Concurrency:           item.Concurrency,
				Priority:              item.Priority,
				RateMultiplier:        item.RateMultiplier,
				GroupIDs:              item.GroupIDs,
				ExpiresAt:             item.ExpiresAt,
				AutoPauseOnExpired:    item.AutoPauseOnExpired,
				SkipMixedChannelCheck: skipCheck,
			})
			if err != nil {
				failed++
				results = append(results, gin.H{
					"name":    item.Name,
					"success": false,
					"error":   err.Error(),
				})
				continue
			}
			// 收集需要异步设置隐私的 OAuth 账号
			if account.Type == service.AccountTypeOAuth {
				// 使用 EffectivePlatform：Antigravity 账号现 Platform=="gemini"，
				// 需按别名路由到 antigravity 隐私设置分支。
				switch account.EffectivePlatform() {
				case service.PlatformAntigravity:
					antigravityPrivacyAccounts = append(antigravityPrivacyAccounts, account)
				case service.PlatformOpenAI:
					openaiPrivacyAccounts = append(openaiPrivacyAccounts, account)
				}
			}
			// OpenAI APIKey 账号异步探测 /v1/responses 能力。
			h.scheduleOpenAIResponsesProbe(account)
			success++
			results = append(results, gin.H{
				"name":    item.Name,
				"id":      account.ID,
				"success": true,
			})
		}

		// 异步设置隐私，避免批量创建时阻塞请求
		adminSvc := h.adminService
		if len(antigravityPrivacyAccounts) > 0 {
			accounts := antigravityPrivacyAccounts
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("batch_create_antigravity_privacy_panic", "recover", r)
					}
				}()
				bgCtx := context.Background()
				for _, acc := range accounts {
					adminSvc.ForceAntigravityPrivacy(bgCtx, acc)
				}
			}()
		}
		if len(openaiPrivacyAccounts) > 0 {
			accounts := openaiPrivacyAccounts
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("batch_create_openai_privacy_panic", "recover", r)
					}
				}()
				bgCtx := context.Background()
				for _, acc := range accounts {
					adminSvc.ForceOpenAIPrivacy(bgCtx, acc)
				}
			}()
		}

		return gin.H{
			"success": success,
			"failed":  failed,
			"results": results,
		}, nil
	})
}

// BatchUpdateCredentialsRequest represents batch credentials update request
type BatchUpdateCredentialsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required,min=1"`
	Field      string  `json:"field" binding:"required,oneof=account_uuid org_uuid intercept_warmup_requests"`
	Value      any     `json:"value"`
}

// BatchUpdateCredentials handles batch updating credentials fields
// POST /api/v1/admin/accounts/batch-update-credentials
func (h *AccountHandler) BatchUpdateCredentials(c *gin.Context) {
	var req BatchUpdateCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Validate value type based on field
	if req.Field == "intercept_warmup_requests" {
		// Must be boolean
		if _, ok := req.Value.(bool); !ok {
			response.BadRequest(c, "intercept_warmup_requests must be boolean")
			return
		}
	} else {
		// account_uuid and org_uuid can be string or null
		if req.Value != nil {
			if _, ok := req.Value.(string); !ok {
				response.BadRequest(c, req.Field+" must be string or null")
				return
			}
		}
	}

	ctx := c.Request.Context()

	// 阶段一：预验证所有账号存在，收集 credentials
	type accountUpdate struct {
		ID          int64
		Credentials map[string]any
	}
	updates := make([]accountUpdate, 0, len(req.AccountIDs))
	for _, accountID := range req.AccountIDs {
		account, err := h.adminService.GetAccount(ctx, accountID)
		if err != nil {
			response.Error(c, 404, fmt.Sprintf("Account %d not found", accountID))
			return
		}
		if account.Credentials == nil {
			account.Credentials = make(map[string]any)
		}
		account.Credentials[req.Field] = req.Value
		updates = append(updates, accountUpdate{ID: accountID, Credentials: account.Credentials})
	}

	// 阶段二：依次更新，返回每个账号的成功/失败明细，便于调用方重试
	success := 0
	failed := 0
	successIDs := make([]int64, 0, len(updates))
	failedIDs := make([]int64, 0, len(updates))
	results := make([]gin.H, 0, len(updates))
	for _, u := range updates {
		updateInput := &service.UpdateAccountInput{Credentials: u.Credentials}
		if _, err := h.adminService.UpdateAccount(ctx, u.ID, updateInput); err != nil {
			failed++
			failedIDs = append(failedIDs, u.ID)
			results = append(results, gin.H{
				"account_id": u.ID,
				"success":    false,
				"error":      err.Error(),
			})
			continue
		}
		success++
		successIDs = append(successIDs, u.ID)
		results = append(results, gin.H{
			"account_id": u.ID,
			"success":    true,
		})
	}

	response.Success(c, gin.H{
		"success":     success,
		"failed":      failed,
		"success_ids": successIDs,
		"failed_ids":  failedIDs,
		"results":     results,
	})
}

// BulkUpdate handles bulk updating accounts with selected fields/credentials.
// POST /api/v1/admin/accounts/bulk-update
func (h *AccountHandler) BulkUpdate(c *gin.Context) {
	var req BulkUpdateAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	if len(req.AccountIDs) == 0 && req.Filters == nil {
		response.BadRequest(c, "account_ids or filters is required")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	hasUpdates := req.Name != "" ||
		req.ProxyID != nil ||
		req.Concurrency != nil ||
		req.Priority != nil ||
		req.RateMultiplier != nil ||
		req.LoadFactor != nil ||
		req.Status != "" ||
		req.Schedulable != nil ||
		req.GroupIDs != nil ||
		len(req.Credentials) > 0 ||
		len(req.Extra) > 0

	if !hasUpdates {
		response.BadRequest(c, "No updates provided")
		return
	}

	result, err := h.adminService.BulkUpdateAccounts(c.Request.Context(), &service.BulkUpdateAccountsInput{
		AccountIDs:            req.AccountIDs,
		Filters:               toServiceBulkUpdateAccountFilters(req.Filters),
		Name:                  req.Name,
		ProxyID:               req.ProxyID,
		Concurrency:           req.Concurrency,
		Priority:              req.Priority,
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		Status:                req.Status,
		Schedulable:           req.Schedulable,
		GroupIDs:              req.GroupIDs,
		Credentials:           req.Credentials,
		Extra:                 req.Extra,
		SkipMixedChannelCheck: skipCheck,
	})
	if err != nil {
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			c.JSON(409, gin.H{
				"error":   "mixed_channel_warning",
				"message": mixedErr.Error(),
				"details": gin.H{
					"group_id":         mixedErr.GroupID,
					"group_name":       mixedErr.GroupName,
					"current_platform": mixedErr.CurrentPlatform,
					"other_platform":   mixedErr.OtherPlatform,
				},
			})
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// RepairOpenAIOAuthPlatform repairs OpenAI OAuth accounts that were incorrectly
// persisted as Gemini OAuth accounts by an older upgrade/sync path.
// POST /api/v1/admin/accounts/repair/openai-oauth-platform
func (h *AccountHandler) RepairOpenAIOAuthPlatform(c *gin.Context) {
	result, err := h.adminService.RepairMisclassifiedOpenAIOAuthAccounts(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func toServiceBulkUpdateAccountFilters(filters *BulkUpdateAccountFilters) *service.BulkUpdateAccountFilters {
	if filters == nil {
		return nil
	}
	return &service.BulkUpdateAccountFilters{
		Platform:    filters.Platform,
		Type:        filters.Type,
		Status:      filters.Status,
		Group:       filters.Group,
		Search:      filters.Search,
		PrivacyMode: filters.PrivacyMode,
	}
}
