package admin

import (
	"github.com/Wei-Shaw/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type ModuleHandler struct{ service *service.ModuleService }

func NewModuleHandler(s *service.ModuleService) *ModuleHandler { return &ModuleHandler{service: s} }
func (h *ModuleHandler) ListInstalled(c *gin.Context) {
	items, err := h.service.ListInstalled(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"modules": items})
}
func (h *ModuleHandler) Marketplace(c *gin.Context) {
	result, err := h.service.Marketplace(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *ModuleHandler) InstallFromMarketplace(c *gin.Context) {
	var req struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	_ = c.ShouldBindJSON(&req)
	item, err := h.service.InstallFromMarketplace(c.Request.Context(), req.ID, req.Version)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) Enable(c *gin.Context) {
	item, err := h.service.Enable(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) Disable(c *gin.Context) {
	item, err := h.service.Disable(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) Permissions(c *gin.Context) {
	item, err := h.service.Permissions(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) ApprovePermissions(c *gin.Context) {
	item, err := h.service.ApprovePermissions(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) Uninstall(c *gin.Context) {
	item, err := h.service.Uninstall(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
func (h *ModuleHandler) Purge(c *gin.Context) {
	item, err := h.service.Purge(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
