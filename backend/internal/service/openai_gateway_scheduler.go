package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/xai"
)

// SelectAccount selects an OpenAI account with sticky session support
func (s *OpenAIGatewayService) SelectAccount(ctx context.Context, groupID *int64, sessionHash string) (*Account, error) {
	return s.SelectAccountForModel(ctx, groupID, sessionHash, "")
}

// SelectAccountForModel selects an account supporting the requested model
func (s *OpenAIGatewayService) SelectAccountForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*Account, error) {
	return s.SelectAccountForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, nil)
}

// SelectAccountForModelWithExclusions selects an account supporting the requested model while excluding specified accounts.
// SelectAccountForModelWithExclusions 选择支持指定模型的账号，同时排除指定的账号。
func (s *OpenAIGatewayService) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	return s.selectAccountForModelWithExclusions(s.withOpenAIQuotaAutoPauseContext(ctx), groupID, sessionHash, requestedModel, excludedIDs, false, 0, "", PlatformOpenAI)
}

// noAvailableOpenAISelectionError builds the standard "no account available" error
// while preserving the compact-specific error when applicable.
func noAvailableOpenAISelectionError(requestedModel string, compactBlocked bool) error {
	if compactBlocked {
		return ErrNoAvailableCompactAccounts
	}
	if requestedModel != "" {
		return fmt.Errorf("no available OpenAI accounts supporting model: %s", requestedModel)
	}
	return errors.New("no available OpenAI accounts")
}

// openAICompactSupportTier classifies an OpenAI account by compact capability.
// 0 = explicitly unsupported, 1 = unknown / not yet probed, 2 = explicitly supported.
func openAICompactSupportTier(account *Account) int {
	if account == nil || !account.IsOpenAI() {
		return 0
	}
	supported, known := account.OpenAICompactSupportKnown()
	if !known {
		return 1
	}
	if supported {
		return 2
	}
	return 0
}

// isOpenAIAccountEligibleForRequest centralises the schedulable / OpenAI / model /
// compact-support checks used during account selection.
func isOpenAIAccountEligibleForRequest(ctx context.Context, account *Account, requestedModel string, requireCompact bool, requiredCapability OpenAIEndpointCapability, platform string) bool {
	if account == nil || !account.IsSchedulableForModelWithContext(ctx, requestedModel) {
		return false
	}
	platform = normalizeOpenAICompatiblePlatform(platform)
	if platform == PlatformGrok {
		if !account.IsGrok() || requireCompact {
			return false
		}
		if paused, reason := shouldAutoPauseGrokAccountByQuota(account); paused {
			slog.Debug("grok_account_auto_paused_by_quota",
				"account_id", account.ID,
				"window", reason.window,
				"threshold", reason.threshold,
				"utilization", reason.utilization,
			)
			return false
		}
		if requiredCapability != "" && !isOpenAITextEndpointCapability(requiredCapability) {
			return false
		}
		if requestedModel != "" && !account.IsModelSupported(requestedModel) {
			return false
		}
		return accountMatchesRequestProtocol(ctx, account)
	}
	if !isOpenAISelectionProtocolCompatible(ctx, account) {
		return false
	}
	// Custom 账号请求级协议匹配（sticky session 等绕过候选过滤的路径也需校验）。
	if !accountMatchesRequestProtocol(ctx, account) {
		return false
	}
	if account.IsOpenAI() {
		if paused, reason := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
			// Debug level: this fires per-candidate on the scheduling hot path, so Info
			// would amplify into log spam once several accounts cross the threshold.
			slog.Debug("account_auto_paused_by_quota",
				"account_id", account.ID,
				"window", reason.window,
				"threshold", reason.threshold,
				"utilization", reason.utilization,
			)
			return false
		}
	}
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return false
	}
	if account.IsOpenAI() {
		if !account.SupportsOpenAIEndpointCapability(requiredCapability) {
			return false
		}
		if requireCompact && openAICompactSupportTier(account) == 0 {
			return false
		}
	} else if requireCompact {
		return false
	}
	return true
}

func isOpenAISelectionProtocolCompatible(ctx context.Context, account *Account) bool {
	if account == nil {
		return false
	}
	inbound := InboundProtocolFromContext(ctx)
	if IsMessageProtocol(inbound) {
		_, ok := ProtocolRouteDecisionForAccount(ctx, account)
		return ok
	}
	return account.IsOpenAI()
}

type openAIQuotaAutoPauseDecision struct {
	window      string
	threshold   float64
	utilization float64
}

func shouldAutoPauseGrokAccountByQuota(account *Account) (bool, openAIQuotaAutoPauseDecision) {
	if account == nil || !account.IsGrok() || account.Type != AccountTypeOAuth {
		return false, openAIQuotaAutoPauseDecision{}
	}
	snapshot, err := grokQuotaSnapshotFromExtra(account.Extra)
	if err != nil || snapshot == nil {
		return false, openAIQuotaAutoPauseDecision{}
	}
	now := time.Now()
	if grokQuotaSnapshotStaleForPause(snapshot, now) {
		return false, openAIQuotaAutoPauseDecision{}
	}
	if grokQuotaRetryAfterActive(snapshot, now) {
		return true, openAIQuotaAutoPauseDecision{window: "retry_after", threshold: 1, utilization: 1}
	}
	if paused, decision := shouldAutoPauseGrokQuotaWindow("requests", snapshot.Requests, now); paused {
		return true, decision
	}
	if paused, decision := shouldAutoPauseGrokQuotaWindow("tokens", snapshot.Tokens, now); paused {
		return true, decision
	}
	return false, openAIQuotaAutoPauseDecision{}
}

