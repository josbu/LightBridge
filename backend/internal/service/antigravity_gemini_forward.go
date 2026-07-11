package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	mathrand "math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/antigravity"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// ForwardGemini 转发 Gemini 协议请求
//
// 限流处理流程:
//
//	请求 → antigravityRetryLoop → 预检查(remaining>0? → 切换账号) → 发送上游
//	  ├─ 成功 → 正常返回
//	  └─ 429/503 → handleSmartRetry
//	      ├─ retryDelay >= 7s → 设置模型限流 + 清除粘性绑定 → 切换账号
//	      └─ retryDelay <  7s → 等待后重试 1 次
//	          ├─ 成功 → 正常返回
//	          └─ 失败 → 设置模型限流 + 清除粘性绑定 → 切换账号
func (s *AntigravityGatewayService) ForwardGemini(ctx context.Context, c *gin.Context, account *Account, originalModel string, action string, stream bool, body []byte, isStickySession bool) (*ForwardResult, error) {
	startTime := time.Now()

	sessionID := getSessionID(c)
	prefix := logPrefix(sessionID, account.Name)

	if strings.TrimSpace(originalModel) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing model in URL")
	}
	if strings.TrimSpace(action) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing action in URL")
	}
	if len(body) == 0 {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Request body is empty")
	}

	// 解析请求以获取 image_size（用于图片计费）
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)

	switch action {
	case "generateContent", "streamGenerateContent":
		// ok
	case "countTokens":
		// 直接返回空值，不透传上游
		c.JSON(http.StatusOK, map[string]any{"totalTokens": 0})
		return &ForwardResult{
			RequestID:    "",
			Usage:        ClaudeUsage{},
			Model:        originalModel,
			Stream:       false,
			Duration:     time.Since(startTime),
			FirstTokenMs: nil,
		}, nil
	default:
		return nil, s.writeGoogleError(c, http.StatusNotFound, "Unsupported action: "+action)
	}

	mappedModel := s.getMappedModel(account, originalModel)
	if mappedModel == "" {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		return nil, s.writeGoogleError(c, http.StatusForbidden, fmt.Sprintf("model %s not in model list", originalModel))
	}
	billingModel := mappedModel

	// 获取 access_token
	if s.tokenProvider == nil {
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Antigravity token provider not configured")
	}
	accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: []byte(`{"error":{"message":"Failed to get upstream access token","status":"UNAVAILABLE"}}`),
		}
	}

	// 获取 project_id（部分账户类型可能没有）
	projectID := strings.TrimSpace(account.GetCredential("project_id"))

	// 代理 URL
	proxyURL := ""
	proxyURL, err = s.resolveAccountProxyURL(ctx, account, account.Platform, apiKeyGroupID(getAPIKeyFromContext(c)))
	if err != nil {
		return nil, err
	}

	// Antigravity 上游要求必须包含身份提示词，注入到请求中
	injectedBody, err := injectIdentityPatchToGeminiRequest(body)
	if err != nil {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Invalid request body")
	}

	// 清理 Schema
	if cleanedBody, err := cleanGeminiRequest(injectedBody); err == nil {
		injectedBody = cleanedBody
		logger.LegacyPrintf("service.antigravity_gateway", "[Antigravity] Cleaned request schema in forwarded request for account %s", account.Name)
	} else {
		logger.LegacyPrintf("service.antigravity_gateway", "[Antigravity] Failed to clean schema: %v", err)
	}

	// 推理模型参数清理：移除 temperature/topP/topK 等不支持的参数
	if strippedBody, err := antigravity.StripInvalidGeminiReasoningParams(injectedBody, mappedModel); err == nil {
		injectedBody = strippedBody
	}

	// 包装请求
	wrappedBody, err := s.wrapV1InternalRequest(projectID, mappedModel, injectedBody)
	if err != nil {
		return nil, s.writeGoogleError(c, http.StatusInternalServerError, "Failed to build upstream request")
	}

	// Antigravity 上游只支持流式请求，统一使用 streamGenerateContent
	// 如果客户端请求非流式，在响应处理阶段会收集完整流式响应后返回
	upstreamAction := "streamGenerateContent"

	// 执行带重试的请求
	result, err := s.antigravityRetryLoop(antigravityRetryLoopParams{
		ctx:             ctx,
		prefix:          prefix,
		account:         account,
		proxyURL:        proxyURL,
		accessToken:     accessToken,
		action:          upstreamAction,
		body:            wrappedBody,
		c:               c,
		httpUpstream:    s.httpUpstream,
		settingService:  s.settingService,
		accountRepo:     s.accountRepo,
		handleError:     s.handleUpstreamError,
		requestedModel:  originalModel,
		isStickySession: isStickySession, // ForwardGemini 由上层判断粘性会话
		groupID:         0,               // ForwardGemini 方法没有 groupID，由上层处理粘性会话清除
		sessionHash:     "",              // ForwardGemini 方法没有 sessionHash，由上层处理粘性会话清除
	})
	if err != nil {
		// 检查是否是账号切换信号，转换为 UpstreamFailoverError 让 Handler 切换账号
		if switchErr, ok := IsAntigravityAccountSwitchError(err); ok {
			return nil, &UpstreamFailoverError{
				StatusCode:        http.StatusServiceUnavailable,
				ForceCacheBilling: switchErr.IsStickySession,
			}
		}
		// 区分客户端取消和真正的上游失败，返回更准确的错误消息
		if c.Request.Context().Err() != nil {
			return nil, s.writeGoogleError(c, http.StatusBadGateway, "Client disconnected before upstream response")
		}
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Upstream request failed after retries")
	}
	resp := result.resp
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	// 处理错误响应
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		contentType := resp.Header.Get("Content-Type")
		// 尽早关闭原始响应体，释放连接；后续逻辑仍可能需要读取 body，因此用内存副本重新包装。
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// 模型兜底：模型不存在且开启 fallback 时，自动用 fallback 模型重试一次
		if s.settingService != nil && s.settingService.IsModelFallbackEnabled(ctx) &&
			isModelNotFoundError(resp.StatusCode, respBody) {
			fallbackModel := s.settingService.GetFallbackModel(ctx, PlatformAntigravity)
			if fallbackModel != "" && fallbackModel != mappedModel {
				logger.LegacyPrintf("service.antigravity_gateway", "[Antigravity] Model not found (%s), retrying with fallback model %s (account: %s)", mappedModel, fallbackModel, account.Name)

				fallbackWrapped, err := s.wrapV1InternalRequest(projectID, fallbackModel, injectedBody)
				if err == nil {
					fallbackReq, err := antigravity.NewAPIRequest(ctx, upstreamAction, accessToken, fallbackWrapped)
					if err == nil {
						fallbackResp, err := s.httpUpstream.Do(fallbackReq, proxyURL, account.ID, account.Concurrency)
						if err == nil && fallbackResp.StatusCode < 400 {
							_ = resp.Body.Close()
							resp = fallbackResp
						} else if fallbackResp != nil {
							_ = fallbackResp.Body.Close()
						}
					}
				}
			}
		}

		// Gemini 原生请求中的 thoughtSignature 可能来自旧上下文/旧账号，触发上游严格校验后返回
		// "Corrupted thought signature."。检测到此类 400 时，将 thoughtSignature 清理为 dummy 值后重试一次。
		signatureCheckBody := respBody
		if unwrapped, unwrapErr := s.unwrapV1InternalResponse(respBody); unwrapErr == nil && len(unwrapped) > 0 {
			signatureCheckBody = unwrapped
		}
		if resp.StatusCode == http.StatusBadRequest &&
			s.settingService != nil &&
			s.settingService.IsSignatureRectifierEnabled(ctx) &&
			isSignatureRelatedError(signatureCheckBody) &&
			bytes.Contains(injectedBody, []byte(`"thoughtSignature"`)) {
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractAntigravityErrorMessage(signatureCheckBody)))
			upstreamDetail := s.getUpstreamErrorDetail(signatureCheckBody)
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "signature_error",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})

			logger.LegacyPrintf("service.antigravity_gateway", "Antigravity Gemini account %d: detected signature-related 400, retrying with cleaned thought signatures", account.ID)

			cleanedInjectedBody := CleanGeminiNativeThoughtSignatures(injectedBody)
			retryWrappedBody, wrapErr := s.wrapV1InternalRequest(projectID, mappedModel, cleanedInjectedBody)
			if wrapErr == nil {
				retryResult, retryErr := s.antigravityRetryLoop(antigravityRetryLoopParams{
					ctx:             ctx,
					prefix:          prefix,
					account:         account,
					proxyURL:        proxyURL,
					accessToken:     accessToken,
					action:          upstreamAction,
					body:            retryWrappedBody,
					c:               c,
					httpUpstream:    s.httpUpstream,
					settingService:  s.settingService,
					accountRepo:     s.accountRepo,
					handleError:     s.handleUpstreamError,
					requestedModel:  originalModel,
					isStickySession: isStickySession,
					groupID:         0,
					sessionHash:     "",
				})
				if retryErr == nil {
					retryResp := retryResult.resp
					if retryResp.StatusCode < 400 {
						resp = retryResp
					} else {
						retryRespBody, _ := io.ReadAll(io.LimitReader(retryResp.Body, 2<<20))
						_ = retryResp.Body.Close()
						retryOpsBody := retryRespBody
						if retryUnwrapped, unwrapErr := s.unwrapV1InternalResponse(retryRespBody); unwrapErr == nil && len(retryUnwrapped) > 0 {
							retryOpsBody = retryUnwrapped
						}
						appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
							Platform:           account.EffectivePlatform(),
							AccountID:          account.ID,
							AccountName:        account.Name,
							UpstreamStatusCode: retryResp.StatusCode,
							UpstreamRequestID:  retryResp.Header.Get("x-request-id"),
							Kind:               "signature_retry",
							Message:            sanitizeUpstreamErrorMessage(strings.TrimSpace(extractAntigravityErrorMessage(retryOpsBody))),
							Detail:             s.getUpstreamErrorDetail(retryOpsBody),
						})
						respBody = retryRespBody
						resp = &http.Response{
							StatusCode: retryResp.StatusCode,
							Header:     retryResp.Header.Clone(),
							Body:       io.NopCloser(bytes.NewReader(retryRespBody)),
						}
						contentType = resp.Header.Get("Content-Type")
					}
				} else {
					if switchErr, ok := IsAntigravityAccountSwitchError(retryErr); ok {
						appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
							Platform:           account.EffectivePlatform(),
							AccountID:          account.ID,
							AccountName:        account.Name,
							UpstreamStatusCode: http.StatusServiceUnavailable,
							Kind:               "failover",
							Message:            sanitizeUpstreamErrorMessage(retryErr.Error()),
						})
						return nil, &UpstreamFailoverError{
							StatusCode:        http.StatusServiceUnavailable,
							ForceCacheBilling: switchErr.IsStickySession,
						}
					}
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						Platform:           account.EffectivePlatform(),
						AccountID:          account.ID,
						AccountName:        account.Name,
						UpstreamStatusCode: 0,
						Kind:               "signature_retry_request_error",
						Message:            sanitizeUpstreamErrorMessage(retryErr.Error()),
					})
					logger.LegacyPrintf("service.antigravity_gateway", "Antigravity Gemini account %d: signature retry request failed: %v", account.ID, retryErr)
				}
			} else {
				logger.LegacyPrintf("service.antigravity_gateway", "Antigravity Gemini account %d: signature retry wrap failed: %v", account.ID, wrapErr)
			}
		}

		// fallback 成功：继续按正常响应处理
		if resp.StatusCode < 400 {
			goto handleSuccess
		}

		requestID := resp.Header.Get("x-request-id")
		if requestID != "" {
			c.Header("x-request-id", requestID)
		}

		unwrapped, unwrapErr := s.unwrapV1InternalResponse(respBody)
		unwrappedForOps := unwrapped
		if unwrapErr != nil || len(unwrappedForOps) == 0 {
			unwrappedForOps = respBody
		}
		s.handleUpstreamError(ctx, prefix, account, resp.StatusCode, resp.Header, respBody, originalModel, 0, "", isStickySession)
		upstreamMsg := strings.TrimSpace(extractAntigravityErrorMessage(unwrappedForOps))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		upstreamDetail := s.getUpstreamErrorDetail(unwrappedForOps)

		// Always record upstream context for Ops error logs, even when we will failover.
		setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)

		// 精确匹配服务端配置类 400 错误，触发同账号重试 + failover
		if resp.StatusCode == http.StatusBadRequest && isGoogleProjectConfigError(strings.ToLower(upstreamMsg)) {
			log.Printf("%s status=400 google_config_error failover=true upstream_message=%q account=%d", prefix, upstreamMsg, account.ID)
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: unwrappedForOps, RetryableOnSameAccount: true}
		}

		if s.shouldFailoverUpstreamError(resp.StatusCode) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.EffectivePlatform(),
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  requestID,
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: unwrappedForOps}
		}
		if contentType == "" {
			contentType = "application/json"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.EffectivePlatform(),
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  requestID,
			Kind:               "http_error",
			Message:            upstreamMsg,
			Detail:             upstreamDetail,
		})
		logger.LegacyPrintf("service.antigravity_gateway", "[antigravity-Forward] upstream error status=%d body=%s", resp.StatusCode, truncateForLog(unwrappedForOps, 500))
		c.Data(resp.StatusCode, contentType, unwrappedForOps)
		return nil, fmt.Errorf("antigravity upstream error: %d", resp.StatusCode)
	}

