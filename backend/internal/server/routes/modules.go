package routes

import (
	"github.com/Wei-Shaw/LightBridge/internal/handler"
	"github.com/gin-gonic/gin"
	"path/filepath"
	"strings"
)

func RegisterModuleRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Module == nil {
		return
	}
	modules := v1.Group("/modules")
	modules.GET("/ui", h.Module.UIManifest)
	modules.GET("/account-forms", h.Module.ProviderAccountForms)
}
func RegisterModuleAssetRoutes(r *gin.Engine, dataDir string) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	root := filepath.Join(dataDir, "modules")
	r.GET("/modules/:module/:version/*path", func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("path"), "/")
		c.File(filepath.Join(root, c.Param("module"), c.Param("version"), rel))
	})
}