func grokQuotaRetryAfterActive(snapshot *xai.QuotaSnapshot, now time.Time) bool {
	if snapshot == nil || snapshot.RetryAfterSeconds == nil || *snapshot.RetryAfterSeconds <= 0 {
		return false
	}
	if strings.TrimSpace(snapshot.UpdatedAt) == "" {
		return true
	}
	updatedAt, err := parseTime(snapshot.UpdatedAt)
	if err != nil {
		return true
	}
	return now.Before(updatedAt.Add(time.Duration(*snapshot.RetryAfterSeconds) * time.Second))
}

func shouldAutoPauseGrokQuotaWindow(name string, window *xai.QuotaWindow, now time.Time) (bool, openAIQuotaAutoPauseDecision) {
	if window == nil || window.Limit == nil || window.Remaining == nil || *window.Limit <= 0 {
		return false, openAIQuotaAutoPauseDecision{}
	}
	if window.ResetUnix != nil && *window.ResetUnix > 0 && !now.Before(time.Unix(*window.ResetUnix, 0)) {
		return false, openAIQuotaAutoPauseDecision{}
	}
	utilization := float64(*window.Limit-*window.Remaining) / float64(*window.Limit)
	if *window.Remaining <= 0 || utilization >= 1 {
		return true, openAIQuotaAutoPauseDecision{window: name, threshold: 1, utilization: utilization}
	}
	return false, openAIQuotaAutoPauseDecision{}
}

func grokQuotaSnapshotStaleForPause(snapshot *xai.QuotaSnapshot, now time.Time) bool {
	if snapshot == nil || strings.TrimSpace(snapshot.UpdatedAt) == "" {
		return false
	}
	updatedAt, err := parseTime(snapshot.UpdatedAt)
	if err != nil {
		return false
	}
	return now.Sub(updatedAt) >= grokQuotaPauseStaleAfter
}

func shouldAutoPauseOpenAIAccountByQuota(ctx context.Context, account *Account) (bool, openAIQuotaAutoPauseDecision) {
	if account == nil || !account.IsOpenAI() {
		return false, openAIQuotaAutoPauseDecision{}
	}
	// Per-account explicit-disable flags must take precedence over the global default.
	// Without these, leaving the account threshold blank means "use global default",
	// so an admin has no way to exempt a single account from auto-pause once a global
	// default exists. The disable flag is per-window so an account can opt out of
	// only 5h or only 7d auto-pause.
	disabled5h := resolveAccountExtraBool(account.Extra, "auto_pause_5h_disabled")
	disabled7d := resolveAccountExtraBool(account.Extra, "auto_pause_7d_disabled")
	threshold5h, threshold7d := resolveOpenAIQuotaAutoPauseThresholds(ctx, account)
	now := time.Now()
	if !disabled5h && threshold5h > 0 {
		if utilization, ok := resolveOpenAIQuotaUtilization(account.Extra, "5h", now); ok && utilization >= threshold5h {
			return true, openAIQuotaAutoPauseDecision{window: "5h", threshold: threshold5h, utilization: utilization}
		}
	}
	if !disabled7d && threshold7d > 0 {
		if utilization, ok := resolveOpenAIQuotaUtilization(account.Extra, "7d", now); ok && utilization >= threshold7d {
			return true, openAIQuotaAutoPauseDecision{window: "7d", threshold: threshold7d, utilization: utilization}
		}
	}
	return false, openAIQuotaAutoPauseDecision{}
}

// resolveAccountExtraBool reads a bool-like value from account extra, tolerating
// the few shapes JSON unmarshalling may produce (real bool, "true"/"false"
// strings, 0/1 numbers).
func resolveAccountExtraBool(extra map[string]any, key string) bool {
	if len(extra) == 0 {
		return false
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && parsed
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0
		}
	}
	return false
}

func resolveOpenAIQuotaAutoPauseThresholds(ctx context.Context, account *Account) (float64, float64) {
	threshold5h, _ := resolveAccountExtraNumber(account.Extra, "auto_pause_5h_threshold")
	threshold7d, _ := resolveAccountExtraNumber(account.Extra, "auto_pause_7d_threshold")
	threshold5h = clamp01(threshold5h)
	threshold7d = clamp01(threshold7d)
	if threshold5h > 0 && threshold7d > 0 {
		return threshold5h, threshold7d
	}
	settings := openAIQuotaAutoPauseSettingsFromContext(ctx)
	if threshold5h <= 0 {
		threshold5h = clamp01(settings.DefaultThreshold5h)
	}
	if threshold7d <= 0 {
		threshold7d = clamp01(settings.DefaultThreshold7d)
	}
	return threshold5h, threshold7d
}

