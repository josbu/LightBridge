package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/antigravity"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

const (
	antigravityStickySessionTTL = time.Hour
	antigravityMaxRetries       = 3
	antigravityRetryBaseDelay   = 1 * time.Second
	antigravityRetryMaxDelay    = 16 * time.Second

	// 限流相关常量
	// antigravityRateLimitThreshold 限流等待/切换阈值
	// - 智能重试：retryDelay < 此阈值时等待后重试，>= 此阈值时直接限流模型
	// - 预检查：剩余限流时间 < 此阈值时等待，>= 此阈值时切换账号
	antigravityRateLimitThreshold       = 7 * time.Second
	antigravitySmartRetryMinWait        = 1 * time.Second  // 智能重试最小等待时间
	antigravitySmartRetryMaxAttempts    = 1                // 智能重试最大次数（仅重试 1 次，防止重复限流/长期等待）
	antigravityDefaultRateLimitDuration = 30 * time.Second // 默认限流时间（无 retryDelay 时使用）

	// MODEL_CAPACITY_EXHAUSTED 专用重试参数
	// 模型容量不足时，所有账号共享同一容量池，切换账号无意义
	// 使用固定 1s 间隔重试，最多重试 60 次
	antigravityModelCapacityRetryMaxAttempts = 60
	antigravityModelCapacityRetryWait        = 1 * time.Second

	// Google RPC 状态和类型常量
	googleRPCStatusResourceExhausted      = "RESOURCE_EXHAUSTED"
	googleRPCStatusUnavailable            = "UNAVAILABLE"
	googleRPCTypeRetryInfo                = "type.googleapis.com/google.rpc.RetryInfo"
	googleRPCTypeErrorInfo                = "type.googleapis.com/google.rpc.ErrorInfo"
	googleRPCReasonModelCapacityExhausted = "MODEL_CAPACITY_EXHAUSTED"
	googleRPCReasonRateLimitExceeded      = "RATE_LIMIT_EXCEEDED"

	// 单账号 503 退避重试：Service 层原地重试的最大次数
	// 在 handleSmartRetry 中，对于 shouldRateLimitModel（长延迟 ≥ 7s）的情况，
	// 多账号模式下会设限流+切换账号；但单账号模式下改为原地等待+重试。
	antigravitySingleAccountSmartRetryMaxAttempts = 3

	// 单账号 503 退避重试：原地重试时单次最大等待时间
	// 防止上游返回过长的 retryDelay 导致请求卡住太久
	antigravitySingleAccountSmartRetryMaxWait = 15 * time.Second

	// 单账号 503 退避重试：原地重试的总累计等待时间上限
	// 超过此上限将不再重试，直接返回 503
	antigravitySingleAccountSmartRetryTotalMaxWait = 30 * time.Second

	// MODEL_CAPACITY_EXHAUSTED 全局去重：重试全部失败后的 cooldown 时间
	antigravityModelCapacityCooldown = 10 * time.Second
)

// antigravityPassthroughErrorMessages 透传给客户端的错误消息白名单（小写）
// 匹配时使用 strings.Contains，无需完全匹配
var antigravityPassthroughErrorMessages = []string{
	"prompt is too long",
}

// MODEL_CAPACITY_EXHAUSTED 全局去重：避免多个并发请求同时对同一模型进行容量耗尽重试
var (
	modelCapacityExhaustedMu    sync.RWMutex
	modelCapacityExhaustedUntil = make(map[string]time.Time) // modelName -> cooldown until
)

const (
	antigravityForwardBaseURLEnv  = "GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL"
	antigravityFallbackSecondsEnv = "GATEWAY_ANTIGRAVITY_FALLBACK_COOLDOWN_SECONDS"
)

// AntigravityAccountSwitchError 账号切换信号
// 当账号限流时间超过阈值时，通知上层切换账号
type AntigravityAccountSwitchError struct {
	OriginalAccountID int64
	RateLimitedModel  string
	IsStickySession   bool // 是否为粘性会话切换（决定是否缓存计费）
}

func (e *AntigravityAccountSwitchError) Error() string {
	return fmt.Sprintf("account %d model %s rate limited, need switch",
		e.OriginalAccountID, e.RateLimitedModel)
}

// IsAntigravityAccountSwitchError 检查错误是否为账号切换信号
func IsAntigravityAccountSwitchError(err error) (*AntigravityAccountSwitchError, bool) {
	var switchErr *AntigravityAccountSwitchError
	if errors.As(err, &switchErr) {
		return switchErr, true
	}
	return nil, false
}

// PromptTooLongError 表示上游明确返回 prompt too long
type PromptTooLongError struct {
	StatusCode int
	RequestID  string
	Body       []byte
}

func (e *PromptTooLongError) Error() string {
	return fmt.Sprintf("prompt too long: status=%d", e.StatusCode)
}

