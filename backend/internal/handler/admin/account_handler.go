package admin

import (
	"context"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/handler/dto"
	"github.com/WilliamWang1721/LightBridge/internal/service"
)

// AccountHandler handles admin account management
type AccountHandler struct {
	adminService            service.AdminService
	openaiOAuthService      *service.OpenAIOAuthService
	geminiOAuthService      *service.GeminiOAuthService
	antigravityOAuthService *service.AntigravityOAuthService
	rateLimitService        *service.RateLimitService
	accountUsageService     *service.AccountUsageService
	accountTestService      *service.AccountTestService
	concurrencyService      *service.ConcurrencyService
	crsSyncService          *service.CRSSyncService
	sessionLimitCache       service.SessionLimitCache
	rpmCache                service.RPMCache
	tokenCacheInvalidator   service.TokenCacheInvalidator
	modelCatalogService     *service.ModelCatalogService
}

// NewAccountHandler creates a new admin account handler
func NewAccountHandler(
	adminService service.AdminService,
	openaiOAuthService *service.OpenAIOAuthService,
	geminiOAuthService *service.GeminiOAuthService,
	antigravityOAuthService *service.AntigravityOAuthService,
	rateLimitService *service.RateLimitService,
	accountUsageService *service.AccountUsageService,
	accountTestService *service.AccountTestService,
	concurrencyService *service.ConcurrencyService,
	crsSyncService *service.CRSSyncService,
	sessionLimitCache service.SessionLimitCache,
	rpmCache service.RPMCache,
	tokenCacheInvalidator service.TokenCacheInvalidator,
	modelCatalogServices ...*service.ModelCatalogService,
) *AccountHandler {
	var modelCatalogService *service.ModelCatalogService
	if len(modelCatalogServices) > 0 {
		modelCatalogService = modelCatalogServices[0]
	}
	return &AccountHandler{
		adminService:            adminService,
		openaiOAuthService:      openaiOAuthService,
		geminiOAuthService:      geminiOAuthService,
		antigravityOAuthService: antigravityOAuthService,
		rateLimitService:        rateLimitService,
		accountUsageService:     accountUsageService,
		accountTestService:      accountTestService,
		concurrencyService:      concurrencyService,
		crsSyncService:          crsSyncService,
		sessionLimitCache:       sessionLimitCache,
		rpmCache:                rpmCache,
		tokenCacheInvalidator:   tokenCacheInvalidator,
		modelCatalogService:     modelCatalogService,
	}
}