func resolveAccountExtraNumber(extra map[string]any, keys ...string) (float64, bool) {
	if len(extra) == 0 {
		return 0, false
	}
	for _, key := range keys {
		value, ok := extra[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v, true
		case float32:
			return float64(v), true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case json.Number:
			parsed, err := v.Float64()
			if err == nil {
				return parsed, true
			}
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

// resolveOpenAIQuotaUtilization returns the current utilization ratio (0..1) for the
// given Codex usage window. ok=false means there is no usable signal to pause on:
// either no snapshot exists, or the window has already rolled over so the cached
// percentage is stale. The stale guard matters because a paused account stops
// receiving requests, so its snapshot is never refreshed from upstream headers —
// without this check an old used_percent would keep the account paused forever even
// after the real window reset.
func resolveOpenAIQuotaUtilization(extra map[string]any, window string, now time.Time) (float64, bool) {
	usedPercent := readOpenAIQuotaUsedPercent(extra, window)
	if usedPercent <= 0 {
		return 0, false
	}
	if openAIQuotaWindowReset(extra, window, now) {
		return 0, false
	}
	return usedPercent / 100, true
}

// openAIQuotaWindowReset reports whether the Codex usage window's reset time has
// already passed relative to now. It prefers the absolute codex_<window>_reset_at
// timestamp and falls back to codex_<window>_reset_after_seconds anchored at
// codex_usage_updated_at, mirroring AccountUsageService's window-progress logic.
func openAIQuotaWindowReset(extra map[string]any, window string, now time.Time) bool {
	if len(extra) == 0 {
		return false
	}
	if resetAtRaw, ok := extra["codex_"+window+"_reset_at"]; ok {
		if resetAt, err := parseTime(fmt.Sprint(resetAtRaw)); err == nil {
			return !now.Before(resetAt)
		}
	}
	resetAfter := parseExtraInt(extra["codex_"+window+"_reset_after_seconds"])
	if resetAfter <= 0 {
		return false
	}
	base := now
	if updatedRaw, ok := extra["codex_usage_updated_at"]; ok {
		if updatedAt, err := parseTime(fmt.Sprint(updatedRaw)); err == nil {
			base = updatedAt
		}
	}
	resetAt := base.Add(time.Duration(resetAfter) * time.Second)
	return !now.Before(resetAt)
}

func readOpenAIQuotaUsedPercent(extra map[string]any, window string) float64 {
	if len(extra) == 0 {
		return 0
	}
	if value, ok := resolveAccountExtraNumber(extra, "codex_"+window+"_used_percent"); ok {
		return value
	}
	return 0
}

type openAIQuotaAutoPauseCtxKey struct{}

func withOpenAIQuotaAutoPauseSettings(ctx context.Context, settings OpsOpenAIAccountQuotaAutoPauseSettings) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIQuotaAutoPauseCtxKey{}, settings)
}

func openAIQuotaAutoPauseSettingsFromContext(ctx context.Context) OpsOpenAIAccountQuotaAutoPauseSettings {
	if ctx == nil {
		return OpsOpenAIAccountQuotaAutoPauseSettings{}
	}
	settings, _ := ctx.Value(openAIQuotaAutoPauseCtxKey{}).(OpsOpenAIAccountQuotaAutoPauseSettings)
	return settings
}

func (s *OpenAIGatewayService) withOpenAIQuotaAutoPauseContext(ctx context.Context) context.Context {
	if s == nil || s.settingService == nil {
		return ctx
	}
	return withOpenAIQuotaAutoPauseSettings(ctx, s.settingService.GetOpenAIQuotaAutoPauseSettings(ctx))
}

// prioritizeOpenAICompactAccounts re-orders a slice so that accounts with known
// compact support are tried first, followed by unknown, then explicitly unsupported.
// The relative order within each tier is preserved.
func prioritizeOpenAICompactAccounts(accounts []*Account) []*Account {
	if len(accounts) == 0 {
		return nil
	}
	supported := make([]*Account, 0, len(accounts))
	unknown := make([]*Account, 0, len(accounts))
	unsupported := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		switch openAICompactSupportTier(account) {
		case 2:
			supported = append(supported, account)
		case 1:
			unknown = append(unknown, account)
		default:
			unsupported = append(unsupported, account)
		}
	}
	out := make([]*Account, 0, len(accounts))
	out = append(out, supported...)
	out = append(out, unknown...)
	out = append(out, unsupported...)
	return out
}

// resolveOpenAIAccountUpstreamModelForRequest resolves the upstream model that
// would be sent for a given request, honouring compact-only mappings when the
// caller is on the /responses/compact path.

func (s *OpenAIGatewayService) selectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, stickyAccountID int64, requiredCapability OpenAIEndpointCapability, platform string) (*Account, error) {
	platform = normalizeOpenAICompatiblePlatform(platform)
	if s.checkChannelPricingRestriction(ctx, groupID, requestedModel) {
		slog.Warn("channel pricing restriction blocked request",
			"group_id", derefGroupID(groupID),
			"model", requestedModel)
		return nil, fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, requestedModel)
	}

	// 1. 尝试粘性会话命中
	// Try sticky session hit
	if account := s.tryStickySessionHit(ctx, groupID, sessionHash, requestedModel, excludedIDs, requireCompact, stickyAccountID, requiredCapability, platform); account != nil {
		return account, nil
	}

	// 2. 获取可调度的 OpenAI 账号
	// Get schedulable OpenAI accounts
	accounts, err := s.listSchedulableAccounts(ctx, groupID, platform)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}

	// 3. 按优先级 + LRU 选择最佳账号
	// Select by priority + LRU
	selected, compactBlocked := s.selectBestAccount(ctx, groupID, accounts, requestedModel, excludedIDs, requireCompact, requiredCapability, platform)

	if selected == nil {
		return nil, noAvailableOpenAISelectionError(requestedModel, compactBlocked)
	}

	hydrated, err := s.hydrateSelectedAccount(ctx, selected)
	if err != nil {
		return nil, err
	}

	// 4. 设置粘性会话绑定
	// Set sticky session binding
	if sessionHash != "" {
		_ = s.setStickySessionAccountID(ctx, groupID, sessionHash, selected.ID, openaiStickySessionTTL)
	}

	return hydrated, nil
}

