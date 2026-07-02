package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

var errProtocolRouteUnsupported = errors.New("protocol route unsupported")

type protocolForwardResult struct {
	Gateway *service.ForwardResult
	OpenAI  *service.OpenAIForwardResult
}

func (r protocolForwardResult) GatewayResult() *service.ForwardResult {
	if r.Gateway != nil {
		return r.Gateway
	}
	return openAIForwardAsGatewayResult(r.OpenAI)
}

func openAIForwardAsGatewayResult(result *service.OpenAIForwardResult) *service.ForwardResult {
	if result == nil {
		return nil
	}
	return &service.ForwardResult{
		RequestID: result.RequestID,
		Usage: service.ClaudeUsage{
			InputTokens:              result.Usage.InputTokens,
			OutputTokens:             result.Usage.OutputTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
			ImageOutputTokens:        result.Usage.ImageOutputTokens,
		},
		Model:              result.Model,
		UpstreamModel:      result.UpstreamModel,
		Stream:             result.Stream,
		Duration:           result.Duration,
		FirstTokenMs:       result.FirstTokenMs,
		ReasoningEffort:    result.ReasoningEffort,
		ImageCount:         result.ImageCount,
		ImageSize:          result.ImageSize,
		ImageInputSize:     result.ImageInputSize,
		ImageOutputSize:    result.ImageOutputSize,
		ImageOutputSizes:   result.ImageOutputSizes,
		ImageSizeSource:    result.ImageSizeSource,
		ImageSizeBreakdown: result.ImageSizeBreakdown,
	}
}

func (h *GatewayHandler) routeContext(ctx context.Context, account *service.Account) (context.Context, service.ProtocolRouteDecision, error) {
	decision, ok := service.ProtocolRouteDecisionForAccount(ctx, account)
	if !ok {
		accountID := int64(0)
		relayMode := service.RelayModeRouter
		accountName := ""
		accountPlatform := ""
		if account != nil {
			accountID = account.ID
			accountName = account.Name
			accountPlatform = account.Platform
			relayMode = account.RelayMode()
		}
		failureReason := decision.FailureReason
		if failureReason == "" {
			failureReason = "unknown routing failure"
		}
		return ctx, decision, fmt.Errorf("%w: inbound=%s account_id=%d account_name=%s account_platform=%s relay_mode=%s reason=%s",
			errProtocolRouteUnsupported,
			decision.InboundProtocol,
			accountID,
			accountName,
			accountPlatform,
			relayMode,
			failureReason,
		)
	}
	routeCtx := service.WithProtocolRouteDecision(ctx, decision)
	routeLogger := logger.FromContext(routeCtx).With(appendProtocolRouteLogFields(nil, routeCtx)...)
	return logger.IntoContext(routeCtx, routeLogger), decision, nil
}

func (h *GatewayHandler) forwardMessagesViaProtocolRouter(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	parsedReq *service.ParsedRequest,
	promptCacheKey string,
	defaultMappedModel string,
	hasBoundSession bool,
) (protocolForwardResult, error) {
	routeCtx, decision, err := h.routeContext(ctx, account)
	if err != nil {
		return protocolForwardResult{}, err
	}
	c.Request = c.Request.WithContext(routeCtx)
	if decision.RelayMode == service.RelayModeFullPassthrough {
		result, err := h.forwardFullPassthrough(routeCtx, c, account, body, parsedReq.Model, parsedReq.Stream)
		return protocolForwardResult{Gateway: result}, err
	}

	switch decision.TargetProtocol {
	case service.CustomProtocolAnthropicMessages:
		if account.IsAntigravity() && account.Type != service.AccountTypeAPIKey {
			result, err := h.antigravityGatewayService.Forward(routeCtx, c, account, body, hasBoundSession)
			return protocolForwardResult{Gateway: result}, err
		}
		result, err := h.gatewayService.Forward(routeCtx, c, account, parsedReq)
		return protocolForwardResult{Gateway: result}, err

	case service.CustomProtocolOpenAIResponses, service.CustomProtocolOpenAIChatCompletions:
		if h.openAIGatewayService == nil {
			return protocolForwardResult{}, fmt.Errorf("%w: openai gateway service is not initialized (inbound=%s target=%s account_id=%d account_name=%s)",
				errProtocolRouteUnsupported, decision.InboundProtocol, decision.TargetProtocol, account.ID, account.Name)
		}
		result, err := h.openAIGatewayService.ForwardAsAnthropic(routeCtx, c, account, body, promptCacheKey, defaultMappedModel)
		return protocolForwardResult{OpenAI: result}, err

	case service.CustomProtocolGemini:
		if account.IsAntigravity() {
			result, err := h.antigravityGatewayService.ForwardGemini(routeCtx, c, account, parsedReq.Model, "generateContent", parsedReq.Stream, body, hasBoundSession)
			return protocolForwardResult{Gateway: result}, err
		}
		result, err := h.geminiCompatService.Forward(routeCtx, c, account, body)
		return protocolForwardResult{Gateway: result}, err
	}

	return protocolForwardResult{}, fmt.Errorf("%w: no forwarding handler for target protocol %q (inbound=%s account_id=%d account_name=%s)",
		errProtocolRouteUnsupported, decision.TargetProtocol, decision.InboundProtocol, account.ID, account.Name)
}

