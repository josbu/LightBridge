package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/handler/dto"
	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

// List handles listing all accounts with pagination
// GET /api/v1/admin/accounts
func (h *AccountHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	search := c.Query("search")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	// 标准化和验证 search 参数
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}
	lite := parseBoolQueryWithDefault(c.Query("lite"), false)

	var groupID int64
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil {
				response.ErrorFrom(c, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter"))
				return
			}
			if parsedGroupID < 0 {
				response.ErrorFrom(c, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter"))
				return
			}
			groupID = parsedGroupID
		}
	}

	accounts, total, err := h.adminService.ListAccounts(c.Request.Context(), page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Get current concurrency counts for all accounts
	accountIDs := make([]int64, len(accounts))
	for i, acc := range accounts {
		accountIDs[i] = acc.ID
	}

	concurrencyCounts := make(map[int64]int)
	var windowCosts map[int64]float64
	var activeSessions map[int64]int
	var rpmCounts map[int64]int

	// 始终获取并发数（Redis ZCARD，极低开销）
	if h.concurrencyService != nil {
		if cc, ccErr := h.concurrencyService.GetAccountConcurrencyBatch(c.Request.Context(), accountIDs); ccErr == nil && cc != nil {
			concurrencyCounts = cc
		}
	}

	// 识别需要查询窗口费用、会话数和 RPM 的账号（Anthropic OAuth/SetupToken 且启用了相应功能）
	windowCostAccountIDs := make([]int64, 0)
	sessionLimitAccountIDs := make([]int64, 0)
	rpmAccountIDs := make([]int64, 0)
	sessionIdleTimeouts := make(map[int64]time.Duration) // 各账号的会话空闲超时配置
	for i := range accounts {
		acc := &accounts[i]
		if acc.IsAnthropicOAuthOrSetupToken() {
			if acc.GetWindowCostLimit() > 0 {
				windowCostAccountIDs = append(windowCostAccountIDs, acc.ID)
			}
			if acc.GetMaxSessions() > 0 {
				sessionLimitAccountIDs = append(sessionLimitAccountIDs, acc.ID)
				sessionIdleTimeouts[acc.ID] = time.Duration(acc.GetSessionIdleTimeoutMinutes()) * time.Minute
			}
			if acc.GetBaseRPM() > 0 {
				rpmAccountIDs = append(rpmAccountIDs, acc.ID)
			}
		}
	}

	// 始终获取 RPM 计数（Redis GET，极低开销）
	if len(rpmAccountIDs) > 0 && h.rpmCache != nil {
		rpmCounts, _ = h.rpmCache.GetRPMBatch(c.Request.Context(), rpmAccountIDs)
		if rpmCounts == nil {
			rpmCounts = make(map[int64]int)
		}
	}

	// 始终获取活跃会话数（Redis ZCARD，低开销）
	if len(sessionLimitAccountIDs) > 0 && h.sessionLimitCache != nil {
		activeSessions, _ = h.sessionLimitCache.GetActiveSessionCountBatch(c.Request.Context(), sessionLimitAccountIDs, sessionIdleTimeouts)
		if activeSessions == nil {
			activeSessions = make(map[int64]int)
		}
	}

	// 始终获取窗口费用（PostgreSQL 聚合查询）
	if len(windowCostAccountIDs) > 0 {
		windowCosts = make(map[int64]float64)
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(c.Request.Context())
		g.SetLimit(10) // 限制并发数

		for i := range accounts {
			acc := &accounts[i]
			if !acc.IsAnthropicOAuthOrSetupToken() || acc.GetWindowCostLimit() <= 0 {
				continue
			}
			accCopy := acc // 闭包捕获
			g.Go(func() error {
				// 使用统一的窗口开始时间计算逻辑（考虑窗口过期情况）
				startTime := accCopy.GetCurrentWindowStartTime()
				stats, err := h.accountUsageService.GetAccountWindowStats(gctx, accCopy.ID, startTime)
				if err == nil && stats != nil {
					mu.Lock()
					windowCosts[accCopy.ID] = stats.StandardCost // 使用标准费用
					mu.Unlock()
				}
				return nil // 不返回错误，允许部分失败
			})
		}
		_ = g.Wait()
	}

	// Build response with concurrency info
	result := make([]AccountWithConcurrency, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		item := AccountWithConcurrency{
			Account:            dto.AccountFromService(acc),
			CurrentConcurrency: concurrencyCounts[acc.ID],
		}

		// 添加窗口费用（仅当启用时）
		if windowCosts != nil {
			if cost, ok := windowCosts[acc.ID]; ok {
				item.CurrentWindowCost = &cost
			}
		}

		// 添加活跃会话数（仅当启用时）
		if activeSessions != nil {
			if count, ok := activeSessions[acc.ID]; ok {
				item.ActiveSessions = &count
			}
		}

		// 添加 RPM 计数（仅当启用时）
		if rpmCounts != nil {
			if rpm, ok := rpmCounts[acc.ID]; ok {
				item.CurrentRPM = &rpm
			}
		}

		result[i] = item
	}

	etag := buildAccountsListETag(result, total, page, pageSize, platform, accountType, status, search, lite)
	if etag != "" {
		c.Header("ETag", etag)
		c.Header("Vary", "If-None-Match")
		if ifNoneMatchMatched(c.GetHeader("If-None-Match"), etag) {
			c.Status(http.StatusNotModified)
			return
		}
	}

	response.Paginated(c, result, total, page, pageSize)
}