// tryStickySessionHit 尝试从粘性会话获取账号。
// 如果命中且账号可用则返回账号；如果账号不可用则清理会话并返回 nil。
//
// tryStickySessionHit attempts to get account from sticky session.
// Returns account if hit and usable; clears session and returns nil if account is unavailable.
func (s *OpenAIGatewayService) tryStickySessionHit(ctx context.Context, groupID *int64, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, stickyAccountID int64, requiredCapability OpenAIEndpointCapability, platform string) *Account {
	if sessionHash == "" {
		return nil
	}

	accountID := stickyAccountID
	if accountID <= 0 {
		var err error
		accountID, err = s.getStickySessionAccountID(ctx, groupID, sessionHash)
		if err != nil || accountID <= 0 {
			return nil
		}
	}

	if _, excluded := excludedIDs[accountID]; excluded {
		return nil
	}

	account, err := s.getSchedulableAccount(ctx, accountID)
	if err != nil {
		return nil
	}

	// 检查账号是否需要清理粘性会话
	// Check if sticky session should be cleared
	if shouldClearStickySession(account, requestedModel) {
		_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
		return nil
	}

	// 验证账号是否可用于当前请求
	// Verify account is usable for current request
	if !isOpenAIAccountEligibleForRequest(ctx, account, requestedModel, false, requiredCapability, platform) {
		return nil
	}
	if s.isOpenAIAccountRuntimeBlocked(account) {
		_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
		return nil
	}
	account = s.recheckSelectedOpenAIAccountFromDB(ctx, account, requestedModel, requireCompact, requiredCapability, platform)
	if account == nil {
		_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
		return nil
	}
	if groupID != nil && s.needsUpstreamChannelRestrictionCheck(ctx, groupID) &&
		s.isUpstreamModelRestrictedByChannel(ctx, *groupID, account, requestedModel, requireCompact) {
		_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
		return nil
	}

	// 刷新会话 TTL 并返回账号
	// Refresh session TTL and return account
	_ = s.refreshStickySessionTTL(ctx, groupID, sessionHash, openaiStickySessionTTL)
	return account
}

// selectBestAccount 从候选账号中选择最佳账号（优先级 + LRU）。
// 返回 nil 表示无可用账号。
//
// selectBestAccount selects the best account from candidates (priority + LRU).
// Returns nil if no available account. The second return reports whether at
// least one candidate was filtered out solely because it lacks compact support
// (only meaningful when requireCompact=true).
func (s *OpenAIGatewayService) selectBestAccount(ctx context.Context, groupID *int64, accounts []Account, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, requiredCapability OpenAIEndpointCapability, platform string) (*Account, bool) {
	var selected *Account
	selectedCompactTier := -1
	compactBlocked := false
	needsUpstreamCheck := s.needsUpstreamChannelRestrictionCheck(ctx, groupID)

	for i := range accounts {
		acc := &accounts[i]

		// 跳过被排除的账号
		// Skip excluded accounts
		if _, excluded := excludedIDs[acc.ID]; excluded {
			continue
		}

		fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, acc, requestedModel, false, requiredCapability, platform)
		if fresh == nil {
			continue
		}
		fresh = s.recheckSelectedOpenAIAccountFromDB(ctx, fresh, requestedModel, false, requiredCapability, platform)
		if fresh == nil {
			continue
		}
		if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, fresh, requestedModel, requireCompact) {
			continue
		}
		compactTier := 0
		if requireCompact {
			compactTier = openAICompactSupportTier(fresh)
			if compactTier == 0 {
				compactBlocked = true
				continue
			}
		}

		// 选择优先级最高且最久未使用的账号
		// Select highest priority and least recently used
		if selected == nil {
			selected = fresh
			selectedCompactTier = compactTier
			continue
		}

		// compact 模式下高 tier 优先；同 tier 内才比较 priority/LRU。
		if requireCompact && compactTier != selectedCompactTier {
			if compactTier > selectedCompactTier {
				selected = fresh
				selectedCompactTier = compactTier
			}
			continue
		}

		if s.isBetterAccount(fresh, selected) {
			selected = fresh
			selectedCompactTier = compactTier
		}
	}

	return selected, compactBlocked
}

// isBetterAccount 判断 candidate 是否比 current 更优。
// 规则：优先级更高（数值更小）优先；同优先级时，未使用过的优先，其次是最久未使用的。
//
// isBetterAccount checks if candidate is better than current.
// Rules: higher priority (lower value) wins; same priority: never used > least recently used.
func (s *OpenAIGatewayService) isBetterAccount(candidate, current *Account) bool {
	// 优先级更高（数值更小）
	// Higher priority (lower value)
	if candidate.Priority < current.Priority {
		return true
	}
	if candidate.Priority > current.Priority {
		return false
	}

	// 同优先级，比较最后使用时间
	// Same priority, compare last used time
	switch {
	case candidate.LastUsedAt == nil && current.LastUsedAt != nil:
		// candidate 从未使用，优先
		return true
	case candidate.LastUsedAt != nil && current.LastUsedAt == nil:
		// current 从未使用，保持
		return false
	case candidate.LastUsedAt == nil && current.LastUsedAt == nil:
		// 都未使用，保持
		return false
	default:
		// 都使用过，选择最久未使用的
		return candidate.LastUsedAt.Before(*current.LastUsedAt)
	}
}