func (h *GatewayHandler) forwardResponsesViaProtocolRouter(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	parsedReq *service.ParsedRequest,
) (protocolForwardResult, error) {
	routeCtx, decision, err := h.routeContext(ctx, account)
	if err != nil {
		return protocolForwardResult{}, err
	}
	c.Request = c.Request.WithContext(routeCtx)
	if decision.RelayMode == service.RelayModeFullPassthrough {
		result, err := h.forwardFullPassthrough(routeCtx, c, account, body, parsedReq.Model, parsedReq.Stream)
		return protocolForwardResult{Gateway: result}, err
	}

	switch decision.TargetProtocol {
	case service.CustomProtocolOpenAIResponses, service.CustomProtocolOpenAIChatCompletions:
		if h.openAIGatewayService == nil {
			return protocolForwardResult{}, fmt.Errorf("%w: openai gateway service is not initialized (inbound=%s target=%s account_id=%d account_name=%s)",
				errProtocolRouteUnsupported, decision.InboundProtocol, decision.TargetProtocol, account.ID, account.Name)
		}
		result, err := h.openAIGatewayService.Forward(routeCtx, c, account, body)
		return protocolForwardResult{OpenAI: result}, err
	case service.CustomProtocolAnthropicMessages:
		result, err := h.gatewayService.ForwardAsResponses(routeCtx, c, account, body, parsedReq)
		return protocolForwardResult{Gateway: result}, err
	case service.CustomProtocolGemini:
		result, err := h.geminiCompatService.ForwardAsResponses(routeCtx, c, account, body)
		return protocolForwardResult{Gateway: result}, err
	default:
		return protocolForwardResult{}, fmt.Errorf("%w: no forwarding handler for target protocol %q (inbound=%s account_id=%d account_name=%s)",
			errProtocolRouteUnsupported, decision.TargetProtocol, decision.InboundProtocol, account.ID, account.Name)
	}
}

func (h *GatewayHandler) forwardChatCompletionsViaProtocolRouter(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	parsedReq *service.ParsedRequest,
	promptCacheKey string,
	defaultMappedModel string,
) (protocolForwardResult, error) {
	routeCtx, decision, err := h.routeContext(ctx, account)
	if err != nil {
		return protocolForwardResult{}, err
	}
	c.Request = c.Request.WithContext(routeCtx)
	if decision.RelayMode == service.RelayModeFullPassthrough {
		result, err := h.forwardFullPassthrough(routeCtx, c, account, body, parsedReq.Model, parsedReq.Stream)
		return protocolForwardResult{Gateway: result}, err
	}

	switch decision.TargetProtocol {
	case service.CustomProtocolOpenAIResponses, service.CustomProtocolOpenAIChatCompletions:
		if h.openAIGatewayService == nil {
			return protocolForwardResult{}, fmt.Errorf("%w: openai gateway service is not initialized (inbound=%s target=%s account_id=%d account_name=%s)",
				errProtocolRouteUnsupported, decision.InboundProtocol, decision.TargetProtocol, account.ID, account.Name)
		}
		result, err := h.openAIGatewayService.ForwardAsChatCompletions(routeCtx, c, account, body, promptCacheKey, defaultMappedModel)
		return protocolForwardResult{OpenAI: result}, err
	case service.CustomProtocolAnthropicMessages:
		result, err := h.gatewayService.ForwardAsChatCompletions(routeCtx, c, account, body, parsedReq)
		return protocolForwardResult{Gateway: result}, err
	case service.CustomProtocolGemini:
		result, err := h.geminiCompatService.ForwardAsChatCompletions(routeCtx, c, account, body)
		return protocolForwardResult{Gateway: result}, err
	default:
		return protocolForwardResult{}, fmt.Errorf("%w: no forwarding handler for target protocol %q (inbound=%s account_id=%d account_name=%s)",
			errProtocolRouteUnsupported, decision.TargetProtocol, decision.InboundProtocol, account.ID, account.Name)
	}
}

