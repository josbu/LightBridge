package handler

import (
	"github.com/Wei-Shaw/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
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
