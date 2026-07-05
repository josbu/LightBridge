package middleware

import (
	"context"
	"net/http"

	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

type ProgressiveFeatureReader interface {
	IsProgressiveFeatureEnabled(ctx context.Context, feature service.ProgressiveFeature) bool
}

// RequireProgressiveFeature rejects requests before they reach feature handlers
// when the corresponding progressive feature is disabled.
func RequireProgressiveFeature(reader ProgressiveFeatureReader, feature service.ProgressiveFeature) gin.HandlerFunc {
	def, ok := service.ProgressiveFeatureDefinitionFor(feature)
	defaultEnabled := false
	if ok {
		defaultEnabled = def.DefaultEnabled
	}

	return func(c *gin.Context) {
		enabled := defaultEnabled
		if reader != nil {
			enabled = reader.IsProgressiveFeatureEnabled(c.Request.Context(), feature)
		}
		if !enabled {
			AbortWithError(c, http.StatusNotFound, "FEATURE_DISABLED", "feature is disabled")
			return
		}
		c.Next()
	}
}