// antigravityRetryLoopParams 重试循环的参数
type antigravityRetryLoopParams struct {
	ctx             context.Context
	prefix          string
	account         *Account
	proxyURL        string
	accessToken     string
	action          string
	body            []byte
	c               *gin.Context
	httpUpstream    HTTPUpstream
	settingService  *SettingService
	accountRepo     AccountRepository // 用于智能重试的模型级别限流
	handleError     func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, requestedModel string, groupID int64, sessionHash string, isStickySession bool) *handleModelRateLimitResult
	requestedModel  string // 用于限流检查的原始请求模型
	isStickySession bool   // 是否为粘性会话（用于账号切换时的缓存计费判断）
	groupID         int64  // 用于模型级限流时清除粘性会话
	sessionHash     string // 用于模型级限流时清除粘性会话
}

// antigravityRetryLoopResult 重试循环的结果
type antigravityRetryLoopResult struct {
	resp *http.Response
}

// resolveAntigravityForwardBaseURL 解析转发用 base URL。
// 默认使用 daily（ForwardBaseURLs 的首个地址）；当环境变量为 prod 时使用第二个地址。
func resolveAntigravityForwardBaseURL() string {
	baseURLs := antigravity.ForwardBaseURLs()
	if len(baseURLs) == 0 {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(antigravityForwardBaseURLEnv)))
	if mode == "prod" && len(baseURLs) > 1 {
		return baseURLs[1]
	}
	return baseURLs[0]
}

// smartRetryAction 智能重试的处理结果
type smartRetryAction int

const (
	smartRetryActionContinue      smartRetryAction = iota // 继续默认重试逻辑
	smartRetryActionBreakWithResp                         // 结束循环并返回 resp
	smartRetryActionContinueURL                           // 继续 URL fallback 循环
)

// smartRetryResult 智能重试的结果
type smartRetryResult struct {
	action      smartRetryAction
	resp        *http.Response
	err         error
	switchError *AntigravityAccountSwitchError // 模型限流时返回账号切换信号
}