func buildAccountsListETag(
	items []AccountWithConcurrency,
	total int64,
	page, pageSize int,
	platform, accountType, status, search string,
	lite bool,
) string {
	payload := struct {
		Total       int64                    `json:"total"`
		Page        int                      `json:"page"`
		PageSize    int                      `json:"page_size"`
		Platform    string                   `json:"platform"`
		AccountType string                   `json:"type"`
		Status      string                   `json:"status"`
		Search      string                   `json:"search"`
		Lite        bool                     `json:"lite"`
		Items       []AccountWithConcurrency `json:"items"`
	}{
		Total:       total,
		Page:        page,
		PageSize:    pageSize,
		Platform:    platform,
		AccountType: accountType,
		Status:      status,
		Search:      search,
		Lite:        lite,
		Items:       items,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "\"" + hex.EncodeToString(sum[:]) + "\""
}

func ifNoneMatchMatched(ifNoneMatch, etag string) bool {
	if etag == "" || ifNoneMatch == "" {
		return false
	}
	for _, token := range strings.Split(ifNoneMatch, ",") {
		candidate := strings.TrimSpace(token)
		if candidate == "*" {
			return true
		}
		if candidate == etag {
			return true
		}
		if strings.HasPrefix(candidate, "W/") && strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// GetByID handles getting an account by ID
// GET /api/v1/admin/accounts/:id
func (h *AccountHandler) GetByID(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// CheckMixedChannel handles checking mixed channel risk for account-group binding.
// POST /api/v1/admin/accounts/check-mixed-channel
func (h *AccountHandler) CheckMixedChannel(c *gin.Context) {
	var req CheckMixedChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if len(req.GroupIDs) == 0 {
		response.Success(c, gin.H{"has_risk": false})
		return
	}

	accountID := int64(0)
	if req.AccountID != nil {
		accountID = *req.AccountID
	}

	err := h.adminService.CheckMixedChannelRisk(c.Request.Context(), accountID, req.Platform, req.GroupIDs)
	if err != nil {
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			response.Success(c, gin.H{
				"has_risk": true,
				"error":    "mixed_channel_warning",
				"message":  mixedErr.Error(),
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

	response.Success(c, gin.H{"has_risk": false})
}

// Create handles creating a new account
// POST /api/v1/admin/accounts
func (h *AccountHandler) Create(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	// 捕获闭包内创建的账号引用，用于创建成功后触发异步探测。
	// 幂等重放时闭包不会执行 → createdAccount 为 nil → 不重复调度。
	var createdAccount *service.Account

	result, err := executeAdminIdempotent(c, "admin.accounts.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		account, execErr := h.adminService.CreateAccount(ctx, &service.CreateAccountInput{
			Name:                  req.Name,
			Notes:                 req.Notes,
			Platform:              req.Platform,
			Type:                  req.Type,
			Credentials:           req.Credentials,
			Extra:                 req.Extra,
			ProxyID:               req.ProxyID,
			Concurrency:           req.Concurrency,
			Priority:              req.Priority,
			RateMultiplier:        req.RateMultiplier,
			LoadFactor:            req.LoadFactor,
			GroupIDs:              req.GroupIDs,
			ExpiresAt:             req.ExpiresAt,
			AutoPauseOnExpired:    req.AutoPauseOnExpired,
			SkipMixedChannelCheck: skipCheck,
		})
		if execErr != nil {
			return nil, execErr
		}
		createdAccount = account
		// Antigravity OAuth: 新账号直接设置隐私
		h.adminService.ForceAntigravityPrivacy(ctx, account)
		// OpenAI OAuth: 新账号直接设置隐私
		h.adminService.ForceOpenAIPrivacy(ctx, account)
		if refreshed, handled, refreshErr := h.adminService.RefreshModuleProviderAccount(ctx, account); handled {
			if refreshErr != nil {
				return nil, refreshErr
			}
			if refreshed != nil {
				createdAccount = refreshed
				account = refreshed
			}
		}
		return h.buildAccountResponseWithRuntime(ctx, account), nil
	})
	if err != nil {
		// 检查是否为混合渠道错误
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			// 创建接口仅返回最小必要字段，详细信息由专门检查接口提供
			c.JSON(409, gin.H{
				"error":   "mixed_channel_warning",
				"message": mixedErr.Error(),
			})
			return
		}

		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}

	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	// OpenAI APIKey 账号创建后异步探测上游 /v1/responses 能力。
	// 探测失败不影响账号创建响应。
	h.scheduleOpenAIResponsesProbe(createdAccount)
	h.scheduleOAuthModelSync(createdAccount)
	response.Success(c, result.Data)
}

// Update handles updating an account
// PUT /api/v1/admin/accounts/:id
func (h *AccountHandler) Update(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	// base_rpm 输入校验：负值归零，超过 10000 截断
	sanitizeExtraBaseRPM(req.Extra)

	// 确定是否跳过混合渠道检查
	skipCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	account, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Name:                  req.Name,
		Notes:                 req.Notes,
		Type:                  req.Type,
		Credentials:           req.Credentials,
		Extra:                 req.Extra,
		ProxyID:               req.ProxyID,
		Concurrency:           req.Concurrency, // 指针类型，nil 表示未提供
		Priority:              req.Priority,    // 指针类型，nil 表示未提供
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		Status:                req.Status,
		GroupIDs:              req.GroupIDs,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    req.AutoPauseOnExpired,
		SkipMixedChannelCheck: skipCheck,
	})
	if err != nil {
		// 检查是否为混合渠道错误
		var mixedErr *service.MixedChannelError
		if errors.As(err, &mixedErr) {
			// 更新接口仅返回最小必要字段，详细信息由专门检查接口提供
			c.JSON(409, gin.H{
				"error":   "mixed_channel_warning",
				"message": mixedErr.Error(),
			})
			return
		}

		response.ErrorFrom(c, err)
		return
	}

	// OpenAI APIKey: credentials 修改后重新探测上游能力（base_url/api_key 可能变更）。
	// 异步执行，探测失败不影响账号更新响应。
	if len(req.Credentials) > 0 {
		h.scheduleOpenAIResponsesProbe(account)
	}

	response.Success(c, h.buildAccountResponseWithRuntime(c.Request.Context(), account))
}

// scheduleOpenAIResponsesProbe 异步触发 OpenAI APIKey 账号的 Responses API 能力探测。
//
// 仅对 platform=openai && type=apikey 账号生效；其他账号无操作。
// 探测本身在 goroutine 中执行（会发一次 HTTP 请求到上游），不会阻塞
// 当前请求。探测错误仅记录日志，不向上下文传播：探测失败时标记保持缺失，
// 网关会按"现状即证据"默认走 Responses。
func (h *AccountHandler) scheduleOpenAIResponsesProbe(account *service.Account) {
	if account == nil || account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeAPIKey {
		return
	}
	if h.accountTestService == nil {
		return
	}
	accountID := account.ID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("openai_responses_probe_panic", "account_id", accountID, "recover", r)
			}
		}()
		h.accountTestService.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), accountID)
	}()
}

