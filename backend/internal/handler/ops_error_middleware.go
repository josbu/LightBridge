package handler

import (
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/ip"
	middleware2 "github.com/WilliamWang1721/LightBridge/internal/server/middleware"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

// OpsErrorLoggerMiddleware records error responses (status >= 400) into ops_error_logs.
//
// Notes:
// - It buffers response bodies only when status >= 400 to avoid overhead for successful traffic.
// - Streaming errors after the response has started (SSE) may still need explicit logging.
func OpsErrorLoggerMiddleware(ops *service.OpsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		originalWriter := c.Writer
		w := acquireOpsCaptureWriter(originalWriter)
		defer func() {
			if c.Writer == w {
				// No inner middleware wrapped our writer, so we can cleanly
				// detach it and recycle it into the pool.
				c.Writer = originalWriter
				releaseOpsCaptureWriter(w)
				return
			}
			// An inner middleware (e.g. PrivacyFilterResponseWriter) wrapped our
			// writer and still embeds it as its ResponseWriter. Outer middlewares
			// (Logger/Recovery) keep calling methods through that wrapper after we
			// return, so we MUST NOT nil out w or return it to the pool here —
			// doing so left a dangling *opsCaptureWriter whose ResponseWriter was
			// nil, causing a nil-pointer panic in c.Writer.Status()/Written()
			// (and a sync.Pool data race). Leave it live; it is GC'd together with
			// the wrapping writer when the request completes.
		}()
		c.Writer = w
		c.Next()

		if ops == nil {
			return
		}
		if !ops.IsMonitoringEnabled(c.Request.Context()) {
			return
		}

		status := c.Writer.Status()
		if status < 400 {
			// Even when the client request succeeds, we still want to persist upstream error attempts
			// (retries/failover) so ops can observe upstream instability that gets "covered" by retries.
			var events []*service.OpsUpstreamErrorEvent
			if v, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
				if arr, ok := v.([]*service.OpsUpstreamErrorEvent); ok && len(arr) > 0 {
					events = arr
				}
			}
			// Also accept single upstream fields set by gateway services (rare for successful requests).
			hasUpstreamContext := len(events) > 0
			if !hasUpstreamContext {
				if v, ok := c.Get(service.OpsUpstreamStatusCodeKey); ok {
					switch t := v.(type) {
					case int:
						hasUpstreamContext = t > 0
					case int64:
						hasUpstreamContext = t > 0
					}
				}
			}
			if !hasUpstreamContext {
				if v, ok := c.Get(service.OpsUpstreamErrorMessageKey); ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						hasUpstreamContext = true
					}
				}
			}
			if !hasUpstreamContext {
				if v, ok := c.Get(service.OpsUpstreamErrorDetailKey); ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						hasUpstreamContext = true
					}
				}
			}
			// Also log routing-capacity errors (e.g. "no available accounts") that occur in streaming
			// mode. When streamStarted=true, handleStreamingAwareError sends an SSE event but does
			// NOT set the HTTP status code (it stays 200), so the status < 400 branch is entered.
			// Without upstream error context these would be silently dropped. The routing-capacity
			// flag is set by markOpsRoutingCapacityLimitedIfNoAvailable before the error response.
			routingCapacityLimited := isOpsRoutingCapacityLimited(c)
			if !hasUpstreamContext && !routingCapacityLimited {
				return
			}

			apiKey, _ := middleware2.GetAPIKeyFromContext(c)
			clientRequestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)

			model, _ := c.Get(opsModelKey)
			streamV, _ := c.Get(opsStreamKey)
			accountIDV, _ := c.Get(opsAccountIDKey)

			var modelName string
			if s, ok := model.(string); ok {
				modelName = s
			}
			stream := false
			if b, ok := streamV.(bool); ok {
				stream = b
			}

			// Prefer showing the account that experienced the upstream error (if we have events),
			// otherwise fall back to the final selected account (best-effort).
			var accountID *int64
			if len(events) > 0 {
				if last := events[len(events)-1]; last != nil && last.AccountID > 0 {
					v := last.AccountID
					accountID = &v
				}
			}
			if accountID == nil {
				if v, ok := accountIDV.(int64); ok && v > 0 {
					accountID = &v
				}
			}

			fallbackPlatform := guessPlatformFromPath(c.Request.URL.Path)
			platform := resolveOpsPlatform(c.Request.Context(), apiKey, fallbackPlatform)

			requestID := c.Writer.Header().Get("X-Request-Id")
			if requestID == "" {
				requestID = c.Writer.Header().Get("x-request-id")
			}

			// Best-effort backfill single upstream fields from the last event (if present).
			var upstreamStatusCode *int
			var upstreamErrorMessage *string
			var upstreamErrorDetail *string
			if len(events) > 0 {
				last := events[len(events)-1]
				if last != nil {
					if last.UpstreamStatusCode > 0 {
						code := last.UpstreamStatusCode
						upstreamStatusCode = &code
					}
					if msg := strings.TrimSpace(last.Message); msg != "" {
						upstreamErrorMessage = &msg
					}
					if detail := strings.TrimSpace(last.Detail); detail != "" {
						upstreamErrorDetail = &detail
					}
				}
			}

			if upstreamStatusCode == nil {
				if v, ok := c.Get(service.OpsUpstreamStatusCodeKey); ok {
					switch t := v.(type) {
					case int:
						if t > 0 {
							code := t
							upstreamStatusCode = &code
						}
					case int64:
						if t > 0 {
							code := int(t)
							upstreamStatusCode = &code
						}
					}
				}
			}
			if upstreamErrorMessage == nil {
				if v, ok := c.Get(service.OpsUpstreamErrorMessageKey); ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						msg := strings.TrimSpace(s)
						upstreamErrorMessage = &msg
					}
				}
			}
			if upstreamErrorDetail == nil {
				if v, ok := c.Get(service.OpsUpstreamErrorDetailKey); ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						detail := strings.TrimSpace(s)
						upstreamErrorDetail = &detail
					}
				}
			}

			// If we still have nothing meaningful, skip — unless this is a routing-capacity
			// error (e.g. "no available accounts") which should always be logged.
			if upstreamStatusCode == nil && upstreamErrorMessage == nil && upstreamErrorDetail == nil && len(events) == 0 && !routingCapacityLimited {
				return
			}

			effectiveUpstreamStatus := 0
			if upstreamStatusCode != nil {
				effectiveUpstreamStatus = *upstreamStatusCode
			}

			// For routing-capacity errors in streaming mode, the intended error status is 503
			// (not the HTTP 200 that the SSE response carries).
			effectiveStatus := status
			if routingCapacityLimited && status < 400 {
				effectiveStatus = 503
			}

			recoveredMsg := "Recovered upstream error"
			if routingCapacityLimited && !hasUpstreamContext {
				// Routing-capacity error: derive message from the error body captured by the
				// response writer (the handler wrote a JSON 503 before the SSE fallback).
				recoveredMsg = parseOpsErrorResponse(w.buf.Bytes()).Message
				if recoveredMsg == "" {
					recoveredMsg = "No available accounts"
				}
			} else {
				if effectiveUpstreamStatus > 0 {
					recoveredMsg += " " + strconvItoa(effectiveUpstreamStatus)
				}
				if upstreamErrorMessage != nil && strings.TrimSpace(*upstreamErrorMessage) != "" {
					recoveredMsg += ": " + strings.TrimSpace(*upstreamErrorMessage)
				}
			}
			recoveredMsg = truncateString(recoveredMsg, 2048)
			phase, isBusinessLimited, errorOwner, errorSource := classifyOpsErrorLog(c, "api_error", recoveredMsg, "", effectiveStatus)

			entry := &service.OpsInsertErrorLogInput{
				RequestID:       requestID,
				ClientRequestID: clientRequestID,

				AccountID: accountID,
				Platform:  platform,
				Model:     modelName,
				RequestPath: func() string {
					if c.Request != nil && c.Request.URL != nil {
						return c.Request.URL.Path
					}
					return ""
				}(),
				Stream:           stream,
				InboundEndpoint:  GetInboundEndpoint(c),
				UpstreamEndpoint: GetUpstreamEndpoint(c, platform),
				RequestedModel:   modelName,
				UpstreamModel: func() string {
					if v, ok := c.Get(opsUpstreamModelKey); ok {
						if s, ok := v.(string); ok {
							return strings.TrimSpace(s)
						}
					}
					return ""
				}(),
				RequestType: func() *int16 {
					if v, ok := c.Get(opsRequestTypeKey); ok {
						switch t := v.(type) {
						case int16:
							return &t
						case int:
							v16 := int16(t)
							return &v16
						}
					}
					return nil
				}(),
				UserAgent: c.GetHeader("User-Agent"),

				ErrorPhase:        phase,
				ErrorType:         "api_error",
				Severity:          classifyOpsSeverity("api_error", effectiveStatus),
				StatusCode:        effectiveStatus,
				IsBusinessLimited: isBusinessLimited,
				IsCountTokens:     isCountTokensRequest(c),

				ErrorMessage: recoveredMsg,
				ErrorBody:    w.buf.String(),

				ErrorSource: errorSource,
				ErrorOwner:  errorOwner,

				UpstreamStatusCode:   upstreamStatusCode,
				UpstreamErrorMessage: upstreamErrorMessage,
				UpstreamErrorDetail:  upstreamErrorDetail,
				UpstreamErrors:       events,

				CreatedAt: time.Now(),
			}
			applyOpsSchedulerDiagnosticsFromContext(c, entry)
			applyOpsLatencyFieldsFromContext(c, entry)

			if apiKey != nil {
				entry.APIKeyID = &apiKey.ID
				if apiKey.User != nil {
					entry.UserID = &apiKey.User.ID
				}
				if apiKey.GroupID != nil {
					entry.GroupID = apiKey.GroupID
				}
				if platform := resolveOpsPlatform(c.Request.Context(), apiKey, entry.Platform); platform != "" {
					entry.Platform = platform
				}
			}

			var clientIP string
			if ip := strings.TrimSpace(ip.GetClientIP(c)); ip != "" {
				clientIP = ip
				entry.ClientIP = &clientIP
			}

			// Skip logging if a passthrough rule with skip_monitoring=true matched.
			if v, ok := c.Get(service.OpsSkipPassthroughKey); ok {
				if skip, _ := v.(bool); skip {
					return
				}
			}

			enqueueOpsErrorLog(ops, entry)
			return
		}

		body := w.buf.Bytes()
		parsed := parseOpsErrorResponse(body)

		// Skip logging if a passthrough rule with skip_monitoring=true matched.
		if v, ok := c.Get(service.OpsSkipPassthroughKey); ok {
			if skip, _ := v.(bool); skip {
				return
			}
		}

		// Skip logging if the error should be filtered based on settings
		if shouldSkipOpsErrorLog(c.Request.Context(), ops, parsed.Message, string(body), c.Request.URL.Path) {
			return
		}

		apiKey, _ := middleware2.GetAPIKeyFromContext(c)

		clientRequestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)

		model, _ := c.Get(opsModelKey)
		streamV, _ := c.Get(opsStreamKey)
		accountIDV, _ := c.Get(opsAccountIDKey)

		var modelName string
		if s, ok := model.(string); ok {
			modelName = s
		}
		stream := false
		if b, ok := streamV.(bool); ok {
			stream = b
		}
		var accountID *int64
		if v, ok := accountIDV.(int64); ok && v > 0 {
			accountID = &v
		}

		fallbackPlatform := guessPlatformFromPath(c.Request.URL.Path)
		platform := resolveOpsPlatform(c.Request.Context(), apiKey, fallbackPlatform)

		requestID := c.Writer.Header().Get("X-Request-Id")
		if requestID == "" {
			requestID = c.Writer.Header().Get("x-request-id")
		}

		normalizedType := normalizeOpsErrorType(parsed.ErrorType, parsed.Code)

		phase, isBusinessLimited, errorOwner, errorSource := classifyOpsErrorLog(c, normalizedType, parsed.Message, parsed.Code, status)

		entry := &service.OpsInsertErrorLogInput{
			RequestID:       requestID,
			ClientRequestID: clientRequestID,

			AccountID: accountID,
			Platform:  platform,
			Model:     modelName,
			RequestPath: func() string {
				if c.Request != nil && c.Request.URL != nil {
					return c.Request.URL.Path
				}
				return ""
			}(),
			Stream:           stream,
			InboundEndpoint:  GetInboundEndpoint(c),
			UpstreamEndpoint: GetUpstreamEndpoint(c, platform),
			RequestedModel:   modelName,
			UpstreamModel: func() string {
				if v, ok := c.Get(opsUpstreamModelKey); ok {
					if s, ok := v.(string); ok {
						return strings.TrimSpace(s)
					}
				}
				return ""
			}(),
			RequestType: func() *int16 {
				if v, ok := c.Get(opsRequestTypeKey); ok {
					switch t := v.(type) {
					case int16:
						return &t
					case int:
						v16 := int16(t)
						return &v16
					}
				}
				return nil
			}(),
			UserAgent: c.GetHeader("User-Agent"),

			ErrorPhase:        phase,
			ErrorType:         normalizedType,
			Severity:          classifyOpsSeverity(normalizedType, status),
			StatusCode:        status,
			IsBusinessLimited: isBusinessLimited,
			IsCountTokens:     isCountTokensRequest(c),

			ErrorMessage: parsed.Message,
			// Keep the full captured error body (capture is already capped at 64KB) so the
			// service layer can sanitize JSON before truncating for storage.
			ErrorBody:   string(body),
			ErrorSource: errorSource,
			ErrorOwner:  errorOwner,

			CreatedAt: time.Now(),
		}
		applyOpsLatencyFieldsFromContext(c, entry)

		// Capture upstream error context set by gateway services (if present).
		// This does NOT affect the client response; it enriches Ops troubleshooting data.
		{
			if v, ok := c.Get(service.OpsUpstreamStatusCodeKey); ok {
				switch t := v.(type) {
				case int:
					if t > 0 {
						code := t
						entry.UpstreamStatusCode = &code
					}
				case int64:
					if t > 0 {
						code := int(t)
						entry.UpstreamStatusCode = &code
					}
				}
			}
			if v, ok := c.Get(service.OpsUpstreamErrorMessageKey); ok {
				if s, ok := v.(string); ok {
					if msg := strings.TrimSpace(s); msg != "" {
						entry.UpstreamErrorMessage = &msg
					}
				}
			}
			if v, ok := c.Get(service.OpsUpstreamErrorDetailKey); ok {
				if s, ok := v.(string); ok {
					if detail := strings.TrimSpace(s); detail != "" {
						entry.UpstreamErrorDetail = &detail
					}
				}
			}
			if v, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
				if events, ok := v.([]*service.OpsUpstreamErrorEvent); ok && len(events) > 0 {
					entry.UpstreamErrors = events
					// Best-effort backfill the single upstream fields from the last event when missing.
					last := events[len(events)-1]
					if last != nil {
						if entry.UpstreamStatusCode == nil && last.UpstreamStatusCode > 0 {
							code := last.UpstreamStatusCode
							entry.UpstreamStatusCode = &code
						}
						if entry.UpstreamErrorMessage == nil && strings.TrimSpace(last.Message) != "" {
							msg := strings.TrimSpace(last.Message)
							entry.UpstreamErrorMessage = &msg
						}
						if entry.UpstreamErrorDetail == nil && strings.TrimSpace(last.Detail) != "" {
							detail := strings.TrimSpace(last.Detail)
							entry.UpstreamErrorDetail = &detail
						}
					}
				}
			}
		}
		applyOpsSchedulerDiagnosticsFromContext(c, entry)

		if apiKey != nil {
			entry.APIKeyID = &apiKey.ID
			if apiKey.User != nil {
				entry.UserID = &apiKey.User.ID
			}
			if apiKey.GroupID != nil {
				entry.GroupID = apiKey.GroupID
			}
			if platform := resolveOpsPlatform(c.Request.Context(), apiKey, entry.Platform); platform != "" {
				entry.Platform = platform
			}
		}

		var clientIP string
		if ip := strings.TrimSpace(ip.GetClientIP(c)); ip != "" {
			clientIP = ip
			entry.ClientIP = &clientIP
		}

		enqueueOpsErrorLog(ops, entry)
	}
}