// SelectAccountWithLoadAwareness selects an account with load-awareness and wait plan.
func (s *OpenAIGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*AccountSelectionResult, error) {
	return s.selectAccountWithLoadAwareness(s.withOpenAIQuotaAutoPauseContext(ctx), groupID, sessionHash, requestedModel, excludedIDs, false, "", PlatformOpenAI)
}

func (s *OpenAIGatewayService) selectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, requiredCapability OpenAIEndpointCapability, platform string) (*AccountSelectionResult, error) {
	platform = normalizeOpenAICompatiblePlatform(platform)
	if s.checkChannelPricingRestriction(ctx, groupID, requestedModel) {
		slog.Warn("channel pricing restriction blocked request",
			"group_id", derefGroupID(groupID),
			"model", requestedModel)
		return nil, fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, requestedModel)
	}

	cfg := s.schedulingConfig()
	needsUpstreamCheck := s.needsUpstreamChannelRestrictionCheck(ctx, groupID)
	var stickyAccountID int64
	if sessionHash != "" && s.cache != nil {
		if accountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash); err == nil {
			stickyAccountID = accountID
		}
	}
	if s.concurrencyService == nil || !cfg.LoadBatchEnabled {
		account, err := s.selectAccountForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, excludedIDs, requireCompact, stickyAccountID, requiredCapability, platform)
		if err != nil {
			return nil, err
		}
		result, err := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if err == nil && result != nil && result.Acquired {
			return s.newAcquiredSelectionResult(ctx, account, result.ReleaseFunc)
		}
		if stickyAccountID > 0 && stickyAccountID == account.ID && s.concurrencyService != nil {
			waitingCount, _ := s.concurrencyService.GetAccountWaitingCount(ctx, account.ID)
			if waitingCount < cfg.StickySessionMaxWaiting {
				return s.newSelectionResult(ctx, account, false, nil, &AccountWaitPlan{
					AccountID:      account.ID,
					MaxConcurrency: account.Concurrency,
					Timeout:        cfg.StickySessionWaitTimeout,
					MaxWaiting:     cfg.StickySessionMaxWaiting,
				})
			}
		}
		return s.newSelectionResult(ctx, account, false, nil, &AccountWaitPlan{
			AccountID:      account.ID,
			MaxConcurrency: account.Concurrency,
			Timeout:        cfg.FallbackWaitTimeout,
			MaxWaiting:     cfg.FallbackMaxWaiting,
		})
	}

	accounts, err := s.listSchedulableAccounts(ctx, groupID, platform)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrNoAvailableAccounts
	}

	isExcluded := func(accountID int64) bool {
		if excludedIDs == nil {
			return false
		}
		_, excluded := excludedIDs[accountID]
		return excluded
	}

	// ============ Layer 1: Sticky session ============
	if sessionHash != "" {
		accountID := stickyAccountID
		if accountID > 0 && !isExcluded(accountID) {
			account, err := s.getSchedulableAccount(ctx, accountID)
			if err == nil {
				clearSticky := shouldClearStickySession(account, requestedModel)
				if clearSticky {
					_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
				}
				if !clearSticky && isOpenAIAccountEligibleForRequest(ctx, account, requestedModel, false, requiredCapability, platform) {
					account = s.recheckSelectedOpenAIAccountFromDB(ctx, account, requestedModel, requireCompact, requiredCapability, platform)
					if account == nil {
						_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
					} else if s.isOpenAIAccountRuntimeBlocked(account) {
						_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
					} else if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, account, requestedModel, requireCompact) {
						_ = s.deleteStickySessionAccountID(ctx, groupID, sessionHash)
					} else {
						result, err := s.tryAcquireAccountSlot(ctx, accountID, account.Concurrency)
						if err == nil && result != nil && result.Acquired {
							selection, selectErr := s.newAcquiredSelectionResult(ctx, account, result.ReleaseFunc)
							if selectErr != nil {
								return nil, selectErr
							}
							_ = s.refreshStickySessionTTL(ctx, groupID, sessionHash, openaiStickySessionTTL)
							return selection, nil
						}

						waitingCount, _ := s.concurrencyService.GetAccountWaitingCount(ctx, accountID)
						if waitingCount < cfg.StickySessionMaxWaiting {
							return s.newSelectionResult(ctx, account, false, nil, &AccountWaitPlan{
								AccountID:      accountID,
								MaxConcurrency: account.Concurrency,
								Timeout:        cfg.StickySessionWaitTimeout,
								MaxWaiting:     cfg.StickySessionMaxWaiting,
							})
						}
					}
				}
			}
		}
	}

	// ============ Layer 2: Load-aware selection ============
	baseCandidateCount := 0
	candidates := make([]*Account, 0, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		if isExcluded(acc.ID) {
			continue
		}
		// Scheduler snapshots can be temporarily stale (bucket rebuild is throttled);
		// re-check schedulability here so recently rate-limited/overloaded accounts
		// are not selected again before the bucket is rebuilt.
		if !isOpenAIAccountEligibleForRequest(ctx, acc, requestedModel, false, requiredCapability, platform) {
			continue
		}
		if s.isOpenAIAccountRuntimeBlocked(acc) {
			continue
		}
		if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, acc, requestedModel, requireCompact) {
			continue
		}
		baseCandidateCount++
		candidates = append(candidates, acc)
	}

	if len(candidates) == 0 {
		return nil, ErrNoAvailableAccounts
	}

	accountLoads := make([]AccountWithConcurrency, 0, len(candidates))
	for _, acc := range candidates {
		accountLoads = append(accountLoads, AccountWithConcurrency{
			ID:             acc.ID,
			MaxConcurrency: acc.EffectiveLoadFactor(),
		})
	}

	tryAcquireFromLoadMap := func(loadMap map[int64]*AccountLoadInfo) (*AccountSelectionResult, bool, error) {
		var available []accountWithLoad
		for _, acc := range candidates {
			loadInfo := loadMap[acc.ID]
			if loadInfo == nil {
				loadInfo = &AccountLoadInfo{AccountID: acc.ID}
			}
			if loadInfo.LoadRate < 100 {
				available = append(available, accountWithLoad{
					account:  acc,
					loadInfo: loadInfo,
				})
			}
		}

		if len(available) == 0 {
			return nil, false, nil
		}

		sort.SliceStable(available, func(i, j int) bool {
			a, b := available[i], available[j]
			if a.account.Priority != b.account.Priority {
				return a.account.Priority < b.account.Priority
			}
			if a.loadInfo.LoadRate != b.loadInfo.LoadRate {
				return a.loadInfo.LoadRate < b.loadInfo.LoadRate
			}
			switch {
			case a.account.LastUsedAt == nil && b.account.LastUsedAt != nil:
				return true
			case a.account.LastUsedAt != nil && b.account.LastUsedAt == nil:
				return false
			case a.account.LastUsedAt == nil && b.account.LastUsedAt == nil:
				return false
			default:
				return a.account.LastUsedAt.Before(*b.account.LastUsedAt)
			}
		})
		shuffleWithinSortGroups(available)

		selectionOrder := make([]accountWithLoad, 0, len(available))
		if requireCompact {
			appendTier := func(out []accountWithLoad, tier int) []accountWithLoad {
				for _, item := range available {
					if openAICompactSupportTier(item.account) == tier {
						out = append(out, item)
					}
				}
				return out
			}
			selectionOrder = appendTier(selectionOrder, 2)
			selectionOrder = appendTier(selectionOrder, 1)
			// tier 0 候选作为兜底追加：DB recheck 时若发现 cache tier 0 实际
			// 已升级为 1/2（探测刚跑完，cache 尚未刷新），仍可正常命中。
			selectionOrder = appendTier(selectionOrder, 0)
		} else {
			selectionOrder = append(selectionOrder, available...)
		}

		for _, item := range selectionOrder {
			fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, item.account, requestedModel, false, requiredCapability, platform)
			if fresh == nil {
				continue
			}
			fresh = s.recheckSelectedOpenAIAccountFromDB(ctx, fresh, requestedModel, requireCompact, requiredCapability, platform)
			if fresh == nil {
				continue
			}
			if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, fresh, requestedModel, requireCompact) {
				continue
			}
			result, err := s.tryAcquireAccountSlot(ctx, fresh.ID, fresh.Concurrency)
			if err == nil && result != nil && result.Acquired {
				selection, selectErr := s.newAcquiredSelectionResult(ctx, fresh, result.ReleaseFunc)
				if selectErr != nil {
					return nil, true, selectErr
				}
				if sessionHash != "" {
					_ = s.setStickySessionAccountID(ctx, groupID, sessionHash, fresh.ID, openaiStickySessionTTL)
				}
				return selection, true, nil
			}
		}
		return nil, true, nil
	}

	loadMap, err := s.concurrencyService.GetAccountsLoadBatch(ctx, accountLoads)
	if err != nil {
		ordered := append([]*Account(nil), candidates...)
		sortAccountsByPriorityAndLastUsed(ordered, false)
		if requireCompact {
			ordered = prioritizeOpenAICompactAccounts(ordered)
		}
		for _, acc := range ordered {
			fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, acc, requestedModel, false, requiredCapability, platform)
			if fresh == nil {
				continue
			}
			fresh = s.recheckSelectedOpenAIAccountFromDB(ctx, fresh, requestedModel, requireCompact, requiredCapability, platform)
			if fresh == nil {
				continue
			}
			if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, fresh, requestedModel, requireCompact) {
				continue
			}
			result, err := s.tryAcquireAccountSlot(ctx, fresh.ID, fresh.Concurrency)
			if err == nil && result != nil && result.Acquired {
				selection, selectErr := s.newAcquiredSelectionResult(ctx, fresh, result.ReleaseFunc)
				if selectErr != nil {
					return nil, selectErr
				}
				if sessionHash != "" {
					_ = s.setStickySessionAccountID(ctx, groupID, sessionHash, fresh.ID, openaiStickySessionTTL)
				}
				return selection, nil
			}
		}
	} else {
		if selection, attempted, selectErr := tryAcquireFromLoadMap(loadMap); selectErr != nil {
			return nil, selectErr
		} else if selection != nil {
			return selection, nil
		} else if attempted {
			if freshLoadMap, loadErr := s.concurrencyService.GetAccountsLoadBatchFresh(ctx, accountLoads); loadErr == nil {
				if selection, _, selectErr := tryAcquireFromLoadMap(freshLoadMap); selectErr != nil {
					return nil, selectErr
				} else if selection != nil {
					return selection, nil
				}
			}
		}
	}

	// ============ Layer 3: Fallback wait ============
	sortAccountsByPriorityAndLastUsed(candidates, false)
	if requireCompact {
		candidates = prioritizeOpenAICompactAccounts(candidates)
	}
	for _, acc := range candidates {
		fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, acc, requestedModel, false, requiredCapability, platform)
		if fresh == nil {
			continue
		}
		fresh = s.recheckSelectedOpenAIAccountFromDB(ctx, fresh, requestedModel, requireCompact, requiredCapability, platform)
		if fresh == nil {
			continue
		}
		if needsUpstreamCheck && s.isUpstreamModelRestrictedByChannel(ctx, *groupID, fresh, requestedModel, requireCompact) {
			continue
		}
		return s.newSelectionResult(ctx, fresh, false, nil, &AccountWaitPlan{
			AccountID:      fresh.ID,
			MaxConcurrency: fresh.Concurrency,
			Timeout:        cfg.FallbackWaitTimeout,
			MaxWaiting:     cfg.FallbackMaxWaiting,
		})
	}

	if requireCompact && baseCandidateCount > 0 {
		return nil, ErrNoAvailableCompactAccounts
	}
	return nil, ErrNoAvailableAccounts
}

