package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/ctxkey"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

func setOpsRequestContext(c *gin.Context, model string, stream bool) {
	if c == nil {
		return
	}
	model = strings.TrimSpace(model)
	c.Set(opsModelKey, model)
	c.Set(opsStreamKey, stream)
	if c.Request != nil && model != "" {
		ctx := context.WithValue(c.Request.Context(), ctxkey.Model, model)
		c.Request = c.Request.WithContext(ctx)
	}
}

// setOpsEndpointContext stores upstream model and request type for ops error logging.
// Called by handlers after model mapping and request type determination.
func setOpsEndpointContext(c *gin.Context, upstreamModel string, requestType int16) {
	if c == nil {
		return
	}
	if upstreamModel = strings.TrimSpace(upstreamModel); upstreamModel != "" {
		c.Set(opsUpstreamModelKey, upstreamModel)
	}
	c.Set(opsRequestTypeKey, requestType)
}

func setOpsSelectedAccount(c *gin.Context, accountID int64, platform ...string) {
	if c == nil || accountID <= 0 {
		return
	}
	c.Set(opsAccountIDKey, accountID)
	if c.Request != nil {
		ctx := context.WithValue(c.Request.Context(), ctxkey.AccountID, accountID)
		if len(platform) > 0 {
			p := strings.TrimSpace(platform[0])
			if p != "" {
				ctx = context.WithValue(ctx, ctxkey.Platform, p)
			}
		}
		c.Request = c.Request.WithContext(ctx)
	}
}

func markOpsRoutingCapacityLimited(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(opsRoutingCapacityLimitedKey, true)
}

func markOpsRoutingCapacityLimitedIfNoAvailable(c *gin.Context, err error) {
	if !isOpsNoAvailableAccountError(err) {
		return
	}
	if err != nil {
		service.SetOpsSchedulerDiagnosticsDetail(c, err.Error())
	}
	markOpsRoutingCapacityLimited(c)
}

func isOpsRoutingCapacityLimited(c *gin.Context) bool {
	if c == nil {
		return false
	}
	v, ok := c.Get(opsRoutingCapacityLimitedKey)
	if !ok {
		return false
	}
	marked, _ := v.(bool)
	return marked
}

func isOpsNoAvailableAccountError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrNoAvailableAccounts) || errors.Is(err, service.ErrNoAvailableCompactAccounts) {
		return true
	}
	return isOpsNoAvailableAccountMessage(err.Error())
}

func applyOpsSchedulerDiagnosticsFromContext(c *gin.Context, entry *service.OpsInsertErrorLogInput) {
	if c == nil || entry == nil {
		return
	}
	diagnostics := service.GetOpsSchedulerDiagnostics(c)
	if diagnostics == "" {
		return
	}
	if entry.UpstreamErrorDetail == nil || strings.TrimSpace(*entry.UpstreamErrorDetail) == "" {
		detail := diagnostics
		entry.UpstreamErrorDetail = &detail
	}
}