// handleSmartRetry 处理 OAuth 账号的智能重试逻辑
// 将 429/503 限流处理逻辑抽取为独立函数，减少 antigravityRetryLoop 的复杂度
func (s *AntigravityGatewayService) handleSmartRetry(p antigravityRetryLoopParams, resp *http.Response, respBody []byte, baseURL string, urlIdx int, availableURLs []string) *smartRetryResult {
	// "Resource has been exhausted" 是 URL 级别限流，切换 URL（仅 429）
	if resp.StatusCode == http.StatusTooManyRequests && isURLLevelRateLimit(respBody) && urlIdx < len(availableURLs)-1 {
		logger.LegacyPrintf("service.antigravity_gateway", "%s URL fallback (429): %s -> %s", p.prefix, baseURL, availableURLs[urlIdx+1])
		return &smartRetryResult{action: smartRetryActionContinueURL}
	}

	category := antigravity429Unknown
	if resp.StatusCode == http.StatusTooManyRequests {
		category = classifyAntigravity429(respBody)
	}

	// 判断是否触发智能重试
	shouldSmartRetry, shouldRateLimitModel, waitDuration, modelName, isModelCapacityExhausted := shouldTriggerAntigravitySmartRetry(p.account, respBody)

	// AI Credits 超量请求：
	// 仅在上游明确返回免费配额耗尽时才允许切换到 credits。
	if resp.StatusCode == http.StatusTooManyRequests &&
		category == antigravity429QuotaExhausted &&
		p.account.IsOveragesEnabled() &&
		!p.account.isCreditsExhausted() {
		result := s.attemptCreditsOveragesRetry(p, baseURL, modelName, waitDuration, resp.StatusCode, respBody)
		if result.handled && result.resp != nil {
			return &smartRetryResult{
				action: smartRetryActionBreakWithResp,
				resp:   result.resp,
			}
		}
	}

	// 情况1: retryDelay >= 阈值，限流模型并切换账号
	if shouldRateLimitModel {
		// 单账号 503 退避重试模式：不设限流、不切换账号，改为原地等待+重试
		// 谷歌上游 503 (MODEL_CAPACITY_EXHAUSTED) 通常是暂时性的，等几秒就能恢复。
		// 多账号场景下切换账号是最优选择，但单账号场景下设限流毫无意义（只会导致双重等待）。
		if resp.StatusCode == http.StatusServiceUnavailable && isSingleAccountRetry(p.ctx) {
			return s.handleSingleAccountRetryInPlace(p, resp, respBody, baseURL, waitDuration, modelName)
		}

		rateLimitDuration := waitDuration
		if rateLimitDuration <= 0 {
			rateLimitDuration = antigravityDefaultRateLimitDuration
		}
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d oauth_long_delay model=%s account=%d upstream_retry_delay=%v body=%s (model rate limit, switch account)",
			p.prefix, resp.StatusCode, modelName, p.account.ID, rateLimitDuration, truncateForLog(respBody, 200))

		resetAt := time.Now().Add(rateLimitDuration)
		if !setModelRateLimitByModelName(p.ctx, p.accountRepo, p.account.ID, modelName, p.prefix, resp.StatusCode, resetAt, false) {
			p.handleError(p.ctx, p.prefix, p.account, resp.StatusCode, resp.Header, respBody, p.requestedModel, p.groupID, p.sessionHash, p.isStickySession)
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d rate_limited account=%d (no model mapping)", p.prefix, resp.StatusCode, p.account.ID)
		} else {
			s.updateAccountModelRateLimitInCache(p.ctx, p.account, modelName, resetAt)
		}

		// 返回账号切换信号，让上层切换账号重试
		return &smartRetryResult{
			action: smartRetryActionBreakWithResp,
			switchError: &AntigravityAccountSwitchError{
				OriginalAccountID: p.account.ID,
				RateLimitedModel:  modelName,
				IsStickySession:   p.isStickySession,
			},
		}
	}

	// 情况2: retryDelay < 阈值（或 MODEL_CAPACITY_EXHAUSTED），智能重试
	if shouldSmartRetry {
		var lastRetryResp *http.Response
		var lastRetryBody []byte

		// MODEL_CAPACITY_EXHAUSTED 使用独立的重试参数（60 次，固定 1s 间隔）
		maxAttempts := antigravitySmartRetryMaxAttempts
		if isModelCapacityExhausted {
			maxAttempts = antigravityModelCapacityRetryMaxAttempts
			waitDuration = antigravityModelCapacityRetryWait

			// 全局去重：如果其他 goroutine 已在重试同一模型且尚在 cooldown 中，直接返回 503
			if modelName != "" {
				modelCapacityExhaustedMu.RLock()
				cooldownUntil, exists := modelCapacityExhaustedUntil[modelName]
				modelCapacityExhaustedMu.RUnlock()
				if exists && time.Now().Before(cooldownUntil) {
					log.Printf("%s status=%d model_capacity_exhausted_dedup model=%s account=%d cooldown_until=%v (skip retry)",
						p.prefix, resp.StatusCode, modelName, p.account.ID, cooldownUntil.Format("15:04:05"))
					return &smartRetryResult{
						action: smartRetryActionBreakWithResp,
						resp: &http.Response{
							StatusCode: resp.StatusCode,
							Header:     resp.Header.Clone(),
							Body:       io.NopCloser(bytes.NewReader(respBody)),
						},
					}
				}
			}
		}

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			log.Printf("%s status=%d oauth_smart_retry attempt=%d/%d delay=%v model=%s account=%d",
				p.prefix, resp.StatusCode, attempt, maxAttempts, waitDuration, modelName, p.account.ID)

			timer := time.NewTimer(waitDuration)
			select {
			case <-p.ctx.Done():
				timer.Stop()
				log.Printf("%s status=context_canceled_during_smart_retry", p.prefix)
				return &smartRetryResult{action: smartRetryActionBreakWithResp, err: p.ctx.Err()}
			case <-timer.C:
			}

			// 智能重试：创建新请求
			retryReq, err := antigravity.NewAPIRequestWithURL(p.ctx, baseURL, p.action, p.accessToken, p.body)
			if err != nil {
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=smart_retry_request_build_failed error=%v", p.prefix, err)
				p.handleError(p.ctx, p.prefix, p.account, resp.StatusCode, resp.Header, respBody, p.requestedModel, p.groupID, p.sessionHash, p.isStickySession)
				return &smartRetryResult{
					action: smartRetryActionBreakWithResp,
					resp: &http.Response{
						StatusCode: resp.StatusCode,
						Header:     resp.Header.Clone(),
						Body:       io.NopCloser(bytes.NewReader(respBody)),
					},
				}
			}

			retryResp, retryErr := p.httpUpstream.Do(retryReq, p.proxyURL, p.account.ID, p.account.Concurrency)
			if retryErr == nil && retryResp != nil && retryResp.StatusCode != http.StatusTooManyRequests && retryResp.StatusCode != http.StatusServiceUnavailable {
				log.Printf("%s status=%d smart_retry_success attempt=%d/%d", p.prefix, retryResp.StatusCode, attempt, maxAttempts)
				// 重试成功，清除 MODEL_CAPACITY_EXHAUSTED cooldown
				if isModelCapacityExhausted && modelName != "" {
					modelCapacityExhaustedMu.Lock()
					delete(modelCapacityExhaustedUntil, modelName)
					modelCapacityExhaustedMu.Unlock()
				}
				return &smartRetryResult{action: smartRetryActionBreakWithResp, resp: retryResp}
			}

			// 网络错误时，继续重试
			if retryErr != nil || retryResp == nil {
				log.Printf("%s status=smart_retry_network_error attempt=%d/%d error=%v", p.prefix, attempt, maxAttempts, retryErr)
				continue
			}

			// 重试失败，关闭之前的响应
			if lastRetryResp != nil {
				_ = lastRetryResp.Body.Close()
			}
			lastRetryResp = retryResp
			if retryResp != nil {
				lastRetryBody, _ = io.ReadAll(io.LimitReader(retryResp.Body, 8<<10))
				_ = retryResp.Body.Close()
			}

			// 解析新的重试信息，用于下次重试的等待时间（MODEL_CAPACITY_EXHAUSTED 使用固定循环，跳过）
			if !isModelCapacityExhausted && attempt < maxAttempts && lastRetryBody != nil {
				newShouldRetry, _, newWaitDuration, _, _ := shouldTriggerAntigravitySmartRetry(p.account, lastRetryBody)
				if newShouldRetry && newWaitDuration > 0 {
					waitDuration = newWaitDuration
				}
			}
		}

		// 所有重试都失败
		rateLimitDuration := waitDuration
		if rateLimitDuration <= 0 {
			rateLimitDuration = antigravityDefaultRateLimitDuration
		}
		retryBody := lastRetryBody
		if retryBody == nil {
			retryBody = respBody
		}

		// MODEL_CAPACITY_EXHAUSTED：模型容量不足，切换账号无意义
		// 直接返回上游错误响应，不设置模型限流，不切换账号
		if isModelCapacityExhausted {
			// 设置 cooldown，让后续请求快速失败，避免重复重试
			if modelName != "" {
				modelCapacityExhaustedMu.Lock()
				modelCapacityExhaustedUntil[modelName] = time.Now().Add(antigravityModelCapacityCooldown)
				modelCapacityExhaustedMu.Unlock()
			}
			log.Printf("%s status=%d smart_retry_exhausted_model_capacity attempts=%d model=%s account=%d body=%s (model capacity exhausted, not switching account)",
				p.prefix, resp.StatusCode, maxAttempts, modelName, p.account.ID, truncateForLog(retryBody, 200))
			return &smartRetryResult{
				action: smartRetryActionBreakWithResp,
				resp: &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(retryBody)),
				},
			}
		}

		// 单账号 503 退避重试模式：智能重试耗尽后不设限流、不切换账号，
		// 直接返回 503 让 Handler 层的单账号退避循环做最终处理。
		if resp.StatusCode == http.StatusServiceUnavailable && isSingleAccountRetry(p.ctx) {
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d smart_retry_exhausted_single_account attempts=%d model=%s account=%d body=%s (return 503 directly)",
				p.prefix, resp.StatusCode, antigravitySmartRetryMaxAttempts, modelName, p.account.ID, truncateForLog(retryBody, 200))
			return &smartRetryResult{
				action: smartRetryActionBreakWithResp,
				resp: &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(retryBody)),
				},
			}
		}

		log.Printf("%s status=%d smart_retry_exhausted attempts=%d model=%s account=%d upstream_retry_delay=%v body=%s (switch account)",
			p.prefix, resp.StatusCode, maxAttempts, modelName, p.account.ID, rateLimitDuration, truncateForLog(retryBody, 200))

		resetAt := time.Now().Add(rateLimitDuration)
		if p.accountRepo != nil && modelName != "" {
			if err := p.accountRepo.SetModelRateLimit(p.ctx, p.account.ID, modelName, resetAt); err != nil {
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limit_failed model=%s error=%v", p.prefix, resp.StatusCode, modelName, err)
			} else {
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limited_after_smart_retry model=%s account=%d reset_in=%v",
					p.prefix, resp.StatusCode, modelName, p.account.ID, rateLimitDuration)
				s.updateAccountModelRateLimitInCache(p.ctx, p.account, modelName, resetAt)
			}
		}

		// 清除粘性会话绑定，避免下次请求仍命中限流账号
		if s.cache != nil && p.sessionHash != "" {
			_ = s.cache.DeleteSessionAccountID(p.ctx, p.groupID, p.sessionHash)
		}

		// 返回账号切换信号，让上层切换账号重试
		return &smartRetryResult{
			action: smartRetryActionBreakWithResp,
			switchError: &AntigravityAccountSwitchError{
				OriginalAccountID: p.account.ID,
				RateLimitedModel:  modelName,
				IsStickySession:   p.isStickySession,
			},
		}
	}

	// 未触发智能重试，继续默认重试逻辑
	return &smartRetryResult{action: smartRetryActionContinue}
}