handleSuccess:
	requestID := resp.Header.Get("x-request-id")
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	var clientDisconnect bool

	if stream {
		// 客户端要求流式，直接透传
		streamRes, err := s.handleGeminiStreamingResponse(c, resp, startTime)
		if err != nil {
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=stream_error error=%v", prefix, err)
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
		clientDisconnect = streamRes.clientDisconnect
	} else {
		// 客户端要求非流式，收集流式响应后返回
		streamRes, err := s.handleGeminiStreamToNonStreaming(c, resp, startTime)
		if err != nil {
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=stream_collect_error error=%v", prefix, err)
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
	}

	if usage == nil {
		usage = &ClaudeUsage{}
	}

	// 判断是否为图片生成模型
	imageCount := 0
	if isImageGenerationModel(mappedModel) {
		// Gemini 图片生成 API 每次请求只生成一张图片（API 限制）
		imageCount = 1
	}

	return &ForwardResult{
		RequestID:        requestID,
		Usage:            *usage,
		Model:            originalModel,
		UpstreamModel:    billingModel,
		Stream:           stream,
		Duration:         time.Since(startTime),
		FirstTokenMs:     firstTokenMs,
		ClientDisconnect: clientDisconnect,
		ImageCount:       imageCount,
		ImageSize:        imageSize,
		ImageInputSize:   imageInputSize,
	}, nil
}

func (s *AntigravityGatewayService) shouldFailoverUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

// isGoogleProjectConfigError 判断（已提取的小写）错误消息是否属于 Google 服务端配置类问题。
// 只精确匹配已知的服务端侧错误，避免对客户端请求错误做无意义重试。
// 适用于所有走 Google 后端的平台（Antigravity、Gemini）。
func isGoogleProjectConfigError(lowerMsg string) bool {
	// Google 间歇性 Bug：Project ID 有效但被临时识别失败
	return strings.Contains(lowerMsg, "invalid project resource name")
}

// googleConfigErrorCooldown 服务端配置类 400 错误的临时封禁时长
const googleConfigErrorCooldown = 1 * time.Minute

// tempUnscheduleGoogleConfigError 对服务端配置类 400 错误触发临时封禁，
// 避免短时间内反复调度到同一个有问题的账号。
func tempUnscheduleGoogleConfigError(ctx context.Context, repo AccountRepository, accountID int64, logPrefix string) {
	until := time.Now().Add(googleConfigErrorCooldown)
	reason := "400: invalid project resource name (auto temp-unschedule 1m)"
	if err := repo.SetTempUnschedulable(ctx, accountID, until, reason); err != nil {
		log.Printf("%s temp_unschedule_failed account=%d error=%v", logPrefix, accountID, err)
	} else {
		log.Printf("%s temp_unscheduled account=%d until=%v reason=%q", logPrefix, accountID, until.Format("15:04:05"), reason)
	}
}

// emptyResponseCooldown 空流式响应的临时封禁时长
const emptyResponseCooldown = 1 * time.Minute

// tempUnscheduleEmptyResponse 对空流式响应触发临时封禁，
// 避免短时间内反复调度到同一个返回空响应的账号。
func tempUnscheduleEmptyResponse(ctx context.Context, repo AccountRepository, accountID int64, logPrefix string) {
	until := time.Now().Add(emptyResponseCooldown)
	reason := "empty stream response (auto temp-unschedule 1m)"
	if err := repo.SetTempUnschedulable(ctx, accountID, until, reason); err != nil {
		log.Printf("%s temp_unschedule_failed account=%d error=%v", logPrefix, accountID, err)
	} else {
		log.Printf("%s temp_unscheduled account=%d until=%v reason=%q", logPrefix, accountID, until.Format("15:04:05"), reason)
	}
}

// sleepAntigravityBackoffWithContext 带 context 取消检查的退避等待
// 返回 true 表示正常完成等待，false 表示 context 已取消
func sleepAntigravityBackoffWithContext(ctx context.Context, attempt int) bool {
	delay := antigravityRetryBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > antigravityRetryMaxDelay {
		delay = antigravityRetryMaxDelay
	}

	// +/- 20% jitter
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	jitter := time.Duration(float64(delay) * 0.2 * (r.Float64()*2 - 1))
	sleepFor := delay + jitter
	if sleepFor < 0 {
		sleepFor = 0
	}

	timer := time.NewTimer(sleepFor)
	select {
	case <-ctx.Done():
		timer.Stop()
		return false
	case <-timer.C:
		return true
	}
}

// isSingleAccountRetry 检查 context 中是否设置了单账号退避重试标记
func isSingleAccountRetry(ctx context.Context) bool {
	v, _ := SingleAccountRetryFromContext(ctx)
	return v
}

// setModelRateLimitByModelName 使用官方模型 ID 设置模型级限流
// 直接使用上游返回的模型 ID（如 claude-sonnet-4-5）作为限流 key
// 返回是否已成功设置（若模型名为空或 repo 为 nil 将返回 false）
func setModelRateLimitByModelName(ctx context.Context, repo AccountRepository, accountID int64, modelName, prefix string, statusCode int, resetAt time.Time, afterSmartRetry bool) bool {
	if repo == nil || modelName == "" {
		return false
	}
	// 直接使用官方模型 ID 作为 key，不再转换为 scope
	if err := repo.SetModelRateLimit(ctx, accountID, modelName, resetAt); err != nil {
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limit_failed model=%s error=%v", prefix, statusCode, modelName, err)
		return false
	}
	if afterSmartRetry {
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limited_after_smart_retry model=%s account=%d reset_in=%v", prefix, statusCode, modelName, accountID, time.Until(resetAt).Truncate(time.Second))
	} else {
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limited model=%s account=%d reset_in=%v", prefix, statusCode, modelName, accountID, time.Until(resetAt).Truncate(time.Second))
	}
	return true
}

func antigravityFallbackCooldownSeconds() (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(antigravityFallbackSecondsEnv))
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// antigravitySmartRetryInfo 智能重试所需的信息
type antigravitySmartRetryInfo struct {
	RetryDelay               time.Duration // 重试延迟时间
	ModelName                string        // 限流的模型名称（如 "claude-sonnet-4-5"）
	IsModelCapacityExhausted bool          // 是否为模型容量不足（MODEL_CAPACITY_EXHAUSTED）
}

// parseAntigravitySmartRetryInfo 解析 Google RPC RetryInfo 和 ErrorInfo 信息
// 返回解析结果，如果解析失败或不满足条件返回 nil
//
// 支持两种情况：
// 1. 429 RESOURCE_EXHAUSTED + RATE_LIMIT_EXCEEDED：
//   - error.status == "RESOURCE_EXHAUSTED"
//   - error.details[].reason == "RATE_LIMIT_EXCEEDED"
//
// 2. 503 UNAVAILABLE + MODEL_CAPACITY_EXHAUSTED：
//   - error.status == "UNAVAILABLE"
//   - error.details[].reason == "MODEL_CAPACITY_EXHAUSTED"
//
// 必须满足以下条件才会返回有效值：
// - error.details[] 中存在 @type == "type.googleapis.com/google.rpc.RetryInfo" 的元素
// - 该元素包含 retryDelay 字段，格式为 "数字s"（如 "0.201506475s"）
func parseAntigravitySmartRetryInfo(body []byte) *antigravitySmartRetryInfo {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		return nil
	}

	// 检查 status 是否符合条件
	// 情况1: 429 RESOURCE_EXHAUSTED (需要进一步检查 reason == RATE_LIMIT_EXCEEDED)
	// 情况2: 503 UNAVAILABLE (需要进一步检查 reason == MODEL_CAPACITY_EXHAUSTED)
	status, _ := errObj["status"].(string)
	isResourceExhausted := status == googleRPCStatusResourceExhausted
	isUnavailable := status == googleRPCStatusUnavailable

	if !isResourceExhausted && !isUnavailable {
		return nil
	}

	details, ok := errObj["details"].([]any)
	if !ok {
		return nil
	}

	var retryDelay time.Duration
	var modelName string
	var hasRateLimitExceeded bool      // 429 需要此 reason
	var hasModelCapacityExhausted bool // 503 需要此 reason

	for _, d := range details {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}

		atType, _ := dm["@type"].(string)

		// 从 ErrorInfo 提取模型名称和 reason
		if atType == googleRPCTypeErrorInfo {
			if meta, ok := dm["metadata"].(map[string]any); ok {
				if model, ok := meta["model"].(string); ok {
					modelName = model
				}
			}
			// 检查 reason
			if reason, ok := dm["reason"].(string); ok {
				if reason == googleRPCReasonModelCapacityExhausted {
					hasModelCapacityExhausted = true
				}
				if reason == googleRPCReasonRateLimitExceeded {
					hasRateLimitExceeded = true
				}
			}
			continue
		}

		// 从 RetryInfo 提取重试延迟
		if atType == googleRPCTypeRetryInfo {
			delay, ok := dm["retryDelay"].(string)
			if !ok || delay == "" {
				continue
			}
			// 使用 time.ParseDuration 解析，支持所有 Go duration 格式
			// 例如: "0.5s", "10s", "4m50s", "1h30m", "200ms" 等
			dur, err := time.ParseDuration(delay)
			if err != nil {
				logger.LegacyPrintf("service.antigravity_gateway", "[Antigravity] failed to parse retryDelay: %s error=%v", delay, err)
				continue
			}
			retryDelay = dur
		}
	}

	// 验证条件
	// 情况1: RESOURCE_EXHAUSTED 需要有 RATE_LIMIT_EXCEEDED reason
	// 情况2: UNAVAILABLE 需要有 MODEL_CAPACITY_EXHAUSTED reason
	if isResourceExhausted && !hasRateLimitExceeded {
		return nil
	}
	if isUnavailable && !hasModelCapacityExhausted {
		return nil
	}

	// 必须有模型名才返回有效结果
	if modelName == "" {
		return nil
	}

	// 如果上游未提供 retryDelay，使用默认限流时间
	if retryDelay <= 0 {
		retryDelay = antigravityDefaultRateLimitDuration
	}

	return &antigravitySmartRetryInfo{
		RetryDelay:               retryDelay,
		ModelName:                modelName,
		IsModelCapacityExhausted: hasModelCapacityExhausted,
	}
}