// CreateAccountRequest represents create account request
type CreateAccountRequest struct {
	Name                    string         `json:"name" binding:"required"`
	Notes                   *string        `json:"notes"`
	Platform                string         `json:"platform" binding:"required"`
	Type                    string         `json:"type" binding:"required,oneof=oauth setup-token apikey upstream bedrock service_account"`
	Credentials             map[string]any `json:"credentials" binding:"required"`
	Extra                   map[string]any `json:"extra"`
	ProxyID                 *int64         `json:"proxy_id"`
	Concurrency             int            `json:"concurrency"`
	Priority                int            `json:"priority"`
	RateMultiplier          *float64       `json:"rate_multiplier"`
	LoadFactor              *int           `json:"load_factor"`
	GroupIDs                []int64        `json:"group_ids"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      *bool          `json:"auto_pause_on_expired"`
	ConfirmMixedChannelRisk *bool          `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

// UpdateAccountRequest represents update account request
// 使用指针类型来区分"未提供"和"设置为0"
type UpdateAccountRequest struct {
	Name                    string         `json:"name"`
	Notes                   *string        `json:"notes"`
	Type                    string         `json:"type" binding:"omitempty,oneof=oauth setup-token apikey upstream bedrock service_account"`
	Credentials             map[string]any `json:"credentials"`
	Extra                   map[string]any `json:"extra"`
	ProxyID                 *int64         `json:"proxy_id"`
	Concurrency             *int           `json:"concurrency"`
	Priority                *int           `json:"priority"`
	RateMultiplier          *float64       `json:"rate_multiplier"`
	LoadFactor              *int           `json:"load_factor"`
	Status                  string         `json:"status" binding:"omitempty,oneof=active inactive error"`
	GroupIDs                *[]int64       `json:"group_ids"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      *bool          `json:"auto_pause_on_expired"`
	ConfirmMixedChannelRisk *bool          `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

// BulkUpdateAccountsRequest represents the payload for bulk editing accounts
type BulkUpdateAccountsRequest struct {
	AccountIDs              []int64                   `json:"account_ids"`
	Filters                 *BulkUpdateAccountFilters `json:"filters"`
	Name                    string                    `json:"name"`
	ProxyID                 *int64                    `json:"proxy_id"`
	Concurrency             *int                      `json:"concurrency"`
	Priority                *int                      `json:"priority"`
	RateMultiplier          *float64                  `json:"rate_multiplier"`
	LoadFactor              *int                      `json:"load_factor"`
	Status                  string                    `json:"status" binding:"omitempty,oneof=active inactive error"`
	Schedulable             *bool                     `json:"schedulable"`
	GroupIDs                *[]int64                  `json:"group_ids"`
	Credentials             map[string]any            `json:"credentials"`
	Extra                   map[string]any            `json:"extra"`
	ConfirmMixedChannelRisk *bool                     `json:"confirm_mixed_channel_risk"` // 用户确认混合渠道风险
}

type BulkUpdateAccountFilters struct {
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Group       string `json:"group"`
	Search      string `json:"search"`
	PrivacyMode string `json:"privacy_mode"`
}

// CheckMixedChannelRequest represents check mixed channel risk request
type CheckMixedChannelRequest struct {
	Platform  string  `json:"platform" binding:"required"`
	GroupIDs  []int64 `json:"group_ids"`
	AccountID *int64  `json:"account_id"`
}

// AccountWithConcurrency extends Account with real-time concurrency info
type AccountWithConcurrency struct {
	*dto.Account
	CurrentConcurrency int `json:"current_concurrency"`
	// 以下字段仅对 Anthropic OAuth/SetupToken 账号有效，且仅在启用相应功能时返回
	CurrentWindowCost *float64 `json:"current_window_cost,omitempty"` // 当前窗口费用
	ActiveSessions    *int     `json:"active_sessions,omitempty"`     // 当前活跃会话数
	CurrentRPM        *int     `json:"current_rpm,omitempty"`         // 当前分钟 RPM 计数
}

const accountListGroupUngroupedQueryValue = "ungrouped"

func (h *AccountHandler) buildAccountResponseWithRuntime(ctx context.Context, account *service.Account) AccountWithConcurrency {
	item := AccountWithConcurrency{
		Account:            dto.AccountFromService(account),
		CurrentConcurrency: 0,
	}
	if account == nil {
		return item
	}

	if h.concurrencyService != nil {
		if counts, err := h.concurrencyService.GetAccountConcurrencyBatch(ctx, []int64{account.ID}); err == nil {
			item.CurrentConcurrency = counts[account.ID]
		}
	}

	if account.IsAnthropicOAuthOrSetupToken() {
		if h.accountUsageService != nil && account.GetWindowCostLimit() > 0 {
			startTime := account.GetCurrentWindowStartTime()
			if stats, err := h.accountUsageService.GetAccountWindowStats(ctx, account.ID, startTime); err == nil && stats != nil {
				cost := stats.StandardCost
				item.CurrentWindowCost = &cost
			}
		}

		if h.sessionLimitCache != nil && account.GetMaxSessions() > 0 {
			idleTimeout := time.Duration(account.GetSessionIdleTimeoutMinutes()) * time.Minute
			idleTimeouts := map[int64]time.Duration{account.ID: idleTimeout}
			if sessions, err := h.sessionLimitCache.GetActiveSessionCountBatch(ctx, []int64{account.ID}, idleTimeouts); err == nil {
				if count, ok := sessions[account.ID]; ok {
					item.ActiveSessions = &count
				}
			}
		}

		if h.rpmCache != nil && account.GetBaseRPM() > 0 {
			if rpm, err := h.rpmCache.GetRPM(ctx, account.ID); err == nil {
				item.CurrentRPM = &rpm
			}
		}
	}

	return item
}