// handleSingleAccountRetryInPlace 单账号 503 退避重试的原地重试逻辑。
//
// 在多账号场景下，收到 503 + 长 retryDelay（≥ 7s）时会设置模型限流 + 切换账号；
// 但在单账号场景下，设限流毫无意义（因为切换回来的还是同一个账号，还要等限流过期）。
// 此方法改为在 Service 层原地等待 + 重试，避免双重等待问题：
//
//	旧流程：Service 设限流 → Handler 退避等待 → Service 等限流过期 → 再请求（总耗时 = 退避 + 限流）
//	新流程：Service 直接等 retryDelay → 重试 → 成功/再等 → 重试...（总耗时 ≈ 实际 retryDelay × 重试次数）
//
// 约束：
//   - 单次等待不超过 antigravitySingleAccountSmartRetryMaxWait
//   - 总累计等待不超过 antigravitySingleAccountSmartRetryTotalMaxWait
//   - 最多重试 antigravitySingleAccountSmartRetryMaxAttempts 次
func (s *AntigravityGatewayService) handleSingleAccountRetryInPlace(
	p antigravityRetryLoopParams,
	resp *http.Response,
	respBody []byte,
	baseURL string,
	waitDuration time.Duration,
	modelName string,
) *smartRetryResult {
	// 限制单次等待时间
	if waitDuration > antigravitySingleAccountSmartRetryMaxWait {
		waitDuration = antigravitySingleAccountSmartRetryMaxWait
	}
	if waitDuration < antigravitySmartRetryMinWait {
		waitDuration = antigravitySmartRetryMinWait
	}

	logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d single_account_503_retry_in_place model=%s account=%d upstream_retry_delay=%v (retrying in-place instead of rate-limiting)",
		p.prefix, resp.StatusCode, modelName, p.account.ID, waitDuration)

	var lastRetryResp *http.Response
	var lastRetryBody []byte
	totalWaited := time.Duration(0)

	for attempt := 1; attempt <= antigravitySingleAccountSmartRetryMaxAttempts; attempt++ {
		// 检查累计等待是否超限
		if totalWaited+waitDuration > antigravitySingleAccountSmartRetryTotalMaxWait {
			remaining := antigravitySingleAccountSmartRetryTotalMaxWait - totalWaited
			if remaining <= 0 {
				logger.LegacyPrintf("service.antigravity_gateway", "%s single_account_503_retry: total_wait_exceeded total=%v max=%v, giving up",
					p.prefix, totalWaited, antigravitySingleAccountSmartRetryTotalMaxWait)
				break
			}
			waitDuration = remaining
		}

		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d single_account_503_retry attempt=%d/%d delay=%v total_waited=%v model=%s account=%d",
			p.prefix, resp.StatusCode, attempt, antigravitySingleAccountSmartRetryMaxAttempts, waitDuration, totalWaited, modelName, p.account.ID)

		timer := time.NewTimer(waitDuration)
		select {
		case <-p.ctx.Done():
			timer.Stop()
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=context_canceled_during_single_account_retry", p.prefix)
			return &smartRetryResult{action: smartRetryActionBreakWithResp, err: p.ctx.Err()}
		case <-timer.C:
		}
		totalWaited += waitDuration

		// 创建新请求
		retryReq, err := antigravity.NewAPIRequestWithURL(p.ctx, baseURL, p.action, p.accessToken, p.body)
		if err != nil {
			logger.LegacyPrintf("service.antigravity_gateway", "%s single_account_503_retry: request_build_failed error=%v", p.prefix, err)
			break
		}

		retryResp, retryErr := p.httpUpstream.Do(retryReq, p.proxyURL, p.account.ID, p.account.Concurrency)
		if retryErr == nil && retryResp != nil && retryResp.StatusCode != http.StatusTooManyRequests && retryResp.StatusCode != http.StatusServiceUnavailable {
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d single_account_503_retry_success attempt=%d/%d total_waited=%v",
				p.prefix, retryResp.StatusCode, attempt, antigravitySingleAccountSmartRetryMaxAttempts, totalWaited)
			// 关闭之前的响应
			if lastRetryResp != nil {
				_ = lastRetryResp.Body.Close()
			}
			return &smartRetryResult{action: smartRetryActionBreakWithResp, resp: retryResp}
		}

		// 网络错误时继续重试
		if retryErr != nil || retryResp == nil {
			logger.LegacyPrintf("service.antigravity_gateway", "%s single_account_503_retry: network_error attempt=%d/%d error=%v",
				p.prefix, attempt, antigravitySingleAccountSmartRetryMaxAttempts, retryErr)
			continue
		}

		// 关闭之前的响应
		if lastRetryResp != nil {
			_ = lastRetryResp.Body.Close()
		}
		lastRetryResp = retryResp
		lastRetryBody, _ = io.ReadAll(io.LimitReader(retryResp.Body, 8<<10))
		_ = retryResp.Body.Close()

		// 解析新的重试信息，更新下次等待时间
		if attempt < antigravitySingleAccountSmartRetryMaxAttempts && lastRetryBody != nil {
			_, _, newWaitDuration, _, _ := shouldTriggerAntigravitySmartRetry(p.account, lastRetryBody)
			if newWaitDuration > 0 {
				waitDuration = newWaitDuration
				if waitDuration > antigravitySingleAccountSmartRetryMaxWait {
					waitDuration = antigravitySingleAccountSmartRetryMaxWait
				}
				if waitDuration < antigravitySmartRetryMinWait {
					waitDuration = antigravitySmartRetryMinWait
				}
			}
		}
	}

	// 所有重试都失败，不设限流，直接返回 503
	// Handler 层的单账号退避循环会做最终处理
	retryBody := lastRetryBody
	if retryBody == nil {
		retryBody = respBody
	}
	logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d single_account_503_retry_exhausted attempts=%d total_waited=%v model=%s account=%d body=%s (return 503 directly)",
		p.prefix, resp.StatusCode, antigravitySingleAccountSmartRetryMaxAttempts, totalWaited, modelName, p.account.ID, truncateForLog(retryBody, 200))

	return &smartRetryResult{
		action: smartRetryActionBreakWithResp,
		resp: &http.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       io.NopCloser(bytes.NewReader(retryBody)),
		},
	}
}