func (h *GatewayHandler) forwardGeminiNativeViaProtocolRouter(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	modelName string,
	action string,
	stream bool,
	body []byte,
	hasBoundSession bool,
) (protocolForwardResult, error) {
	routeCtx, decision, err := h.routeContext(ctx, account)
	if err != nil {
		return protocolForwardResult{}, err
	}
	c.Request = c.Request.WithContext(routeCtx)
	if decision.RelayMode == service.RelayModeFullPassthrough {
		result, err := h.forwardFullPassthrough(routeCtx, c, account, body, modelName, stream)
		return protocolForwardResult{Gateway: result}, err
	}

	switch decision.TargetProtocol {
	case service.CustomProtocolGemini:
		var result *service.ForwardResult
		if account.IsAntigravity() && account.Type != service.AccountTypeAPIKey {
			result, err = h.antigravityGatewayService.ForwardGemini(routeCtx, c, account, modelName, action, stream, body, hasBoundSession)
		} else if account.UsesBearerAuth() {
			if upErr := h.ensureAistudioProxy(routeCtx, account); upErr != nil {
				return protocolForwardResult{}, upErr
			}
			result, err = h.geminiCompatService.ForwardNative(routeCtx, c, account, modelName, action, stream, body)
		} else {
			result, err = h.geminiCompatService.ForwardNative(routeCtx, c, account, modelName, action, stream, body)
		}
		return protocolForwardResult{Gateway: result}, err

	case service.CustomProtocolAnthropicMessages:
		claudeBody, err := service.GeminiGenerateContentToAnthropicMessages(body, modelName, stream)
		if err != nil {
			return protocolForwardResult{}, err
		}
		parsedReq, err := service.ParseGatewayRequest(claudeBody, service.PlatformAnthropic)
		if err != nil {
			return protocolForwardResult{}, err
		}
		capture, rec := cloneGinContextForProtocolCapture(c, routeCtx, "/v1/messages", claudeBody)
		result, err := h.gatewayService.Forward(routeCtx, capture, account, parsedReq)
		if err != nil {
			_ = service.WriteCapturedAnthropicAsGemini(c, rec.Code, rec.Header(), rec.Body.Bytes(), stream, modelName)
			return protocolForwardResult{Gateway: result}, err
		}
		if writeErr := service.WriteCapturedAnthropicAsGemini(c, rec.Code, rec.Header(), rec.Body.Bytes(), stream, modelName); writeErr != nil {
			return protocolForwardResult{Gateway: result}, writeErr
		}
		return protocolForwardResult{Gateway: result}, nil

	case service.CustomProtocolOpenAIResponses, service.CustomProtocolOpenAIChatCompletions:
		if h.openAIGatewayService == nil {
			return protocolForwardResult{}, fmt.Errorf("%w: openai gateway service is not initialized (inbound=%s target=%s account_id=%d account_name=%s)",
				errProtocolRouteUnsupported, decision.InboundProtocol, decision.TargetProtocol, account.ID, account.Name)
		}
		claudeBody, err := service.GeminiGenerateContentToAnthropicMessages(body, modelName, stream)
		if err != nil {
			return protocolForwardResult{}, err
		}
		capture, rec := cloneGinContextForProtocolCapture(c, routeCtx, "/v1/messages", claudeBody)
		result, err := h.openAIGatewayService.ForwardAsAnthropic(routeCtx, capture, account, claudeBody, "", modelName)
		if err != nil {
			_ = service.WriteCapturedAnthropicAsGemini(c, rec.Code, rec.Header(), rec.Body.Bytes(), stream, modelName)
			return protocolForwardResult{OpenAI: result}, err
		}
		if writeErr := service.WriteCapturedAnthropicAsGemini(c, rec.Code, rec.Header(), rec.Body.Bytes(), stream, modelName); writeErr != nil {
			return protocolForwardResult{OpenAI: result}, writeErr
		}
		return protocolForwardResult{OpenAI: result}, nil
	}

	return protocolForwardResult{}, fmt.Errorf("%w: no forwarding handler for target protocol %q (inbound=%s account_id=%d account_name=%s)",
		errProtocolRouteUnsupported, decision.TargetProtocol, decision.InboundProtocol, account.ID, account.Name)
}

func (h *GatewayHandler) forwardFullPassthrough(
	ctx context.Context,
	c *gin.Context,
	account *service.Account,
	body []byte,
	requestModel string,
	requestStream bool,
) (*service.ForwardResult, error) {
	if h.gatewayService == nil {
		return nil, fmt.Errorf("%w: gateway service is not initialized for full passthrough (account_id=%d account_name=%s)",
			errProtocolRouteUnsupported, account.ID, account.Name)
	}
	token := ""
	tokenType := ""
	var err error
	if account != nil && account.IsOpenAI() && h.openAIGatewayService != nil {
		token, tokenType, err = h.openAIGatewayService.GetAccessToken(ctx, account)
	} else {
		token, tokenType, err = h.gatewayService.GetAccessToken(ctx, account)
	}
	if err != nil {
		return nil, err
	}
	return h.gatewayService.ForwardFullPassthrough(ctx, c, account, service.FullPassthroughInput{
		Body:          body,
		Token:         token,
		TokenType:     tokenType,
		RequestModel:  requestModel,
		RequestStream: requestStream,
	})
}

func cloneGinContextForProtocolCapture(parent *gin.Context, ctx context.Context, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	capture, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
	if parent != nil {
		if parent.Request != nil {
			req.Header = parent.Request.Header.Clone()
		}
		if len(parent.Keys) > 0 {
			capture.Keys = make(map[string]any, len(parent.Keys))
			for k, v := range parent.Keys {
				capture.Keys[k] = v
			}
		}
	}
	req.Header.Set("Content-Type", "application/json")
	capture.Request = req
	return capture, rec
}