// shouldTriggerAntigravitySmartRetry 判断是否应该触发智能重试
// 返回：
//   - shouldRetry: 是否应该智能重试（retryDelay < antigravityRateLimitThreshold，或 MODEL_CAPACITY_EXHAUSTED）
//   - shouldRateLimitModel: 是否应该限流模型并切换账号（仅 RATE_LIMIT_EXCEEDED 且 retryDelay >= 阈值）
//   - waitDuration: 等待时间
//   - modelName: 限流的模型名称
//   - isModelCapacityExhausted: 是否为模型容量不足（MODEL_CAPACITY_EXHAUSTED）
func shouldTriggerAntigravitySmartRetry(account *Account, respBody []byte) (shouldRetry bool, shouldRateLimitModel bool, waitDuration time.Duration, modelName string, isModelCapacityExhausted bool) {
	if !account.IsAntigravity() {
		return false, false, 0, "", false
	}

	info := parseAntigravitySmartRetryInfo(respBody)
	if info == nil {
		return false, false, 0, "", false
	}

	// MODEL_CAPACITY_EXHAUSTED（模型容量不足）：所有账号共享同一模型容量池
	// 切换账号无意义，使用固定 1s 间隔重试
	if info.IsModelCapacityExhausted {
		return true, false, antigravityModelCapacityRetryWait, info.ModelName, true
	}

	// RATE_LIMIT_EXCEEDED（账号级限流）：
	// retryDelay >= 阈值：直接限流模型，不重试
	// 注意：如果上游未提供 retryDelay，parseAntigravitySmartRetryInfo 已设置为默认 30s
	if info.RetryDelay >= antigravityRateLimitThreshold {
		return false, true, info.RetryDelay, info.ModelName, false
	}

	// retryDelay < 阈值：智能重试
	waitDuration = info.RetryDelay
	if waitDuration < antigravitySmartRetryMinWait {
		waitDuration = antigravitySmartRetryMinWait
	}

	return true, false, waitDuration, info.ModelName, false
}