// antigravityRetryLoop 执行带 URL fallback 的重试循环
func (s *AntigravityGatewayService) antigravityRetryLoop(p antigravityRetryLoopParams) (*antigravityRetryLoopResult, error) {
	// 预检查：模型限流 + overages 启用 + 积分未耗尽 → 直接注入 AI Credits
	overagesInjected := false
	if p.requestedModel != "" && p.account.IsAntigravity() &&
		p.account.IsOveragesEnabled() && !p.account.isCreditsExhausted() &&
		p.account.isModelRateLimitedWithContext(p.ctx, p.requestedModel) {
		if creditsBody := injectEnabledCreditTypes(p.body); creditsBody != nil {
			p.body = creditsBody
			overagesInjected = true
			logger.LegacyPrintf("service.antigravity_gateway", "%s pre_check: model_rate_limited_credits_inject model=%s account=%d (injecting enabledCreditTypes)",
				p.prefix, p.requestedModel, p.account.ID)
		}
	}

	// 预检查：如果账号已限流，直接返回切换信号
	if p.requestedModel != "" {
		if remaining := p.account.GetRateLimitRemainingTimeWithContext(p.ctx, p.requestedModel); remaining > 0 {
			// 已注入积分的请求不再受普通模型限流预检查阻断。
			if overagesInjected {
				logger.LegacyPrintf("service.antigravity_gateway", "%s pre_check: credits_injected_ignore_rate_limit remaining=%v model=%s account=%d",
					p.prefix, remaining.Truncate(time.Millisecond), p.requestedModel, p.account.ID)
			} else if isSingleAccountRetry(p.ctx) {
				// 单账号 503 退避重试模式：跳过限流预检查，直接发请求。
				// 首次请求设的限流是为了多账号调度器跳过该账号，在单账号模式下无意义。
				// 如果上游确实还不可用，handleSmartRetry → handleSingleAccountRetryInPlace
				// 会在 Service 层原地等待+重试，不需要在预检查这里等。
				logger.LegacyPrintf("service.antigravity_gateway", "%s pre_check: single_account_retry skipping rate_limit remaining=%v model=%s account=%d (will retry in-place if 503)",
					p.prefix, remaining.Truncate(time.Millisecond), p.requestedModel, p.account.ID)
			} else {
				logger.LegacyPrintf("service.antigravity_gateway", "%s pre_check: rate_limit_switch remaining=%v model=%s account=%d",
					p.prefix, remaining.Truncate(time.Millisecond), p.requestedModel, p.account.ID)
				return nil, &AntigravityAccountSwitchError{
					OriginalAccountID: p.account.ID,
					RateLimitedModel:  p.requestedModel,
					IsStickySession:   p.isStickySession,
				}
			}
		}
	}

	baseURL := resolveAntigravityForwardBaseURL()
	if baseURL == "" {
		return nil, errors.New("no antigravity forward base url configured")
	}
	availableURLs := []string{baseURL}

	var resp *http.Response
	var usedBaseURL string
	logBody := p.settingService != nil && p.settingService.cfg != nil && p.settingService.cfg.Gateway.LogUpstreamErrorBody
	maxBytes := 2048
	if p.settingService != nil && p.settingService.cfg != nil && p.settingService.cfg.Gateway.LogUpstreamErrorBodyMaxBytes > 0 {
		maxBytes = p.settingService.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	}
	getUpstreamDetail := func(body []byte) string {
		if !logBody {
			return ""
		}
		return truncateString(string(body), maxBytes)
	}

urlFallbackLoop:
	for urlIdx, baseURL := range availableURLs {
		usedBaseURL = baseURL
		allAttemptsInternal500 := true // 追踪本轮所有 attempt 是否全部命中 INTERNAL 500
		for attempt := 1; attempt <= antigravityMaxRetries; attempt++ {
			select {
			case <-p.ctx.Done():
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=context_canceled error=%v", p.prefix, p.ctx.Err())
				return nil, p.ctx.Err()
			default:
			}

			upstreamReq, err := antigravity.NewAPIRequestWithURL(p.ctx, baseURL, p.action, p.accessToken, p.body)
			if err != nil {
				return nil, err
			}

			resp, err = p.httpUpstream.Do(upstreamReq, p.proxyURL, p.account.ID, p.account.Concurrency)
			if err == nil && resp == nil {
				err = errors.New("upstream returned nil response")
			}
			if err != nil {
				safeErr := sanitizeUpstreamErrorMessage(err.Error())
				appendOpsUpstreamError(p.c, OpsUpstreamErrorEvent{
					Platform:           p.account.Platform,
					AccountID:          p.account.ID,
					AccountName:        p.account.Name,
					UpstreamStatusCode: 0,
					UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
					Kind:               "request_error",
					Message:            safeErr,
				})
				if shouldAntigravityFallbackToNextURL(err, 0) && urlIdx < len(availableURLs)-1 {
					logger.LegacyPrintf("service.antigravity_gateway", "%s URL fallback (connection error): %s -> %s", p.prefix, baseURL, availableURLs[urlIdx+1])
					continue urlFallbackLoop
				}
				if attempt < antigravityMaxRetries {
					logger.LegacyPrintf("service.antigravity_gateway", "%s status=request_failed retry=%d/%d error=%v", p.prefix, attempt, antigravityMaxRetries, err)
					if !sleepAntigravityBackoffWithContext(p.ctx, attempt) {
						logger.LegacyPrintf("service.antigravity_gateway", "%s status=context_canceled_during_backoff", p.prefix)
						return nil, p.ctx.Err()
					}
					continue
				}
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=request_failed retries_exhausted error=%v", p.prefix, err)
				setOpsUpstreamError(p.c, 0, safeErr, "")
				return nil, fmt.Errorf("upstream request failed after retries: %w", err)
			}

			// 统一处理错误响应
			if resp.StatusCode >= 400 {
				respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
				_ = resp.Body.Close()

				if overagesInjected && shouldMarkCreditsExhausted(resp, respBody, nil) {
					modelKey := resolveCreditsOveragesModelKey(p.ctx, p.account, "", p.requestedModel)
					s.handleCreditsRetryFailure(p.ctx, p.prefix, modelKey, p.account, &http.Response{
						StatusCode: resp.StatusCode,
						Header:     resp.Header.Clone(),
						Body:       io.NopCloser(bytes.NewReader(respBody)),
					}, nil)
				}

				// ★ 统一入口：自定义错误码 + 临时不可调度
				if handled, outStatus, policyErr := s.applyErrorPolicy(p, resp.StatusCode, resp.Header, respBody); handled {
					if policyErr != nil {
						return nil, policyErr
					}
					resp = &http.Response{
						StatusCode: outStatus,
						Header:     resp.Header.Clone(),
						Body:       io.NopCloser(bytes.NewReader(respBody)),
					}
					break urlFallbackLoop
				}

				// 429/503 限流处理：区分 URL 级别限流、智能重试和账户配额限流
				if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
					// 尝试智能重试处理（OAuth 账号专用）
					smartResult := s.handleSmartRetry(p, resp, respBody, baseURL, urlIdx, availableURLs)
					switch smartResult.action {
					case smartRetryActionContinueURL:
						continue urlFallbackLoop
					case smartRetryActionBreakWithResp:
						if smartResult.err != nil {
							return nil, smartResult.err
						}
						// 模型限流时返回切换账号信号
						if smartResult.switchError != nil {
							return nil, smartResult.switchError
						}
						resp = smartResult.resp
						break urlFallbackLoop
					}
					// smartRetryActionContinue: 继续默认重试逻辑

					// 账户/模型配额限流，重试 3 次（指数退避）- 默认逻辑（非 OAuth 账号或解析失败）
					if attempt < antigravityMaxRetries {
						upstreamMsg := strings.TrimSpace(extractAntigravityErrorMessage(respBody))
						upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
						appendOpsUpstreamError(p.c, OpsUpstreamErrorEvent{
							Platform:           p.account.Platform,
							AccountID:          p.account.ID,
							AccountName:        p.account.Name,
							UpstreamStatusCode: resp.StatusCode,
							UpstreamRequestID:  resp.Header.Get("x-request-id"),
							UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
							Kind:               "retry",
							Message:            upstreamMsg,
							Detail:             getUpstreamDetail(respBody),
						})
						logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d retry=%d/%d body=%s", p.prefix, resp.StatusCode, attempt, antigravityMaxRetries, truncateForLog(respBody, 200))
						if !sleepAntigravityBackoffWithContext(p.ctx, attempt) {
							logger.LegacyPrintf("service.antigravity_gateway", "%s status=context_canceled_during_backoff", p.prefix)
							return nil, p.ctx.Err()
						}
						continue
					}

					// 重试用尽，标记账户限流
					p.handleError(p.ctx, p.prefix, p.account, resp.StatusCode, resp.Header, respBody, p.requestedModel, p.groupID, p.sessionHash, p.isStickySession)
					logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d rate_limited base_url=%s body=%s", p.prefix, resp.StatusCode, baseURL, truncateForLog(respBody, 200))
					resp = &http.Response{
						StatusCode: resp.StatusCode,
						Header:     resp.Header.Clone(),
						Body:       io.NopCloser(bytes.NewReader(respBody)),
					}
					break urlFallbackLoop
				}

				// 其他可重试错误（500/502/504/529，不包括 429 和 503）
				if shouldRetryAntigravityError(resp.StatusCode) {
					if attempt < antigravityMaxRetries {
						upstreamMsg := strings.TrimSpace(extractAntigravityErrorMessage(respBody))
						upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
						appendOpsUpstreamError(p.c, OpsUpstreamErrorEvent{
							Platform:           p.account.Platform,
							AccountID:          p.account.ID,
							AccountName:        p.account.Name,
							UpstreamStatusCode: resp.StatusCode,
							UpstreamRequestID:  resp.Header.Get("x-request-id"),
							UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
							Kind:               "retry",
							Message:            upstreamMsg,
							Detail:             getUpstreamDetail(respBody),
						})
						logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d retry=%d/%d body=%s", p.prefix, resp.StatusCode, attempt, antigravityMaxRetries, truncateForLog(respBody, 500))
						if !sleepAntigravityBackoffWithContext(p.ctx, attempt) {
							logger.LegacyPrintf("service.antigravity_gateway", "%s status=context_canceled_during_backoff", p.prefix)
							return nil, p.ctx.Err()
						}
						// 追踪 INTERNAL 500：非匹配的 attempt 清除标记
						if !isAntigravityInternalServerError(resp.StatusCode, respBody) {
							allAttemptsInternal500 = false
						}
						continue
					}
				}

				// INTERNAL 500 渐进惩罚：3 次重试全部命中特定 500 时递增计数器并惩罚
				if allAttemptsInternal500 && isAntigravityInternalServerError(resp.StatusCode, respBody) {
					s.handleInternal500RetryExhausted(p.ctx, p.prefix, p.account)
				}

				// 其他 4xx 错误或重试用尽，直接返回
				resp = &http.Response{
					StatusCode: resp.StatusCode,
					Header:     resp.Header.Clone(),
					Body:       io.NopCloser(bytes.NewReader(respBody)),
				}
				break urlFallbackLoop
			}

			// 成功响应（< 400）
			break urlFallbackLoop
		}
	}

	if resp != nil && resp.StatusCode < 400 && usedBaseURL != "" {
		antigravity.DefaultURLAvailability.MarkSuccess(usedBaseURL)
	}

	// 成功响应时清零 INTERNAL 500 连续失败计数器（覆盖所有成功路径，含 smart retry）
	if resp != nil && resp.StatusCode < 400 {
		s.resetInternal500Counter(p.ctx, p.prefix, p.account.ID)
	}

	return &antigravityRetryLoopResult{resp: resp}, nil
}