func (s *OpenAIGatewayService) listSchedulableAccounts(ctx context.Context, groupID *int64, platform string) ([]Account, error) {
	platform = normalizeOpenAICompatiblePlatform(platform)
	if s.schedulerSnapshot != nil {
		accounts, _, err := s.schedulerSnapshot.ListSchedulableAccounts(ctx, groupID, platform, false)
		if err != nil {
			return nil, err
		}
		// Compatibility for snapshots written before Custom routing metadata
		// (extra.protocol / extra.relay_mode) was retained in slim account
		// records. Fall back before filtering: another platform account may survive
		// protocol filtering and only fail later on model/capability checks, otherwise
		// hiding the stale Custom account and still producing a false 503.
		for i := range accounts {
			if accounts[i].IsCustom() && accounts[i].CustomProtocol() == "" && s.accountRepo != nil {
				return s.listSchedulableAccountsFromDB(ctx, groupID, platform)
			}
		}
		return filterAccountsByRequestProtocol(ctx, accounts), nil
	}
	return s.listSchedulableAccountsFromDB(ctx, groupID, platform)
}

func (s *OpenAIGatewayService) listSchedulableAccountsFromDB(ctx context.Context, groupID *int64, platform string) ([]Account, error) {
	platform = normalizeOpenAICompatiblePlatform(platform)
	if platform != PlatformGrok && groupID != nil && IsMessageProtocol(InboundProtocolFromContext(ctx)) {
		accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
		if err != nil {
			return nil, fmt.Errorf("query accounts failed: %w", err)
		}
		return filterAccountsByRequestProtocol(ctx, accounts), nil
	}
	// 有消息协议入站的网关请求可调度所有消息协议账号；无入站协议的内部/旧调用
	// 保持单平台查询，避免把 Embeddings/Images 等专用路径升级成多平台候选。
	queryPlatforms := []string{platform}
	if IsMessageProtocol(InboundProtocolFromContext(ctx)) {
		queryPlatforms = schedulingQueryPlatformsForRequest(ctx, platform, false)
	}
	var accounts []Account
	var err error
	if len(queryPlatforms) == 1 {
		if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			accounts, err = s.accountRepo.ListSchedulableByPlatform(ctx, queryPlatforms[0])
		} else if groupID != nil {
			accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, queryPlatforms[0])
		} else {
			accounts, err = s.accountRepo.ListSchedulableUngroupedByPlatform(ctx, queryPlatforms[0])
		}
	} else if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		accounts, err = s.accountRepo.ListSchedulableByPlatforms(ctx, queryPlatforms)
	} else if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, queryPlatforms)
	} else {
		accounts, err = s.accountRepo.ListSchedulableUngroupedByPlatforms(ctx, queryPlatforms)
	}
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	return filterAccountsByRequestProtocol(ctx, accounts), nil
}

