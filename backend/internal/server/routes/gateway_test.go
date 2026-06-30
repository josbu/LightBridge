package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/handler"
	servermiddleware "github.com/WilliamWang1721/LightBridge/internal/server/middleware"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: service.PlatformOpenAI},
			})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	return router
}

func TestShouldUseOpenAIHandler_CustomGroupOpenAIEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		want     bool
		fullPath string
	}{
		{name: "responses", path: "/v1/responses", want: true},
		{name: "responses compact", path: "/v1/responses/compact", want: true, fullPath: "/v1/responses/*subpath"},
		{name: "chat completions", path: "/v1/chat/completions", want: true},
		{name: "embeddings", path: "/v1/embeddings", want: true},
		{name: "images", path: "/v1/images/generations", want: true},
		{name: "anthropic messages", path: "/v1/messages", want: false},
		{name: "gemini native", path: "/v1beta/models/gemini:generateContent", want: false, fullPath: "/v1beta/models/*modelAction"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: service.PlatformCustom},
			})
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			c.Request = req
			fullPath := tt.fullPath
			if fullPath == "" {
				fullPath = tt.path
			}
			c.Set("_gateway_inbound_endpoint", handler.NormalizeInboundEndpoint(fullPath))

			require.Equal(t, tt.want, shouldUseOpenAIHandler(c))
		})
	}
}

func TestShouldUseGeminiHandler_IgnoresGroupPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	groupID := int64(1)
	c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{Platform: service.PlatformOpenAI},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)
	c.Request = req
	c.Set("_gateway_inbound_endpoint", handler.NormalizeInboundEndpoint("/v1beta/models/*modelAction"))

	require.True(t, shouldUseGeminiHandler(c))
	require.False(t, shouldUseOpenAIHandler(c))
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}
