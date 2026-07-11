package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/claude"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// Forward 转发请求到Claude API
func (s *GatewayService) Forward(ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest) (*ForwardResult, error) {
	startTime := time.Now()
	if parsed == nil {
		return nil, fmt.Errorf("parse request: empty request")
	}
	if result, handled, err := s.forwardModuleProvider(ctx, c, account, parsed, startTime); handled {
		return result, err
	}

	// Web Search 模拟：纯 web_search 请求时，直接调用搜索 API 构造响应
	if account != nil && s.shouldEmulateWebSearch(ctx, account, parsed.GroupID, parsed.Body) {
		return s.handleWebSearchEmulation(ctx, c, account, parsed)
	}

	if account != nil && account.IsAnthropicAPIKeyPassthroughEnabled() {
		passthroughBody := parsed.Body
		passthroughModel := parsed.Model
		if passthroughModel != "" {
			if mappedModel := account.GetMappedModel(passthroughModel); mappedModel != passthroughModel {
				passthroughBody = s.replaceModelInBody(passthroughBody, mappedModel)
				logger.LegacyPrintf("service.gateway", "Passthrough model mapping: %s -> %s (account: %s)", parsed.Model, mappedModel, account.Name)
				passthroughModel = mappedModel
			}
		}
		return s.forwardAnthropicAPIKeyPassthroughWithInput(ctx, c, account, anthropicPassthroughForwardInput{
			Body:          passthroughBody,
			RequestModel:  passthroughModel,
			OriginalModel: parsed.Model,
			RequestStream: parsed.Stream,
			StartTime:     startTime,
		})
	}

	if account != nil && account.IsBedrock() {
		return s.forwardBedrock(ctx, c, account, parsed, startTime)
	}

	// Beta policy: evaluate once; block check + cache filter set for buildUpstreamRequest.
	// Always overwrite the cache to prevent stale values from a previous retry with a different account.
	if account.Platform == PlatformAnthropic && c != nil {
		policy := s.evaluateBetaPolicy(ctx, c.GetHeader("anthropic-beta"), account, parsed.Model)
		if policy.blockErr != nil {
			return nil, policy.blockErr
		}
		filterSet := policy.filterSet
		if filterSet == nil {
			filterSet = map[string]struct{}{}
		}
		c.Set(betaPolicyFilterSetKey, filterSet)
	}

	body := parsed.Body
	reqModel := parsed.Model
	reqStream := parsed.Stream
	originalModel := reqModel

	// === DEBUG: 打印客户端原始请求（headers + body 摘要）===
	if c != nil {
		s.debugLogGatewaySnapshot("CLIENT_ORIGINAL", c.Request.Header, body, map[string]string{
			"account":      fmt.Sprintf("%d(%s)", account.ID, account.Name),
			"account_type": string(account.Type),
			"model":        reqModel,
			"stream":       strconv.FormatBool(reqStream),
		})
	}

	// Claude Code 客户端判定：UA 匹配 claude-cli/* 且携带 metadata.user_id。
	// 真正的 Claude Code 客户端自带完整的 system prompt、cache_control 断点和 header，
	// 不需要代理做任何 body 级别的 mimicry；强行替换反而会破坏客户端的缓存策略
	// （长 system prompt 被替换为 ~45 tokens 的短 prompt，低于 Anthropic 1024 token
	// 最低缓存门槛，导致系统级缓存失效）。
	//
	// 对于非 Claude Code 的第三方客户端（opencode 等），仍然走完整 mimicry。
	isClaudeCode := IsClaudeCodeClient(ctx) || isClaudeCodeClient(c.GetHeader("User-Agent"), parsed.MetadataUserID)
	shouldMimicClaudeCode := account.IsOAuth() && !isClaudeCode

	if shouldMimicClaudeCode {
		// 与 Parrot 对齐：OAuth 账号无条件重写 system（即使客户端已发了 Claude Code
		// 风格的 system prompt）。原因：第三方工具（opencode 等）会发 "You are Claude
		// Code..." system prompt 但缺少 billing attribution block，导致 Anthropic
		// 检测到"有 CC prompt 但无 billing block"的不一致而判为 third-party。
		// Parrot 的 transform_request 从不检查客户端 system 内容，直接覆盖。
		systemRewritten := false
		if !strings.Contains(strings.ToLower(reqModel), "haiku") {
			body = rewriteSystemForNonClaudeCode(body, parsed.System)
			systemRewritten = true
		}

		// system 被重写时保留 CC prompt 的 cache_control: ephemeral（匹配真实 Claude Code 行为）；
		// 未重写时（haiku / 已含 CC 前缀）剥离客户端 cache_control，与原有行为一致。
		// 两种情况下 enforceCacheControlLimit 都会兜底处理上限。
		normalizeOpts := claudeOAuthNormalizeOptions{stripSystemCacheControl: !systemRewritten}
		if s.identityService != nil {
			fp, err := s.identityService.GetOrCreateFingerprint(ctx, account.ID, c.Request.Header)
			if err == nil && fp != nil {
				// metadata 透传开启时跳过 metadata 注入
				_, mimicMPT, _ := s.settingService.GetGatewayForwardingSettings(ctx)
				if !mimicMPT {
					if metadataUserID := s.buildOAuthMetadataUserID(parsed, account, fp); metadataUserID != "" {
						normalizeOpts.injectMetadata = true
						normalizeOpts.metadataUserID = metadataUserID
					}
				}
			}
		}

		body, reqModel = normalizeClaudeOAuthRequestBody(body, reqModel, normalizeOpts)

		// D/E/F: 可选 messages cache 策略 + 工具名混淆 + tools[-1] 断点
		// 与 forward_as_chat_completions / forward_as_responses 路径对齐，
		// 原生 /v1/messages 路径也走同一套可配置字段级改写。
		body = s.rewriteMessageCacheControlIfEnabled(ctx, body)
		if rw := buildToolNameRewriteFromBody(body); rw != nil {
			body = applyToolNameRewriteToBody(body, rw)
			c.Set(toolNameRewriteKey, rw)
		} else {
			body = applyToolsLastCacheBreakpoint(body)
		}
	}

	// 强制执行 cache_control 块数量限制（最多 4 个）
	body = enforceCacheControlLimit(body)

	// 应用模型映射：
	// - APIKey 账号：使用账号级别的显式映射（如果配置），否则透传原始模型名
	// - OAuth/SetupToken 账号：使用 Anthropic 标准映射（短ID → 长ID）
	mappedModel := reqModel
	mappingSource := ""
	if account.Type == AccountTypeAPIKey {
		mappedModel = account.GetMappedModel(reqModel)
		if mappedModel != reqModel {
			mappingSource = "account"
		}
	}
	if mappingSource == "" && account.Platform == PlatformAnthropic && account.Type == AccountTypeServiceAccount {
		if candidate, matched := account.ResolveMappedModel(reqModel); matched {
			mappedModel = candidate
			mappingSource = "account"
		} else {
			normalized := normalizeVertexAnthropicModelID(claude.NormalizeModelID(reqModel))
			if normalized != reqModel {
				mappedModel = normalized
				mappingSource = "vertex"
			}
		}
	}
	if mappingSource == "" && account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
		normalized := claude.NormalizeModelID(reqModel)
		if normalized != reqModel {
			mappedModel = normalized
			mappingSource = "prefix"
		}
	}
	if mappedModel != reqModel {
		// 替换请求体中的模型名
		body = s.replaceModelInBody(body, mappedModel)
		reqModel = mappedModel
		logger.LegacyPrintf("service.gateway", "Model mapping applied: %s -> %s (account: %s, source=%s)", originalModel, mappedModel, account.Name, mappingSource)
	}

	if s.shouldInjectAnthropicCacheTTL1h(ctx, account) {
		body = injectAnthropicCacheControlTTL1h(body)
	}

	// 获取凭证
	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}

	// 获取代理URL（自定义 base URL 模式下，proxy 通过 buildCustomRelayURL 作为查询参数传递）
	proxyURL := ""
	if !account.IsCustomBaseURLEnabled() || account.GetCustomBaseURL() == "" {
		proxyURL, err = s.resolveAccountProxyURL(ctx, account, account.Platform, parsed.GroupID)
		if err != nil {
			return nil, err
		}
	}

	// 解析 TLS 指纹 profile（同一请求生命周期内不变，避免重试循环中重复解析）
	tlsProfile := s.tlsFPProfileService.ResolveTLSProfile(account)

	// 调试日志：记录即将转发的账号信息
	logger.LegacyPrintf("service.gateway", "[Forward] Using account: ID=%d Name=%s Platform=%s Type=%s TLSFingerprint=%v Proxy=%s",
		account.ID, account.Name, account.Platform, account.Type, tlsProfile, proxyURL)
	// Pre-filter: strip empty text blocks (including nested in tool_result) to prevent upstream 400.
	body = StripEmptyTextBlocks(body)

	// 重试循环
	var resp *http.Response
	retryStart := time.Now()
	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		// 构建上游请求（每次重试需要重新构建，因为请求体需要重新读取）
		upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, reqStream)
		upstreamReq, err := s.buildUpstreamRequest(upstreamCtx, c, account, body, token, tokenType, reqModel, reqStream, shouldMimicClaudeCode)
		releaseUpstreamCtx()
		if err != nil {
			return nil, err
		}

		// 发送请求
		resp, err = s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, tlsProfile)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			// Ensure the client receives an error response (handlers assume Forward writes on non-failover errors).
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			setOpsUpstreamError(c, 0, safeErr, "")
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: 0,
				UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
				Kind:               "request_error",
				Message:            safeErr,
			})
			c.JSON(http.StatusBadGateway, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "upstream_error",
					"message": "Upstream request failed",
				},
			})
			return nil, fmt.Errorf("upstream request failed: %s", safeErr)
		}

		// 优先检测thinking block签名错误（400）并重试一次
		if resp.StatusCode == 400 {
			respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if readErr == nil {
				_ = resp.Body.Close()

				if s.shouldRectifySignatureError(ctx, account, respBody) {
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						Platform:           account.EffectivePlatform(),
						AccountID:          account.ID,
						AccountName:        account.Name,
						UpstreamStatusCode: resp.StatusCode,
						UpstreamRequestID:  resp.Header.Get("x-request-id"),
						UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
						Kind:               "signature_error",
						Message:            extractUpstreamErrorMessage(respBody),
						Detail: func() string {
							if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
								return truncateString(string(respBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
							}
							return ""
						}(),
					})

					looksLikeToolSignatureError := func(msg string) bool {
						m := strings.ToLower(msg)
						return strings.Contains(m, "tool_use") ||
							strings.Contains(m, "tool_result") ||
							strings.Contains(m, "functioncall") ||
							strings.Contains(m, "function_call") ||
							strings.Contains(m, "functionresponse") ||
							strings.Contains(m, "function_response")
					}

					// 避免在重试预算已耗尽时再发起额外请求
					if time.Since(retryStart) >= maxRetryElapsed {
						resp.Body = io.NopCloser(bytes.NewReader(respBody))
						break
					}
					logger.LegacyPrintf("service.gateway", "[warn] Account %d: thinking blocks have invalid signature, retrying with filtered blocks", account.ID)

					// Conservative two-stage fallback:
					// 1) Disable thinking + thinking->text (preserve content)
					// 2) Only if upstream still errors AND error message points to tool/function signature issues:
					//    also downgrade tool_use/tool_result blocks to text.

					filteredBody := FilterThinkingBlocksForRetry(body)
					retryCtx, releaseRetryCtx := detachStreamUpstreamContext(ctx, reqStream)
					retryReq, buildErr := s.buildUpstreamRequest(retryCtx, c, account, filteredBody, token, tokenType, reqModel, reqStream, shouldMimicClaudeCode)
					releaseRetryCtx()
					if buildErr == nil {
						retryResp, retryErr := s.httpUpstream.DoWithTLS(retryReq, proxyURL, account.ID, account.Concurrency, tlsProfile)
						if retryErr == nil {
							if retryResp.StatusCode < 400 {
								logger.LegacyPrintf("service.gateway", "Account %d: thinking block retry succeeded (blocks downgraded)", account.ID)
								resp = retryResp
								break
							}

							retryRespBody, retryReadErr := io.ReadAll(io.LimitReader(retryResp.Body, 2<<20))
							_ = retryResp.Body.Close()
							if retryReadErr == nil && retryResp.StatusCode == 400 && s.isSignatureErrorPattern(ctx, account, retryRespBody) {
								appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
									Platform:           account.EffectivePlatform(),
									AccountID:          account.ID,
									AccountName:        account.Name,
									UpstreamStatusCode: retryResp.StatusCode,
									UpstreamRequestID:  retryResp.Header.Get("x-request-id"),
									UpstreamURL:        safeUpstreamURL(retryReq.URL.String()),
									Kind:               "signature_retry_thinking",
									Message:            extractUpstreamErrorMessage(retryRespBody),
									Detail: func() string {
										if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
											return truncateString(string(retryRespBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
										}
										return ""
									}(),
								})
								msg2 := extractUpstreamErrorMessage(retryRespBody)
								if looksLikeToolSignatureError(msg2) && time.Since(retryStart) < maxRetryElapsed {
									logger.LegacyPrintf("service.gateway", "Account %d: signature retry still failing and looks tool-related, retrying with tool blocks downgraded", account.ID)
									filteredBody2 := FilterSignatureSensitiveBlocksForRetry(body)
									retryCtx2, releaseRetryCtx2 := detachStreamUpstreamContext(ctx, reqStream)
									retryReq2, buildErr2 := s.buildUpstreamRequest(retryCtx2, c, account, filteredBody2, token, tokenType, reqModel, reqStream, shouldMimicClaudeCode)
									releaseRetryCtx2()
									if buildErr2 == nil {
										retryResp2, retryErr2 := s.httpUpstream.DoWithTLS(retryReq2, proxyURL, account.ID, account.Concurrency, tlsProfile)
										if retryErr2 == nil {
											resp = retryResp2
											break
										}
										if retryResp2 != nil && retryResp2.Body != nil {
											_ = retryResp2.Body.Close()
										}
										appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
											Platform:           account.EffectivePlatform(),
											AccountID:          account.ID,
											AccountName:        account.Name,
											UpstreamStatusCode: 0,
											UpstreamURL:        safeUpstreamURL(retryReq2.URL.String()),
											Kind:               "signature_retry_tools_request_error",
											Message:            sanitizeUpstreamErrorMessage(retryErr2.Error()),
										})
										logger.LegacyPrintf("service.gateway", "Account %d: tool-downgrade signature retry failed: %v", account.ID, retryErr2)
									} else {
										logger.LegacyPrintf("service.gateway", "Account %d: tool-downgrade signature retry build failed: %v", account.ID, buildErr2)
									}
								}
							}

							// Fall back to the original retry response context.
							resp = &http.Response{
								StatusCode: retryResp.StatusCode,
								Header:     retryResp.Header.Clone(),
								Body:       io.NopCloser(bytes.NewReader(retryRespBody)),
							}
							break
						}
						if retryResp != nil && retryResp.Body != nil {
							_ = retryResp.Body.Close()
						}
						logger.LegacyPrintf("service.gateway", "Account %d: signature error retry failed: %v", account.ID, retryErr)
					} else {
						logger.LegacyPrintf("service.gateway", "Account %d: signature error retry build request failed: %v", account.ID, buildErr)
					}

					// Retry failed: restore original response body and continue handling.
					resp.Body = io.NopCloser(bytes.NewReader(respBody))
					break
				}
				// 不是签名错误（或整流器已关闭），继续检查 budget 约束
				errMsg := extractUpstreamErrorMessage(respBody)
				if isThinkingBudgetConstraintError(errMsg) && s.settingService.IsBudgetRectifierEnabled(ctx) {
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						Platform:           account.EffectivePlatform(),
						AccountID:          account.ID,
						AccountName:        account.Name,
						UpstreamStatusCode: resp.StatusCode,
						UpstreamRequestID:  resp.Header.Get("x-request-id"),
						UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
						Kind:               "budget_constraint_error",
						Message:            errMsg,
						Detail: func() string {
							if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
								return truncateString(string(respBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
							}
							return ""
						}(),
					})

					rectifiedBody, applied := RectifyThinkingBudget(body)
					if applied && time.Since(retryStart) < maxRetryElapsed {
						logger.LegacyPrintf("service.gateway", "Account %d: detected budget_tokens constraint error, retrying with rectified budget (budget_tokens=%d, max_tokens=%d)", account.ID, BudgetRectifyBudgetTokens, BudgetRectifyMaxTokens)
						budgetRetryCtx, releaseBudgetRetryCtx := detachStreamUpstreamContext(ctx, reqStream)
						budgetRetryReq, buildErr := s.buildUpstreamRequest(budgetRetryCtx, c, account, rectifiedBody, token, tokenType, reqModel, reqStream, shouldMimicClaudeCode)
						releaseBudgetRetryCtx()
						if buildErr == nil {
							budgetRetryResp, retryErr := s.httpUpstream.DoWithTLS(budgetRetryReq, proxyURL, account.ID, account.Concurrency, tlsProfile)
							if retryErr == nil {
								resp = budgetRetryResp
								break
							}
							if budgetRetryResp != nil && budgetRetryResp.Body != nil {
								_ = budgetRetryResp.Body.Close()
							}
							logger.LegacyPrintf("service.gateway", "Account %d: budget rectifier retry failed: %v", account.ID, retryErr)
						} else {
							logger.LegacyPrintf("service.gateway", "Account %d: budget rectifier retry build failed: %v", account.ID, buildErr)
						}
					}
				}

				resp.Body = io.NopCloser(bytes.NewReader(respBody))
			}
		}

		// 检查是否需要通用重试（排除400，因为400已经在上面特殊处理过了）
		if resp.StatusCode >= 400 && resp.StatusCode != 400 && s.shouldRetryUpstreamError(account, resp.StatusCode) {
			if attempt < maxRetryAttempts {
				elapsed := time.Since(retryStart)
				if elapsed >= maxRetryElapsed {
					break
				}

				delay := retryBackoffDelay(attempt)
				remaining := maxRetryElapsed - elapsed
				if delay > remaining {
					delay = remaining
				}
				if delay <= 0 {
					break
				}

				respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
				_ = resp.Body.Close()
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  resp.Header.Get("x-request-id"),
					UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
					Kind:               "retry",
					Message:            extractUpstreamErrorMessage(respBody),
					Detail: func() string {
						if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
							return truncateString(string(respBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
						}
						return ""
					}(),
				})
				logger.LegacyPrintf("service.gateway", "Account %d: upstream error %d, retry %d/%d after %v (elapsed=%v/%v)",
					account.ID, resp.StatusCode, attempt, maxRetryAttempts, delay, elapsed, maxRetryElapsed)
				if err := sleepWithContext(ctx, delay); err != nil {
					return nil, err
				}
				continue
			}
			// 最后一次尝试也失败，跳出循环处理重试耗尽
			break
		}

		// 不需要重试（成功或不可重试的错误），跳出循环
		// DEBUG: 输出响应 headers（用于检测 rate limit 信息）
		if account.IsPureGemini() && resp.StatusCode < 400 && s.cfg != nil && s.cfg.Gateway.GeminiDebugResponseHeaders {
			logger.LegacyPrintf("service.gateway", "[DEBUG] Gemini API Response Headers for account %d:", account.ID)
			for k, v := range resp.Header {
				logger.LegacyPrintf("service.gateway", "[DEBUG]   %s: %v", k, v)
			}
		}
		break
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("upstream request failed: empty response")
	}
	defer func() { _ = resp.Body.Close() }()

	// 处理重试耗尽的情况
	if resp.StatusCode >= 400 && s.shouldRetryUpstreamError(account, resp.StatusCode) {
		if s.shouldFailoverUpstreamError(resp.StatusCode) {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(respBody))

			// 调试日志：打印重试耗尽后的错误响应
			logger.LegacyPrintf("service.gateway", "[Forward] Upstream error (retry exhausted, failover): Account=%d(%s) Status=%d RequestID=%s Body=%s",
				account.ID, account.Name, resp.StatusCode, resp.Header.Get("x-request-id"), truncateString(string(respBody), 1000))

			s.handleRetryExhaustedSideEffects(ctx, resp, account)
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "retry_exhausted_failover",
				Message:            extractUpstreamErrorMessage(respBody),
				Detail: func() string {
					if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
						return truncateString(string(respBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
					}
					return ""
				}(),
			})
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
			}
		}
		return s.handleRetryExhaustedError(ctx, resp, c, account)
	}

	// 处理可切换账号的错误
	if resp.StatusCode >= 400 && s.shouldFailoverUpstreamError(resp.StatusCode) {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// 调试日志：打印上游错误响应
		logger.LegacyPrintf("service.gateway", "[Forward] Upstream error (failover): Account=%d(%s) Status=%d RequestID=%s Body=%s",
			account.ID, account.Name, resp.StatusCode, resp.Header.Get("x-request-id"), truncateString(string(respBody), 1000))

		s.handleFailoverSideEffects(ctx, resp, account, reqModel)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "failover",
			Message:            extractUpstreamErrorMessage(respBody),
			Detail: func() string {
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					return truncateString(string(respBody), s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
				}
				return ""
			}(),
		})
		return nil, &UpstreamFailoverError{
			StatusCode:             resp.StatusCode,
			ResponseBody:           respBody,
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
		}
	}
	if resp.StatusCode >= 400 {
		// 可选：对部分 400 触发 failover（默认关闭以保持语义）
		if resp.StatusCode == 400 && s.cfg != nil && s.cfg.Gateway.FailoverOn400 {
			respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if readErr != nil {
				// ReadAll failed, fall back to normal error handling without consuming the stream
				return s.handleErrorResponse(ctx, resp, c, account, reqModel)
			}
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(respBody))

			if s.shouldFailoverOn400(respBody) {
				upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
				upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					Platform:           account.EffectivePlatform(),
					AccountID:          account.ID,
					AccountName:        account.Name,
					UpstreamStatusCode: resp.StatusCode,
					UpstreamRequestID:  resp.Header.Get("x-request-id"),
					Kind:               "failover_on_400",
					Message:            upstreamMsg,
					Detail:             upstreamDetail,
				})

				if s.cfg.Gateway.LogUpstreamErrorBody {
					logger.LegacyPrintf("service.gateway",
						"Account %d: 400 error, attempting failover: %s",
						account.ID,
						truncateForLog(respBody, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes),
					)
				} else {
					logger.LegacyPrintf("service.gateway", "Account %d: 400 error, attempting failover", account.ID)
				}
				s.handleFailoverSideEffects(ctx, resp, account, reqModel)
				return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
			}
		}
		return s.handleErrorResponse(ctx, resp, c, account, reqModel)
	}

	// 处理正常响应

	// 触发上游接受回调（提前释放串行锁，不等流完成）
	if parsed.OnUpstreamAccepted != nil {
		parsed.OnUpstreamAccepted()
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	var clientDisconnect bool
	if reqStream {
		streamResult, err := s.handleStreamingResponse(ctx, resp, c, account, startTime, originalModel, reqModel, shouldMimicClaudeCode)
		if err != nil {
			if err.Error() == "have error in stream" {
				return nil, &UpstreamFailoverError{
					StatusCode: 403,
				}
			}
			return nil, err
		}
		usage = streamResult.usage
		firstTokenMs = streamResult.firstTokenMs
		clientDisconnect = streamResult.clientDisconnect
	} else {
		usage, err = s.handleNonStreamingResponse(ctx, resp, c, account, originalModel, reqModel)
		if err != nil {
			return nil, err
		}
	}

	return &ForwardResult{
		RequestID:        resp.Header.Get("x-request-id"),
		Usage:            *usage,
		Model:            originalModel, // 使用原始模型用于计费和日志
		UpstreamModel:    mappedModel,
		Stream:           reqStream,
		Duration:         time.Since(startTime),
		FirstTokenMs:     firstTokenMs,
		ClientDisconnect: clientDisconnect,
	}, nil
}