// shouldRetryAntigravityError 判断是否应该重试
func shouldRetryAntigravityError(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504, 529:
		return true
	default:
		return false
	}
}

// isURLLevelRateLimit 判断是否为 URL 级别的限流（应切换 URL 重试）
// "Resource has been exhausted" 是 URL/节点级别限流，切换 URL 可能成功
// "exhausted your capacity on this model" 是账户/模型配额限流，切换 URL 无效
func isURLLevelRateLimit(body []byte) bool {
	// 快速检查：包含 "Resource has been exhausted" 且不包含 "capacity on this model"
	bodyStr := string(body)
	return strings.Contains(bodyStr, "Resource has been exhausted") &&
		!strings.Contains(bodyStr, "capacity on this model")
}

// isAntigravityConnectionError 判断是否为连接错误（网络超时、DNS 失败、连接拒绝）
func isAntigravityConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// 检查超时错误
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// 检查连接错误（DNS 失败、连接拒绝）
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// shouldAntigravityFallbackToNextURL 判断是否应切换到下一个 URL
// 仅连接错误和 HTTP 429 触发 URL 降级
func shouldAntigravityFallbackToNextURL(err error, statusCode int) bool {
	if isAntigravityConnectionError(err) {
		return true
	}
	return statusCode == http.StatusTooManyRequests
}

// getSessionID 从 gin.Context 获取 session_id（用于日志追踪）
func getSessionID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetHeader("session_id")
}

// logPrefix 生成统一的日志前缀
func logPrefix(sessionID, accountName string) string {
	if sessionID != "" {
		return fmt.Sprintf("[antigravity-Forward] session=%s account=%s", sessionID, accountName)
	}
	return fmt.Sprintf("[antigravity-Forward] account=%s", accountName)
}