// handleModelRateLimitParams 模型级限流处理参数
type handleModelRateLimitParams struct {
	ctx             context.Context
	prefix          string
	account         *Account
	statusCode      int
	body            []byte
	cache           GatewayCache
	groupID         int64
	sessionHash     string
	isStickySession bool
}

// handleModelRateLimitResult 模型级限流处理结果
type handleModelRateLimitResult struct {
	Handled      bool                           // 是否已处理
	ShouldRetry  bool                           // 是否等待后重试
	WaitDuration time.Duration                  // 等待时间
	SwitchError  *AntigravityAccountSwitchError // 账号切换错误
}

// handleModelRateLimit 处理模型级限流（在原有逻辑之前调用）
// 仅处理 429/503，解析模型名和 retryDelay
// - MODEL_CAPACITY_EXHAUSTED: 返回 Handled=true（实际重试由 handleSmartRetry 处理）
// - RATE_LIMIT_EXCEEDED + retryDelay < 阈值: 返回 ShouldRetry=true，由调用方等待后重试
// - RATE_LIMIT_EXCEEDED + retryDelay >= 阈值: 设置模型限流 + 清除粘性会话 + 返回 SwitchError
func (s *AntigravityGatewayService) handleModelRateLimit(p *handleModelRateLimitParams) *handleModelRateLimitResult {
	if p.statusCode != 429 && p.statusCode != 503 {
		return &handleModelRateLimitResult{Handled: false}
	}

	info := parseAntigravitySmartRetryInfo(p.body)
	if info == nil || info.ModelName == "" {
		return &handleModelRateLimitResult{Handled: false}
	}

	// MODEL_CAPACITY_EXHAUSTED：模型容量不足，所有账号共享同一容量池
	// 切换账号无意义，不设置模型限流（实际重试由 handleSmartRetry 处理）
	if info.IsModelCapacityExhausted {
		log.Printf("%s status=%d model_capacity_exhausted model=%s (not switching account, retry handled by smart retry)",
			p.prefix, p.statusCode, info.ModelName)
		return &handleModelRateLimitResult{
			Handled: true,
		}
	}

	// RATE_LIMIT_EXCEEDED: < antigravityRateLimitThreshold: 等待后重试
	if info.RetryDelay < antigravityRateLimitThreshold {
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limit_wait model=%s wait=%v",
			p.prefix, p.statusCode, info.ModelName, info.RetryDelay)
		return &handleModelRateLimitResult{
			Handled:      true,
			ShouldRetry:  true,
			WaitDuration: info.RetryDelay,
		}
	}

	// RATE_LIMIT_EXCEEDED: >= antigravityRateLimitThreshold: 设置限流 + 清除粘性会话 + 切换账号
	s.setModelRateLimitAndClearSession(p, info)

	return &handleModelRateLimitResult{
		Handled: true,
		SwitchError: &AntigravityAccountSwitchError{
			OriginalAccountID: p.account.ID,
			RateLimitedModel:  info.ModelName,
			IsStickySession:   p.isStickySession,
		},
	}
}

