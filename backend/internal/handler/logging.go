package handler

import (
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func requestLogger(c *gin.Context, component string, fields ...zap.Field) *zap.Logger {
	base := logger.L()
	if c != nil && c.Request != nil {
		base = logger.FromContext(c.Request.Context())
	}

	if component != "" {
		fields = append([]zap.Field{zap.String("component", component)}, fields...)
	}
	if c != nil && c.Request != nil {
		fields = appendProtocolRouteLogFields(fields, c.Request.Context())
	}
	return base.With(fields...)
}

func appendProtocolRouteLogFields(fields []zap.Field, ctx interface {
	Value(key any) any
}) []zap.Field {
	if ctx == nil {
		return fields
	}
	if value, ok := ctx.Value(ctxkey.InboundProtocol).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("inbound_protocol", value))
	}
	if value, ok := ctx.Value(ctxkey.TargetProtocol).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("target_protocol", value))
	}
	if value, ok := ctx.Value(ctxkey.RelayMode).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("relay_mode", value))
	}
	switch value := ctx.Value(ctxkey.ConversionChain).(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			fields = append(fields, zap.String("conversion_chain", value))
		}
	case []string:
		if len(value) > 0 {
			fields = append(fields, zap.String("conversion_chain", strings.Join(value, " -> ")))
		}
	}
	if value, ok := ctx.Value(ctxkey.FinalRelayFormat).(string); ok && strings.TrimSpace(value) != "" {
		fields = append(fields, zap.String("final_relay_format", value))
	}
	return fields
}
