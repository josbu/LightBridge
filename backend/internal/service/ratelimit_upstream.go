package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

// HandleUpstreamError 处理上游错误响应，标记账号状态
// 返回是否应该停止该账号的调度
func (s *RateLimitService) HandleUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte, requestedModel ...string) (shouldDisable bool) {
	customErrorCodesEnabled := account.IsCustomErrorCodesEnabled()

	// 池模式默认不标记本地账号状态；仅当用户显式配置自定义错误码时按本地策略处理。
	if account.IsPoolMode() && !customErrorCodesEnabled {
		slog.Info("pool_mode_error_skipped", "account_id", account.ID, "status_code", statusCode)
		return false
	}

	// apikey 类型账号：检查自定义错误码配置
	// 如果启用且错误码不在列表中，则不处理（不停止调度、不标记限流/过载）
	if !account.ShouldHandleErrorCode(statusCode) {
		slog.Info("account_error_code_skipped", "account_id", account.ID, "status_code", statusCode)
		return false
	}

	if len(requestedModel) > 0 && s.HandleUpstreamModelNotFound(ctx, account, requestedModel[0], statusCode, responseBody) {
		return true
	}

	if statusCode == http.StatusTooManyRequests && account.Platform == PlatformAnthropic {
		fableLimited := s.persistAnthropicFableWindowLimit(ctx, account, headers)
		if s.persistAnthropicExhaustedWindowLimit(ctx, account, headers) {
			return false
		}
		if fableLimited {
			return false
		}
	}

	// 先尝试临时不可调度规则（401除外）
	// 如果匹配成功，直接返回，不执行后续禁用逻辑
	if statusCode != 401 {
		if s.tryTempUnschedulable(ctx, account, statusCode, responseBody) {
			return true
		}
	}

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(responseBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg != "" {
		upstreamMsg = truncateForLog([]byte(upstreamMsg), 512)
	}

	switch statusCode {
	case 400:
		// "organization has been disabled" → 永久禁用
		if strings.Contains(strings.ToLower(upstreamMsg), "organization has been disabled") {
			msg := "Organization disabled (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		} else if account.Platform == PlatformAnthropic && strings.Contains(strings.ToLower(upstreamMsg), "credit balance") {
			// Anthropic API key 余额不足（语义等同 402），停止调度
			msg := "Credit balance exhausted (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		} else if strings.Contains(strings.ToLower(upstreamMsg), "identity verification is required") {
			// KYC 身份验证要求 → 永久禁用，账号需完成身份验证后才能恢复
			msg := "Identity verification required (400): " + upstreamMsg
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		}
		// 其他 400 错误（如参数问题）不处理，不禁用账号
	case 401:
		// OpenAI: token_invalidated / token_revoked 表示 token 被永久作废（非过期），直接标记 error
		openai401Code := extractUpstreamErrorCode(responseBody)
		if account.Platform == PlatformOpenAI && (openai401Code == "token_invalidated" || openai401Code == "token_revoked") {
			msg := "Token revoked (401): account authentication permanently revoked"
			if upstreamMsg != "" {
				msg = "Token revoked (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		// OpenAI: {"detail":"Unauthorized"} 表示 token 完全无效（非标准 OpenAI 错误格式），直接标记 error
		if account.Platform == PlatformOpenAI && gjson.GetBytes(responseBody, "detail").String() == "Unauthorized" {
			msg := "Unauthorized (401): account authentication failed permanently"
			if upstreamMsg != "" {
				msg = "Unauthorized (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		// OAuth 账号在 401 错误时临时不可调度（给 token 刷新窗口）；非 OAuth 账号保持原有 SetError 行为。
		// Antigravity 除外：其 401 由 applyErrorPolicy 的 temp_unschedulable_rules 自行控制。
		if account.Type == AccountTypeOAuth && !account.IsAntigravity() {
			// 1. 失效缓存
			if s.tokenCacheInvalidator != nil {
				if err := s.tokenCacheInvalidator.InvalidateToken(ctx, account); err != nil {
					slog.Warn("oauth_401_invalidate_cache_failed", "account_id", account.ID, "error", err)
				}
			}
			// 缺少 refresh_token 的 OAuth 账号无法在冷却期内自愈（后台刷新服务也会跳过），
			// 直接走 SetError 永久禁用，避免冷却结束后再被选中产生一发无意义的 502。
			if strings.TrimSpace(account.GetCredential("refresh_token")) == "" {
				msg := "Authentication failed (401): refresh_token missing, cannot recover"
				if upstreamMsg != "" {
					msg = "OAuth 401 (no refresh_token): " + upstreamMsg
				}
				s.handleAuthError(ctx, account, msg)
				shouldDisable = true
				break
			}
			// 2. 临时不可调度，替代 SetError（保持 status=active 让刷新服务能拾取）
			// 注意：此处不再写回 account.Credentials/expires_at。
			// 原实现使用请求开始时的 account 快照整列覆盖 credentials JSONB（见
			// persistAccountCredentials → accountRepository.UpdateCredentials → SetCredentials），
			// 在另一个 worker 刚刷新完 refresh_token 的窄窗口内会把新 refresh_token 回滚为旧值，
			// 导致下一周期用旧 refresh_token 调上游拿到 invalid_grant 后，
			// tryRecoverFromRefreshRace 重读 DB 发现 currentRT == usedRT 也救不回来，账号被错误 disable。
			// 这里仅依赖 InvalidateToken + SetTempUnschedulable 让账号在冷却期内不被调度，
			// 冷却结束后由 token_provider 的 NeedsRefresh / token_refresh_service 走带分布式锁的正路刷新。
			msg := "Authentication failed (401): invalid or expired credentials"
			if upstreamMsg != "" {
				msg = "OAuth 401: " + upstreamMsg
			}
			cooldownMinutes := s.cfg.RateLimit.OAuth401CooldownMinutes
			if cooldownMinutes <= 0 {
				cooldownMinutes = 10
			}
			until := time.Now().Add(time.Duration(cooldownMinutes) * time.Minute)
			s.notifyAccountSchedulingBlocked(account, until, "oauth_401")
			if err := s.accountRepo.SetTempUnschedulable(ctx, account.ID, until, msg); err != nil {
				slog.Warn("oauth_401_set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
			}
			shouldDisable = true
		} else {
			// 非 OAuth / Antigravity OAuth：保持 SetError 行为
			msg := "Authentication failed (401): invalid or expired credentials"
			if upstreamMsg != "" {
				msg = "Authentication failed (401): " + upstreamMsg
			}
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
		}
	case 402:
		// OpenAI: deactivated_workspace 表示工作区已停用，直接标记 error
		if account.Platform == PlatformOpenAI && gjson.GetBytes(responseBody, "detail.code").String() == "deactivated_workspace" {
			msg := "Workspace deactivated (402): workspace has been deactivated"
			s.handleAuthError(ctx, account, msg)
			shouldDisable = true
			break
		}
		// 支付要求：余额不足或计费问题，停止调度
		msg := "Payment required (402): insufficient balance or billing issue"
		if upstreamMsg != "" {
			msg = "Payment required (402): " + upstreamMsg
		}
		s.handleAuthError(ctx, account, msg)
		shouldDisable = true
	case 403:
		logger.LegacyPrintf(
			"service.ratelimit",
			"[HandleUpstreamErrorRaw] account_id=%d platform=%s type=%s status=403 request_id=%s cf_ray=%s upstream_msg=%s raw_body=%s",
			account.ID,
			account.Platform,
			account.Type,
			strings.TrimSpace(headers.Get("x-request-id")),
			strings.TrimSpace(headers.Get("cf-ray")),
			upstreamMsg,
			truncateForLog(responseBody, 1024),
		)
		shouldDisable = s.handle403(ctx, account, upstreamMsg, responseBody)
	case 429:
		s.handle429(ctx, account, headers, responseBody)
		shouldDisable = false
	case 529:
		s.handle529(ctx, account)
		shouldDisable = false
	default:
		// 自定义错误码启用时：在列表中的错误码都应该停止调度
		if customErrorCodesEnabled {
			msg := "Custom error code triggered"
			if upstreamMsg != "" {
				msg = upstreamMsg
			}
			s.handleCustomErrorCode(ctx, account, statusCode, msg)
			shouldDisable = true
		} else if statusCode >= 500 {
			// 未启用自定义错误码时：仅记录5xx错误
			slog.Warn("account_upstream_error", "account_id", account.ID, "status_code", statusCode)
			shouldDisable = false
		}
	}

	return shouldDisable
}