// setModelRateLimitAndClearSession 设置模型限流并清除粘性会话
func (s *AntigravityGatewayService) setModelRateLimitAndClearSession(p *handleModelRateLimitParams, info *antigravitySmartRetryInfo) {
	resetAt := time.Now().Add(info.RetryDelay)
	logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d model_rate_limited model=%s account=%d reset_in=%v",
		p.prefix, p.statusCode, info.ModelName, p.account.ID, info.RetryDelay)

	// 设置模型限流状态（数据库）
	if err := s.accountRepo.SetModelRateLimit(p.ctx, p.account.ID, info.ModelName, resetAt); err != nil {
		logger.LegacyPrintf("service.antigravity_gateway", "%s model_rate_limit_failed model=%s error=%v", p.prefix, info.ModelName, err)
	}

	// 立即更新 Redis 快照中账号的限流状态，避免并发请求重复选中
	s.updateAccountModelRateLimitInCache(p.ctx, p.account, info.ModelName, resetAt)

	// 清除粘性会话绑定
	if p.cache != nil && p.sessionHash != "" {
		_ = p.cache.DeleteSessionAccountID(p.ctx, p.groupID, p.sessionHash)
	}
}

// updateAccountModelRateLimitInCache 立即更新 Redis 中账号的模型限流状态
func (s *AntigravityGatewayService) updateAccountModelRateLimitInCache(ctx context.Context, account *Account, modelKey string, resetAt time.Time) {
	if s.schedulerSnapshot == nil || account == nil || modelKey == "" {
		return
	}

	// 更新账号对象的 Extra 字段
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}

	limits, _ := account.Extra["model_rate_limits"].(map[string]any)
	if limits == nil {
		limits = make(map[string]any)
		account.Extra["model_rate_limits"] = limits
	}

	limits[modelKey] = map[string]any{
		"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
		"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
	}

	// 更新 Redis 快照
	if err := s.schedulerSnapshot.UpdateAccountInCache(ctx, account); err != nil {
		logger.LegacyPrintf("service.antigravity_gateway", "[antigravity-Forward] cache_update_failed account=%d model=%s err=%v", account.ID, modelKey, err)
	}
}

