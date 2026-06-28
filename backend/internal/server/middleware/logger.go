package middleware

import (
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/ip"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		clientIP := ip.GetClientIP(c)
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}
		fields = appendProtocolRouteAccessFields(fields, c.Request.Context())

		l := logger.FromContext(c.Request.Context()).With(fields...)
		l.Info("http request completed", zap.Time("completed_at", endTime))

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}

func appendProtocolRouteAccessFields(fields []zap.Field, ctx interface {
	Value(key any) any
}) []zap.Field {
	if ctx == nil {
		return fields
	}
	if value, ok := ctx.Value(ctxkey.InboundProtocol).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("inbound_protocol", strings.TrimSpace(value)))
	}
	if value, ok := ctx.Value(ctxkey.TargetProtocol).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("target_protocol", strings.TrimSpace(value)))
	}
	if value, ok := ctx.Value(ctxkey.RelayMode).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("relay_mode", strings.TrimSpace(value)))
	}
	switch value := ctx.Value(ctxkey.ConversionChain).(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			fields = append(fields, zap.String("conversion_chain", strings.TrimSpace(value)))
		}
	case []string:
		if len(value) > 0 {
			fields = append(fields, zap.String("conversion_chain", strings.Join(value, " -> ")))
		}
	}
	if value, ok := ctx.Value(ctxkey.FinalRelayFormat).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("final_relay_format", strings.TrimSpace(value)))
	}
	return fields
}