// scheduleOAuthModelSync best-effort syncs the live upstream model list after
// OAuth credentials are available. Failure is stored on the catalog sync state
// and never blocks the account workflow.
func (h *AccountHandler) scheduleOAuthModelSync(account *service.Account) {
	if account == nil || !account.IsOAuth() || h.accountTestService == nil || h.modelCatalogService == nil {
		return
	}
	accountID := account.ID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("oauth_model_catalog_sync_panic", "account_id", accountID, "recover", r)
			}
		}()
		ctx := context.Background()
		latest, err := h.adminService.GetAccount(ctx, accountID)
		if err != nil {
			return
		}
		models, err := h.accountTestService.FetchUpstreamSupportedModels(ctx, latest)
		if err != nil {
			h.modelCatalogService.RecordSyncFailure(ctx, latest, service.ModelCatalogSourceUpstream, err)
			return
		}
		if _, _, err := h.modelCatalogService.ReplaceAccountModelsFromSync(ctx, latest, models, service.ModelCatalogSourceUpstream); err != nil {
			slog.Warn("oauth_model_catalog_sync_save_failed", "account_id", accountID, "err", err)
		}
	}()
}

// Delete handles deleting an account
// DELETE /api/v1/admin/accounts/:id
func (h *AccountHandler) Delete(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	err = h.adminService.DeleteAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Account deleted successfully"})
}
