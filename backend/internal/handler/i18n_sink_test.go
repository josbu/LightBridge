package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newErrorTestContext builds a gin context carrying the given Accept-Language.
func newErrorTestContext(acceptLang string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if acceptLang != "" {
		req.Header.Set("Accept-Language", acceptLang)
	}
	c.Request = req
	return c, rec
}

func TestErrorResponseLocalization(t *testing.T) {
	h := &GatewayHandler{}

	t.Run("zh Accept-Language translates message", func(t *testing.T) {
		c, rec := newErrorTestContext("zh-CN,zh;q=0.9")
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
		if !strings.Contains(rec.Body.String(), "无可用账号") {
			t.Fatalf("expected translated body, got %q", rec.Body.String())
		}
	})

	t.Run("english passes through unchanged", func(t *testing.T) {
		c, rec := newErrorTestContext("en-US")
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
		if !strings.Contains(rec.Body.String(), "No available accounts") {
			t.Fatalf("expected english body, got %q", rec.Body.String())
		}
	})

	t.Run("upstream 5xx mapping is translated for zh", func(t *testing.T) {
		c, rec := newErrorTestContext("zh")
		// mapUpstreamError(503) yields the English "Upstream service temporarily unavailable (upstream_status=503)".
		_, errType, msg := h.mapUpstreamError(503)
		h.errorResponse(c, http.StatusBadGateway, errType, msg)
		if !strings.Contains(rec.Body.String(), "上游服务暂时不可用") {
			t.Fatalf("expected translated upstream message, got %q", rec.Body.String())
		}
	})
}