func (s *AntigravityGatewayService) handleUpstreamError(
	ctx context.Context, prefix string, account *Account,
	statusCode int, headers http.Header, body []byte,
	requestedModel string,
	groupID int64, sessionHash string, isStickySession bool,
) *handleModelRateLimitResult {
	// 遵守自定义错误码策略：未命中则跳过所有限流处理
	if !account.ShouldHandleErrorCode(statusCode) {
		return nil
	}
	// 模型级限流处理（优先）
	result := s.handleModelRateLimit(&handleModelRateLimitParams{
		ctx:             ctx,
		prefix:          prefix,
		account:         account,
		statusCode:      statusCode,
		body:            body,
		cache:           s.cache,
		groupID:         groupID,
		sessionHash:     sessionHash,
		isStickySession: isStickySession,
	})
	if result.Handled {
		return result
	}

	// 503 仅处理模型限流（MODEL_CAPACITY_EXHAUSTED），非模型限流不做额外处理
	// 避免将普通的 503 错误误判为账号问题
	if statusCode == 503 {
		return nil
	}

	// 429：尝试解析模型级限流，解析失败时兜底为账号级限流
	if statusCode == 429 {
		if logBody, maxBytes := s.getLogConfig(); logBody {
			logger.LegacyPrintf("service.antigravity_gateway", "[Antigravity-Debug] 429 response body: %s", truncateString(string(body), maxBytes))
		}

		resetAt := ParseGeminiRateLimitResetTime(body)
		defaultDur := s.getDefaultRateLimitDuration()

		// 尝试解析模型 key 并设置模型级限流
		//
		// 注意：requestedModel 可能是"映射前"的请求模型名（例如 claude-opus-4-6），
		// 调度与限流判定使用的是 Antigravity 最终模型名（包含映射与 thinking 后缀）。
		// 因此这里必须写入最终模型 key，确保后续调度能正确避开已限流模型。
		modelKey := resolveFinalAntigravityModelKey(ctx, account, requestedModel)
		if strings.TrimSpace(modelKey) == "" {
			// 极少数情况下无法映射（理论上不应发生：能转发成功说明映射已通过），
			// 保持旧行为作为兜底，避免完全丢失模型级限流记录。
			modelKey = resolveAntigravityModelKey(requestedModel)
		}
		if modelKey != "" {
			ra := s.resolveResetTime(resetAt, defaultDur)
			if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, modelKey, ra); err != nil {
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=429 model_rate_limit_set_failed model=%s error=%v", prefix, modelKey, err)
			} else {
				logger.LegacyPrintf("service.antigravity_gateway", "%s status=429 model_rate_limited model=%s account=%d reset_at=%v reset_in=%v",
					prefix, modelKey, account.ID, ra.Format("15:04:05"), time.Until(ra).Truncate(time.Second))
				s.updateAccountModelRateLimitInCache(ctx, account, modelKey, ra)
			}
			return nil
		}

		// 无法解析模型 key，兜底为账号级限流
		ra := s.resolveResetTime(resetAt, defaultDur)
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=429 rate_limited account=%d reset_at=%v reset_in=%v (fallback)",
			prefix, account.ID, ra.Format("15:04:05"), time.Until(ra).Truncate(time.Second))
		if err := s.accountRepo.SetRateLimited(ctx, account.ID, ra); err != nil {
			logger.LegacyPrintf("service.antigravity_gateway", "%s status=429 rate_limit_set_failed account=%d error=%v", prefix, account.ID, err)
		}
		return nil
	}
	// 其他错误码继续使用 rateLimitService
	if s.rateLimitService == nil {
		return nil
	}
	shouldDisable := s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
	if shouldDisable {
		logger.LegacyPrintf("service.antigravity_gateway", "%s status=%d marked_error", prefix, statusCode)
	}
	return nil
}

// getDefaultRateLimitDuration 获取默认限流时间
func (s *AntigravityGatewayService) getDefaultRateLimitDuration() time.Duration {
	defaultDur := antigravityDefaultRateLimitDuration
	if s.settingService != nil && s.settingService.cfg != nil && s.settingService.cfg.Gateway.AntigravityFallbackCooldownMinutes > 0 {
		defaultDur = time.Duration(s.settingService.cfg.Gateway.AntigravityFallbackCooldownMinutes) * time.Minute
	}
	if override, ok := antigravityFallbackCooldownSeconds(); ok {
		defaultDur = override
	}
	return defaultDur
}

// resolveResetTime 根据解析的重置时间或默认时长计算重置时间点
func (s *AntigravityGatewayService) resolveResetTime(resetAt *int64, defaultDur time.Duration) time.Time {
	if resetAt != nil {
		return time.Unix(*resetAt, 0)
	}
	return time.Now().Add(defaultDur)
}