func (s *OpenAIGatewayService) tryAcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int) (*AcquireResult, error) {
	if s.concurrencyService == nil {
		return &AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	return s.concurrencyService.AcquireAccountSlot(ctx, accountID, maxConcurrency)
}

func (s *OpenAIGatewayService) resolveFreshSchedulableOpenAIAccount(ctx context.Context, account *Account, requestedModel string, requireCompact bool, requiredCapability OpenAIEndpointCapability, platform string) *Account {
	if account == nil {
		return nil
	}

	fresh := account
	if s.schedulerSnapshot != nil {
		current, err := s.getSchedulableAccount(ctx, account.ID)
		if err == nil && current != nil {
			fresh = current
		}
	}

	if !isOpenAIAccountEligibleForRequest(ctx, fresh, requestedModel, requireCompact, requiredCapability, platform) {
		if s.schedulerSnapshot == nil || s.accountRepo == nil {
			return nil
		}
		latest, err := s.accountRepo.GetByID(ctx, account.ID)
		if err != nil || latest == nil {
			return nil
		}
		if !isOpenAIAccountEligibleForRequest(ctx, latest, requestedModel, requireCompact, requiredCapability, platform) {
			return nil
		}
		if s.isOpenAIAccountRuntimeBlocked(latest) {
			return nil
		}
		_ = s.schedulerSnapshot.UpdateAccountInCache(ctx, latest)
		return latest
	}
	if s.isOpenAIAccountRuntimeBlocked(fresh) {
		return nil
	}
	return fresh
}

func (s *OpenAIGatewayService) recheckSelectedOpenAIAccountFromDB(ctx context.Context, account *Account, requestedModel string, requireCompact bool, requiredCapability OpenAIEndpointCapability, platform string) *Account {
	if account == nil {
		return nil
	}
	if s.schedulerSnapshot == nil || s.accountRepo == nil {
		if !isOpenAIAccountEligibleForRequest(ctx, account, requestedModel, requireCompact, requiredCapability, platform) {
			return nil
		}
		return account
	}

	latest, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || latest == nil {
		return nil
	}
	if !isOpenAIAccountEligibleForRequest(ctx, latest, requestedModel, requireCompact, requiredCapability, platform) {
		return nil
	}
	if s.isOpenAIAccountRuntimeBlocked(latest) {
		return nil
	}
	return latest
}

func (s *OpenAIGatewayService) getSchedulableAccount(ctx context.Context, accountID int64) (*Account, error) {
	var (
		account *Account
		err     error
	)
	if s.schedulerSnapshot != nil {
		account, err = s.schedulerSnapshot.GetAccount(ctx, accountID)
	} else {
		account, err = s.accountRepo.GetByID(ctx, accountID)
	}
	if err != nil || account == nil {
		return account, err
	}
	return account, nil
}

func (s *OpenAIGatewayService) hydrateSelectedAccount(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || s.schedulerSnapshot == nil {
		return account, nil
	}
	hydrated, err := s.schedulerSnapshot.GetAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if hydrated == nil {
		return nil, fmt.Errorf("selected openai account %d not found during hydration", account.ID)
	}
	return hydrated, nil
}

func (s *OpenAIGatewayService) newSelectionResult(ctx context.Context, account *Account, acquired bool, release func(), waitPlan *AccountWaitPlan) (*AccountSelectionResult, error) {
	hydrated, err := s.hydrateSelectedAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	return &AccountSelectionResult{
		Account:     hydrated,
		Acquired:    acquired,
		ReleaseFunc: release,
		WaitPlan:    waitPlan,
	}, nil
}

func (s *OpenAIGatewayService) newAcquiredSelectionResult(ctx context.Context, account *Account, release func()) (*AccountSelectionResult, error) {
	selection, err := s.newSelectionResult(ctx, account, true, release, nil)
	if err != nil && release != nil {
		release()
	}
	return selection, err
}

func (s *OpenAIGatewayService) schedulingConfig() config.GatewaySchedulingConfig {
	if s.cfg != nil {
		return s.cfg.Gateway.Scheduling
	}
	return config.GatewaySchedulingConfig{
		StickySessionMaxWaiting:  3,
		StickySessionWaitTimeout: 45 * time.Second,
		FallbackWaitTimeout:      30 * time.Second,
		FallbackMaxWaiting:       100,
		LoadBatchEnabled:         true,
		SlotCleanupInterval:      30 * time.Second,
	}
}

// GetAccessToken gets the access token for an OpenAI account
func (s *OpenAIGatewayService) GetAccessToken(ctx context.Context, account *Account) (string, string, error) {
	if account != nil && account.IsGrok() {
		if account.Type != AccountTypeOAuth {
			return "", "", fmt.Errorf("unsupported grok account type: %s", account.Type)
		}
		if s.grokTokenProvider != nil {
			accessToken, err := s.grokTokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return "", "", err
			}
			return accessToken, "oauth", nil
		}
		accessToken := account.GetGrokAccessToken()
		if accessToken == "" {
			return "", "", errors.New("grok access_token not found in credentials")
		}
		return accessToken, "oauth", nil
	}

	switch account.Type {
	case AccountTypeOAuth:
		// 使用 TokenProvider 获取缓存的 token
		if s.openAITokenProvider != nil {
			accessToken, err := s.openAITokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return "", "", err
			}
			return accessToken, "oauth", nil
		}
		// 降级：TokenProvider 未配置时直接从账号读取
		accessToken := account.GetOpenAIAccessToken()
		if accessToken == "" {
			return "", "", errors.New("access_token not found in credentials")
		}
		return accessToken, "oauth", nil
	case AccountTypeAPIKey:
		apiKey := account.GetOpenAIApiKey()
		if apiKey == "" {
			return "", "", errors.New("api_key not found in credentials")
		}
		return apiKey, "apikey", nil
	default:
		return "", "", fmt.Errorf("unsupported account type: %s", account.Type)
	}
}

func (s *OpenAIGatewayService) shouldFailoverUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 402, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

func (s *OpenAIGatewayService) shouldFailoverOpenAIUpstreamResponse(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if s.shouldFailoverUpstreamError(statusCode) {
		return true
	}
	return isOpenAITransientProcessingError(statusCode, upstreamMsg, upstreamBody)
}

func (s *OpenAIGatewayService) handleFailoverSideEffects(ctx context.Context, resp *http.Response, account *Account, requestedModel ...string) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if len(requestedModel) > 0 {
		s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, requestedModel[0])
		return
	}
	s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body)
}
