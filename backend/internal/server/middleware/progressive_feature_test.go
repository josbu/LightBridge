package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type progressiveFeatureReaderStub struct {
	enabled bool
	calls   int
	feature service.ProgressiveFeature
}

func (r *progressiveFeatureReaderStub) IsProgressiveFeatureEnabled(_ context.Context, feature service.ProgressiveFeature) bool {
	r.calls++
	r.feature = feature
	return r.enabled
}

func TestRequireProgressiveFeatureBlocksDisabledFeature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &progressiveFeatureReaderStub{enabled: false}
	called := false

	router := gin.New()
	router.GET("/redeem", RequireProgressiveFeature(reader, service.ProgressiveFeatureRedeem), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/redeem", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "FEATURE_DISABLED")
	require.False(t, called)
	require.Equal(t, 1, reader.calls)
	require.Equal(t, service.ProgressiveFeatureRedeem, reader.feature)
}

func TestRequireProgressiveFeatureAllowsEnabledFeature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &progressiveFeatureReaderStub{enabled: true}
	called := false

	router := gin.New()
	router.GET("/redeem", RequireProgressiveFeature(reader, service.ProgressiveFeatureRedeem), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/redeem", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, called)
}

func TestRequireProgressiveFeatureUsesRegistryDefaultWithoutReader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/payment", RequireProgressiveFeature(nil, service.ProgressiveFeaturePayment), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/redeem", RequireProgressiveFeature(nil, service.ProgressiveFeatureRedeem), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	payment := httptest.NewRecorder()
	router.ServeHTTP(payment, httptest.NewRequest(http.MethodGet, "/payment", nil))
	require.Equal(t, http.StatusNotFound, payment.Code)

	redeem := httptest.NewRecorder()
	router.ServeHTTP(redeem, httptest.NewRequest(http.MethodGet, "/redeem", nil))
	require.Equal(t, http.StatusNoContent, redeem.Code)
}
