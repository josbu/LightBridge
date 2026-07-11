package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/domain"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
	pkghttputil "github.com/WilliamWang1721/LightBridge/internal/pkg/httputil"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/ip"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	middleware2 "github.com/WilliamWang1721/LightBridge/internal/server/middleware"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Messages handles Claude API compatible messages endpoint
// POST /v1/messages
func (h *GatewayHandler) Messages(c *gin.Context) {
	// 从context获取apiKey和user（ApiKeyAuth中间件已设置）
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	defer h.maybeLogCompatibilityFallbackMetrics(reqLog)

	// 读取请求体
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(c, maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	parsedReq, err := service.ParseGatewayRequest(body, domain.PlatformAnthropic)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	reqModel := parsedReq.Model
	reqStream := parsedReq.Stream
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// 设置 max_tokens=1 + haiku 探测请求标识到 context 中
	// 必须在 SetClaudeCodeClientContext 之前设置，因为 ClaudeCodeValidator 需要读取此标识进行绕过判断
	if isMaxTokensOneHaikuRequest(reqModel, parsedReq.MaxTokens, reqStream) {
		ctx := service.WithIsMaxTokensOneHaikuRequest(c.Request.Context(), true, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)
	}

	// 检查是否为 Claude Code 客户端，设置到 context 中（复用已解析请求，避免二次反序列化）。
	SetClaudeCodeClientContext(c, body, parsedReq)
	isClaudeCodeClient := service.IsClaudeCodeClient(c.Request.Context())

	// 版本检查：仅对 Claude Code 客户端，拒绝低于最低版本的请求
	if !h.checkClaudeCodeVersion(c) {
		return
	}

	// 在请求上下文中记录 thinking 状态，供 Antigravity 最终模型 key 推导/模型维度限流使用
	c.Request = c.Request.WithContext(service.WithThinkingEnabled(c.Request.Context(), parsedReq.ThinkingEnabled, h.metadataBridgeEnabled()))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 验证 model 必填
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolAnthropicMessages, reqModel, body); decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return
	}

	// 隐私过滤：转发上游前对请求体脱敏。
	body = h.applyPrivacyFilter(c, reqLog, apiKey, service.ContentModerationProtocolAnthropicMessages, reqModel, body)

	// Track if we've started streaming (for error handling)
	streamStarted := false

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	// 获取订阅信息（可能为nil）- 提前获取用于后续检查
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	// 0. 检查wait队列是否已满
	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err != nil {
		reqLog.Warn("gateway.user_wait_counter_increment_failed", zap.Error(err))
		// On error, allow request to proceed
	} else if !canWait {
		reqLog.Info("gateway.user_wait_queue_full", zap.Int("max_wait", maxWait))
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	// Ensure we decrement if we exit before acquiring the user slot.
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	// 1. 首先获取用户并发槽位
	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	// User slot acquired: no longer waiting in the queue.
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	// 在请求结束或 Context 取消时确保释放槽位，避免客户端断开造成泄漏
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. 【新增】Wait后二次检查余额/订阅
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gateway.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 设置请求所属分组 ID（用于渠道级功能判断，如 WebSearch 模拟）
	parsedReq.GroupID = apiKey.GroupID

	// 计算粘性会话hash
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)

	// [DEBUG-STICKY] 打印会话 hash 生成结果
	reqLog.Info("sticky.session_hash_generated",
		zap.String("session_hash", sessionHash),
		zap.String("metadata_user_id_raw", parsedReq.MetadataUserID),
	)

	setCustomRequiredProtocol(c, service.CustomProtocolAnthropicMessages)
	platform := service.PlatformForRequest(c.Request.Context(), "")
	sessionKey := sessionHash
	if platform == service.PlatformGemini && sessionHash != "" {
		sessionKey = "gemini:" + sessionHash
	}

	// 查询粘性会话绑定的账号 ID
	var sessionBoundAccountID int64
	if sessionKey != "" {
		sessionBoundAccountID, _ = h.gatewayService.GetCachedSessionAccountID(c.Request.Context(), apiKey.GroupID, sessionKey)
		// [DEBUG-STICKY] 打印粘性会话查询结果
		reqLog.Info("sticky.cache_lookup",
			zap.String("session_key", sessionKey),
			zap.Int64("bound_account_id", sessionBoundAccountID),
		)
		if sessionBoundAccountID > 0 {
			prefetchedGroupID := int64(0)
			if apiKey.GroupID != nil {
				prefetchedGroupID = *apiKey.GroupID
			}
			ctx := service.WithPrefetchedStickySession(c.Request.Context(), sessionBoundAccountID, prefetchedGroupID, h.metadataBridgeEnabled())
			c.Request = c.Request.WithContext(ctx)
		}
	} else {
		reqLog.Info("sticky.no_session_key", zap.String("session_hash", sessionHash))
	}
	// 判断是否真的绑定了粘性会话：有 sessionKey 且已经绑定到某个账号
	hasBoundSession := sessionKey != "" && sessionBoundAccountID > 0

	if platform == service.PlatformGemini {
		fs := NewFailoverState(h.maxAccountSwitchesGemini, hasBoundSession)

		// 单账号分组提前设置 SingleAccountRetry 标记，让 Service 层首次 503 就不设模型限流标记。
		// 避免单账号分组收到 503 (MODEL_CAPACITY_EXHAUSTED) 时设 29s 限流，导致后续请求连续快速失败。
		if h.gatewayService.IsSingleAntigravityAccountGroup(c.Request.Context(), apiKey.GroupID) {
			ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
			c.Request = c.Request.WithContext(ctx)
		}

		for {
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionKey, reqModel, fs.FailedAccountIDs, "", int64(0)) // Gemini 不使用会话限制
			if err != nil {
				if len(fs.FailedAccountIDs) == 0 {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					service.SetOpsSchedulerDiagnostics(c, err)
					reqLog.Warn("gateway.select_account_no_available",
						zap.String("model", reqModel),
						zap.Int64p("group_id", apiKey.GroupID),
						zap.String("platform", platform),
						zap.Error(err),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", localizef(c, "No available accounts: %s", err.Error()), streamStarted)
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
					c.Request = c.Request.WithContext(ctx)
					continue
				case FailoverCanceled:
					return
				default: // FailoverExhausted
					if fs.LastFailoverErr != nil {
						h.handleFailoverExhausted(c, fs.LastFailoverErr, service.PlatformGemini, streamStarted)
					} else {
						h.handleFailoverExhaustedSimple(c, 502, streamStarted)
					}
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)

			// 检查请求拦截（预热请求、SUGGESTION MODE等）
			if account.IsInterceptWarmupEnabled() {
				interceptType := detectInterceptType(body, reqModel, parsedReq.MaxTokens, reqStream, isClaudeCodeClient)
				if interceptType != InterceptTypeNone {
					if selection.Acquired && selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					if reqStream {
						sendMockInterceptStream(c, reqModel, interceptType)
					} else {
						sendMockInterceptResponse(c, reqModel, interceptType)
					}
					return
				}
			}

			// 3. 获取账号并发槽位
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				if selection.WaitPlan == nil {
					markOpsRoutingCapacityLimited(c)
					reqLog.Warn("gateway.select_account_no_slot_no_wait_plan",
						zap.Int64("account_id", account.ID),
						zap.String("model", reqModel),
						zap.String("platform", platform),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
					return
				}
				accountWaitCounted := false
				canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
				if err != nil {
					reqLog.Warn("gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				} else if !canWait {
					reqLog.Info("gateway.account_wait_queue_full",
						zap.Int64("account_id", account.ID),
						zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
					)
					h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
					return
				}
				if err == nil && canWait {
					accountWaitCounted = true
				}
				releaseWait := func() {
					if accountWaitCounted {
						h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
						accountWaitCounted = false
					}
				}

				accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					reqStream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gateway.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					releaseWait()
					h.handleConcurrencyError(c, err, "account", streamStarted)
					return
				}
				// Slot acquired: no longer waiting in queue.
				releaseWait()
				if err := h.gatewayService.BindStickySession(c.Request.Context(), apiKey.GroupID, sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			// 账号槽位/等待计数需要在超时或断开时安全回收
			accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

			// 转发请求 - 根据 Router 决策分流到目标协议栈
			var forwarded protocolForwardResult
			var result *service.ForwardResult
			requestCtx := c.Request.Context()
			if fs.SwitchCount > 0 {
				requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
			}
			// 记录 Forward 前已写入字节数，Forward 后若增加则说明 SSE 内容已发，禁止 failover
			writerSizeBeforeForward := c.Writer.Size()
			promptCacheKey := ""
			if h.openAIGatewayService != nil {
				promptCacheKey = h.openAIGatewayService.ExtractSessionID(c, body)
			}
			forwarded, err = h.forwardMessagesViaProtocolRouter(requestCtx, c, account, body, parsedReq, promptCacheKey, "", hasBoundSession)
			result = forwarded.GatewayResult()
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			if err != nil {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					// 流式内容已写入客户端，无法撤销，禁止 failover 以防止流拼接腐化
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, service.PlatformGemini, true)
						return
					}
					action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.EffectivePlatform(), failoverErr)
					switch action {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						h.handleFailoverExhausted(c, fs.LastFailoverErr, service.PlatformGemini, streamStarted)
						return
					case FailoverCanceled:
						return
					}
				}
				wroteFallback := h.ensureForwardErrorResponse(c, streamStarted)
				forwardFailedFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.String("account_name", account.Name),
					zap.String("account_platform", account.Platform),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				}
				if account.Proxy != nil {
					forwardFailedFields = append(forwardFailedFields,
						zap.Int64("proxy_id", account.Proxy.ID),
						zap.String("proxy_name", account.Proxy.Name),
						zap.String("proxy_host", account.Proxy.Host),
						zap.Int("proxy_port", account.Proxy.Port),
					)
				} else if account.ProxyID != nil {
					forwardFailedFields = append(forwardFailedFields, zap.Int64p("proxy_id", account.ProxyID))
				}
				reqLog.Error("gateway.forward_failed", forwardFailedFields...)
				return
			}

			// RPM 计数递增（Forward 成功后）
			// 注意：TOCTOU 竞态是已知且可接受的设计权衡，与 WindowCost 一致的 soft-limit 模式。
			// 在高并发下可能短暂超出 RPM 限制，但不会导致请求失败。
			if account.IsAnthropicOAuthOrSetupToken() && account.GetBaseRPM() > 0 {
				if err := h.gatewayService.IncrementAccountRPM(c.Request.Context(), account.ID); err != nil {
					reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 捕获请求信息（用于异步记录，避免在 goroutine 中访问 gin.Context）
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.EffectivePlatform())

			if result.ReasoningEffort == nil {
				result.ReasoningEffort = service.NormalizeClaudeOutputEffort(parsedReq.OutputEffort)
			}

			// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			if forwarded.OpenAI != nil {
				h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
					if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
						Result:             forwarded.OpenAI,
						APIKey:             apiKey,
						User:               apiKey.User,
						Account:            account,
						Subscription:       subscription,
						InboundEndpoint:    inboundEndpoint,
						UpstreamEndpoint:   upstreamEndpoint,
						UserAgent:          userAgent,
						IPAddress:          clientIP,
						RequestPayloadHash: requestPayloadHash,
						APIKeyService:      h.apiKeyService,
						ChannelUsageFields: channelMapping.ToUsageFields(reqModel, forwarded.OpenAI.UpstreamModel),
					}); err != nil {
						logger.L().With(
							zap.String("component", "handler.gateway.messages"),
							zap.Int64("user_id", subject.UserID),
							zap.Int64("api_key_id", apiKey.ID),
							zap.Any("group_id", apiKey.GroupID),
							zap.String("model", reqModel),
							zap.Int64("account_id", account.ID),
						).Error("gateway.record_usage_failed", zap.Error(err))
					}
				})
				return
			}
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
					Result:             result,
					ParsedRequest:      parsedReq,
					QuotaPlatform:      quotaPlatform,
					APIKey:             apiKey,
					User:               apiKey.User,
					Account:            account,
					Subscription:       subscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					RequestPayloadHash: requestPayloadHash,
					ForceCacheBilling:  fs.ForceCacheBilling,
					APIKeyService:      h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.gateway.messages"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", apiKey.ID),
						zap.Any("group_id", apiKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("gateway.record_usage_failed", zap.Error(err))
				}
			})
			return
		}
	}

	currentAPIKey := apiKey
	currentSubscription := subscription
	var fallbackGroupID *int64
	if apiKey.Group != nil {
		fallbackGroupID = apiKey.Group.FallbackGroupIDOnInvalidRequest
	}
	fallbackUsed := false

	// 单账号分组提前设置 SingleAccountRetry 标记，让 Service 层首次 503 就不设模型限流标记。
	// 避免单账号分组收到 503 (MODEL_CAPACITY_EXHAUSTED) 时设 29s 限流，导致后续请求连续快速失败。
	if h.gatewayService.IsSingleAntigravityAccountGroup(c.Request.Context(), currentAPIKey.GroupID) {
		ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
		c.Request = c.Request.WithContext(ctx)
	}

	for {
		fs := NewFailoverState(h.maxAccountSwitches, hasBoundSession)
		retryWithFallback := false

		for {
			// 选择支持该模型的账号
			reqLog.Info("sticky.selecting_account",
				zap.String("session_key", sessionKey),
				zap.Int64("sticky_bound_account_id", sessionBoundAccountID),
				zap.Bool("has_bound_session", hasBoundSession),
				zap.Int("failed_account_count", len(fs.FailedAccountIDs)),
			)
			selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), currentAPIKey.GroupID, sessionKey, reqModel, fs.FailedAccountIDs, parsedReq.MetadataUserID, subject.UserID)
			if err != nil {
				if len(fs.FailedAccountIDs) == 0 {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
					service.SetOpsSchedulerDiagnostics(c, err)
					reqLog.Warn("gateway.select_account_no_available",
						zap.String("model", reqModel),
						zap.Int64p("group_id", currentAPIKey.GroupID),
						zap.String("platform", platform),
						zap.Bool("fallback_used", fallbackUsed),
						zap.Error(err),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", localizef(c, "No available accounts: %s", err.Error()), streamStarted)
					return
				}
				action := fs.HandleSelectionExhausted(c.Request.Context())
				switch action {
				case FailoverContinue:
					ctx := service.WithSingleAccountRetry(c.Request.Context(), true, h.metadataBridgeEnabled())
					c.Request = c.Request.WithContext(ctx)
					continue
				case FailoverCanceled:
					return
				default: // FailoverExhausted
					if fs.LastFailoverErr != nil {
						h.handleFailoverExhausted(c, fs.LastFailoverErr, platform, streamStarted)
					} else {
						h.handleFailoverExhaustedSimple(c, 502, streamStarted)
					}
					return
				}
			}
			account := selection.Account
			setOpsSelectedAccount(c, account.ID, account.Platform)

			// [DEBUG-STICKY] 打印账号选择结果
			reqLog.Info("sticky.account_selected",
				zap.Int64("selected_account_id", account.ID),
				zap.String("account_name", account.Name),
				zap.Bool("slot_acquired", selection.Acquired),
				zap.Bool("has_wait_plan", selection.WaitPlan != nil),
				zap.Int64("sticky_bound_account_id", sessionBoundAccountID),
				zap.Bool("sticky_honored", sessionBoundAccountID > 0 && sessionBoundAccountID == account.ID),
			)

			// 检查请求拦截（预热请求、SUGGESTION MODE等）
			if account.IsInterceptWarmupEnabled() {
				interceptType := detectInterceptType(body, reqModel, parsedReq.MaxTokens, reqStream, isClaudeCodeClient)
				if interceptType != InterceptTypeNone {
					if selection.Acquired && selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
					if reqStream {
						sendMockInterceptStream(c, reqModel, interceptType)
					} else {
						sendMockInterceptResponse(c, reqModel, interceptType)
					}
					return
				}
			}

			// 3. 获取账号并发槽位
			accountReleaseFunc := selection.ReleaseFunc
			if !selection.Acquired {
				if selection.WaitPlan == nil {
					markOpsRoutingCapacityLimited(c)
					reqLog.Warn("gateway.select_account_no_slot_no_wait_plan",
						zap.Int64("account_id", account.ID),
						zap.String("model", reqModel),
						zap.String("platform", platform),
					)
					h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
					return
				}
				accountWaitCounted := false
				canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
				if err != nil {
					reqLog.Warn("gateway.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				} else if !canWait {
					reqLog.Info("gateway.account_wait_queue_full",
						zap.Int64("account_id", account.ID),
						zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
					)
					h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
					return
				}
				if err == nil && canWait {
					accountWaitCounted = true
				}
				releaseWait := func() {
					if accountWaitCounted {
						h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
						accountWaitCounted = false
					}
				}

				accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
					c,
					account.ID,
					selection.WaitPlan.MaxConcurrency,
					selection.WaitPlan.Timeout,
					reqStream,
					&streamStarted,
				)
				if err != nil {
					reqLog.Warn("gateway.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
					releaseWait()
					h.handleConcurrencyError(c, err, "account", streamStarted)
					return
				}
				// Slot acquired: no longer waiting in queue.
				releaseWait()
				reqLog.Info("sticky.bind_after_wait",
					zap.String("session_key", sessionKey),
					zap.Int64("account_id", account.ID),
				)
				if err := h.gatewayService.BindStickySession(c.Request.Context(), currentAPIKey.GroupID, sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}
			// 账号槽位/等待计数需要在超时或断开时安全回收
			accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

			// ===== 用户消息串行队列 START =====
			var queueRelease func()
			umqMode := h.getUserMsgQueueMode(account, parsedReq)

			switch umqMode {
			case config.UMQModeSerialize:
				// 串行模式：获取锁 + RPM 延迟 + 释放（当前行为不变）
				baseRPM := account.GetBaseRPM()
				release, qErr := h.userMsgQueueHelper.AcquireWithWait(
					c, account.ID, baseRPM, reqStream, &streamStarted,
					h.cfg.Gateway.UserMessageQueue.WaitTimeout(),
					reqLog,
				)
				if qErr != nil {
					// fail-open: 记录 warn，不阻止请求
					reqLog.Warn("gateway.umq_acquire_failed",
						zap.Int64("account_id", account.ID),
						zap.Error(qErr),
					)
				} else {
					queueRelease = release
				}

			case config.UMQModeThrottle:
				// 软性限速：仅施加 RPM 自适应延迟，不阻塞并发
				baseRPM := account.GetBaseRPM()
				if tErr := h.userMsgQueueHelper.ThrottleWithPing(
					c, account.ID, baseRPM, reqStream, &streamStarted,
					h.cfg.Gateway.UserMessageQueue.WaitTimeout(),
					reqLog,
				); tErr != nil {
					reqLog.Warn("gateway.umq_throttle_failed",
						zap.Int64("account_id", account.ID),
						zap.Error(tErr),
					)
				}

			default:
				if umqMode != "" {
					reqLog.Warn("gateway.umq_unknown_mode",
						zap.String("mode", umqMode),
						zap.Int64("account_id", account.ID),
					)
				}
			}

			// 用 wrapReleaseOnDone 确保 context 取消时自动释放（仅 serialize 模式有 queueRelease）
			queueRelease = wrapReleaseOnDone(c.Request.Context(), queueRelease)
			// 注入回调到 ParsedRequest：使用外层 wrapper 以便提前清理 AfterFunc
			parsedReq.OnUpstreamAccepted = queueRelease
			// ===== 用户消息串行队列 END =====

			// 应用渠道模型映射到请求
			if channelMapping.Mapped {
				parsedReq.Model = channelMapping.MappedModel
				parsedReq.Body = h.gatewayService.ReplaceModelInBody(parsedReq.Body, channelMapping.MappedModel)
			}
			// Bedrock CC 兼容：渠道模型映射后，清理 Anthropic API 专有字段、注入 Bedrock 必需字段
			parsedReq.Body = h.gatewayService.ApplyBedrockCCCompat(c.Request.Context(), parsedReq.Body, parsedReq.Model, account, apiKey.GroupID)
			body = parsedReq.Body

			// 转发请求 - 根据 Router 决策分流到目标协议栈
			c.Set("parsed_request", parsedReq)
			var forwarded protocolForwardResult
			var result *service.ForwardResult
			requestCtx := c.Request.Context()
			if fs.SwitchCount > 0 {
				requestCtx = service.WithAccountSwitchCount(requestCtx, fs.SwitchCount, h.metadataBridgeEnabled())
			}
			// 记录 Forward 前已写入字节数，Forward 后若增加则说明 SSE 内容已发，禁止 failover
			writerSizeBeforeForward := c.Writer.Size()
			promptCacheKey := ""
			if h.openAIGatewayService != nil {
				promptCacheKey = h.openAIGatewayService.ExtractSessionID(c, body)
			}
			forwarded, err = h.forwardMessagesViaProtocolRouter(requestCtx, c, account, body, parsedReq, promptCacheKey, "", hasBoundSession)
			result = forwarded.GatewayResult()

			// 兜底释放串行锁（正常情况已通过回调提前释放）
			if queueRelease != nil {
				queueRelease()
			}
			// 清理回调引用，防止 failover 重试时旧回调被错误调用
			parsedReq.OnUpstreamAccepted = nil

			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			if err != nil {
				// Beta policy block: return 400 immediately, no failover
				var betaBlockedErr *service.BetaBlockedError
				if errors.As(err, &betaBlockedErr) {
					service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
					h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", betaBlockedErr.Message)
					return
				}

				var promptTooLongErr *service.PromptTooLongError
				if errors.As(err, &promptTooLongErr) {
					reqLog.Warn("gateway.prompt_too_long_from_antigravity",
						zap.Any("current_group_id", currentAPIKey.GroupID),
						zap.Any("fallback_group_id", fallbackGroupID),
						zap.Bool("fallback_used", fallbackUsed),
					)
					if !fallbackUsed && fallbackGroupID != nil && *fallbackGroupID > 0 {
						fallbackGroup, err := h.gatewayService.ResolveGroupByID(c.Request.Context(), *fallbackGroupID)
						if err != nil {
							reqLog.Warn("gateway.resolve_fallback_group_failed", zap.Int64("fallback_group_id", *fallbackGroupID), zap.Error(err))
							_ = h.antigravityGatewayService.WriteMappedClaudeError(c, account, promptTooLongErr.StatusCode, promptTooLongErr.RequestID, promptTooLongErr.Body)
							return
						}
						if fallbackGroup.SubscriptionType == service.SubscriptionTypeSubscription ||
							fallbackGroup.FallbackGroupIDOnInvalidRequest != nil {
							reqLog.Warn("gateway.fallback_group_invalid",
								zap.Int64("fallback_group_id", fallbackGroup.ID),
								zap.String("fallback_subscription_type", fallbackGroup.SubscriptionType),
							)
							_ = h.antigravityGatewayService.WriteMappedClaudeError(c, account, promptTooLongErr.StatusCode, promptTooLongErr.RequestID, promptTooLongErr.Body)
							return
						}
						fallbackAPIKey := cloneAPIKeyWithGroup(apiKey, fallbackGroup)
						if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), fallbackAPIKey.User, fallbackAPIKey, fallbackGroup, nil, service.QuotaPlatform(c.Request.Context(), fallbackAPIKey)); err != nil {
							status, code, message, retryAfter := billingErrorDetails(err)
							if retryAfter > 0 {
								c.Header("Retry-After", strconv.Itoa(retryAfter))
							}
							h.handleStreamingAwareError(c, status, code, message, streamStarted)
							return
						}
						// 兜底重试按"直接请求兜底分组"处理：清除强制平台，允许 Router 按入站协议调度。
						ctx := context.WithValue(c.Request.Context(), ctxkey.ForcePlatform, "")
						c.Request = c.Request.WithContext(ctx)
						currentAPIKey = fallbackAPIKey
						currentSubscription = nil
						fallbackUsed = true
						retryWithFallback = true
						break
					}
					_ = h.antigravityGatewayService.WriteMappedClaudeError(c, account, promptTooLongErr.StatusCode, promptTooLongErr.RequestID, promptTooLongErr.Body)
					return
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					// 流式内容已写入客户端，无法撤销，禁止 failover 以防止流拼接腐化
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, account.Platform, true)
						return
					}
					action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.EffectivePlatform(), failoverErr)
					switch action {
					case FailoverContinue:
						continue
					case FailoverExhausted:
						h.handleFailoverExhausted(c, fs.LastFailoverErr, account.Platform, streamStarted)
						return
					case FailoverCanceled:
						return
					}
				}
				wroteFallback := h.ensureForwardErrorResponse(c, streamStarted)
				forwardFailedFields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.String("account_name", account.Name),
					zap.String("account_platform", account.Platform),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				}
				if account.Proxy != nil {
					forwardFailedFields = append(forwardFailedFields,
						zap.Int64("proxy_id", account.Proxy.ID),
						zap.String("proxy_name", account.Proxy.Name),
						zap.String("proxy_host", account.Proxy.Host),
						zap.Int("proxy_port", account.Proxy.Port),
					)
				} else if account.ProxyID != nil {
					forwardFailedFields = append(forwardFailedFields, zap.Int64p("proxy_id", account.ProxyID))
				}
				reqLog.Error("gateway.forward_failed", forwardFailedFields...)
				return
			}

			// RPM 计数递增（Forward 成功后）
			// 注意：TOCTOU 竞态是已知且可接受的设计权衡，与 WindowCost 一致的 soft-limit 模式。
			// 在高并发下可能短暂超出 RPM 限制，但不会导致请求失败。
			if account.IsAnthropicOAuthOrSetupToken() && account.GetBaseRPM() > 0 {
				if err := h.gatewayService.IncrementAccountRPM(c.Request.Context(), account.ID); err != nil {
					reqLog.Warn("gateway.rpm_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 绑定粘性会话（成功转发后绑定/刷新）
			// - 无现有绑定（首次请求）：创建绑定
			// - 选中账号与粘性账号一致：刷新 TTL
			// - 粘性账号因负载/RPM 被跳过、选中了其他账号：不覆盖原绑定，
			//   下次请求粘性账号恢复后仍可命中
			if sessionKey != "" && (sessionBoundAccountID == 0 || sessionBoundAccountID == account.ID) {
				if err := h.gatewayService.BindStickySession(c.Request.Context(), currentAPIKey.GroupID, sessionKey, account.ID); err != nil {
					reqLog.Warn("gateway.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			}

			// 捕获请求信息（用于异步记录，避免在 goroutine 中访问 gin.Context）
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			requestPayloadHash := service.HashUsageRequestPayload(body)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := GetUpstreamEndpoint(c, account.EffectivePlatform())

			if result.ReasoningEffort == nil {
				result.ReasoningEffort = service.NormalizeClaudeOutputEffort(parsedReq.OutputEffort)
			}

			// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), currentAPIKey)
			if forwarded.OpenAI != nil {
				h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
					if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
						Result:             forwarded.OpenAI,
						APIKey:             currentAPIKey,
						User:               currentAPIKey.User,
						Account:            account,
						Subscription:       currentSubscription,
						InboundEndpoint:    inboundEndpoint,
						UpstreamEndpoint:   upstreamEndpoint,
						UserAgent:          userAgent,
						IPAddress:          clientIP,
						RequestPayloadHash: requestPayloadHash,
						APIKeyService:      h.apiKeyService,
						ChannelUsageFields: channelMapping.ToUsageFields(reqModel, forwarded.OpenAI.UpstreamModel),
					}); err != nil {
						logger.L().With(
							zap.String("component", "handler.gateway.messages"),
							zap.Int64("user_id", subject.UserID),
							zap.Int64("api_key_id", currentAPIKey.ID),
							zap.Any("group_id", currentAPIKey.GroupID),
							zap.String("model", reqModel),
							zap.Int64("account_id", account.ID),
						).Error("gateway.record_usage_failed", zap.Error(err))
					}
				})
				return
			}
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
					Result:             result,
					ParsedRequest:      parsedReq,
					QuotaPlatform:      quotaPlatform,
					APIKey:             currentAPIKey,
					User:               currentAPIKey.User,
					Account:            account,
					Subscription:       currentSubscription,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					RequestPayloadHash: requestPayloadHash,
					ForceCacheBilling:  fs.ForceCacheBilling,
					APIKeyService:      h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.gateway.messages"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", currentAPIKey.ID),
						zap.Any("group_id", currentAPIKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", account.ID),
					).Error("gateway.record_usage_failed", zap.Error(err))
				}
			})
			return
		}
		if !retryWithFallback {
			return
		}
	}
}
