package routes

import (
	"github.com/WilliamWang1721/LightBridge/internal/handler"
	"github.com/WilliamWang1721/LightBridge/internal/server/middleware"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterModuleRoutes(v1 *gin.RouterGroup, h *handler.Handlers, settingService *service.SettingService) {
	if h == nil || h.Module == nil {
		return
	}
	modules := v1.Group("/modules")
	modules.Use(middleware.RequireProgressiveFeature(settingService, service.ProgressiveFeatureModuleRuntime))
	modules.GET("/ui", h.Module.UIManifest)
	modules.GET("/account-forms", h.Module.ProviderAccountForms)
}

func RegisterModuleAssetRoutes(r *gin.Engine, h *handler.Handlers, settingService *service.SettingService) {
	if h == nil || h.Module == nil {
		return
	}
	assets := r.Group("/modules")
	assets.Use(middleware.RequireProgressiveFeature(settingService, service.ProgressiveFeatureModuleRuntime))
	assets.GET("/:module/:version/*path", h.Module.Asset)
}
