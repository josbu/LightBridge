package handler

import (
	"net/http"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

type ModuleHandler struct{ service *service.ModuleService }

func NewModuleHandler(s *service.ModuleService) *ModuleHandler { return &ModuleHandler{service: s} }
func (h *ModuleHandler) UIManifest(c *gin.Context) {
	items, err := h.service.UIManifest(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"modules": items})
}
func (h *ModuleHandler) ProviderAccountForms(c *gin.Context) {
	items, err := h.service.ProviderAccountForms(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"forms": items})
}

// Asset serves a file only from the enabled version of a module package.
func (h *ModuleHandler) Asset(c *gin.Context) {
	asset, err := h.service.ResolveEnabledAsset(c.Request.Context(), c.Param("module"), c.Param("version"), c.Param("path"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.File(asset)
}
