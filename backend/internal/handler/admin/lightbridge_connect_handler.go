package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

// LightBridgeConnectHandler handles LightBridge Connect operations
type LightBridgeConnectHandler struct {
	lbcService *service.LightBridgeConnectService
	db         *sql.DB
}

// NewLightBridgeConnectHandler creates a new handler
func NewLightBridgeConnectHandler(lbcService *service.LightBridgeConnectService, db *sql.DB) *LightBridgeConnectHandler {
	return &LightBridgeConnectHandler{
		lbcService: lbcService,
		db:         db,
	}
}

// VerifyTokenRequest represents the verification request
type VerifyTokenRequest struct {
	Type        string `json:"type" binding:"required"` // "new-api"
	InstanceURL string `json:"instance_url" binding:"required,url"`
	SystemToken string `json:"system_token" binding:"required"`
}

// VerifyTokenResponse represents the verification response
type VerifyTokenResponse struct {
	Valid       bool   `json:"valid"`
	UserID      int    `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Quota       int64  `json:"quota,omitempty"`      // balance in cents
	UsedQuota   int64  `json:"used_quota,omitempty"` // used in cents
	ErrorMsg    string `json:"error_msg,omitempty"`
}

// VerifyToken verifies a LightBridge Connect token
func (h *LightBridgeConnectHandler) VerifyToken(c *gin.Context) {
	var req VerifyTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Only support new-api for now
	if req.Type != "new-api" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported type: " + req.Type})
		return
	}

	if h.lbcService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "LightBridge Connect service not initialized"})
		return
	}

	// Verify token
	userInfo, err := h.lbcService.VerifyNewAPIToken(c.Request.Context(), req.InstanceURL, req.SystemToken)
	if err != nil {
		c.JSON(http.StatusOK, VerifyTokenResponse{
			Valid:    false,
			ErrorMsg: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, VerifyTokenResponse{
		Valid:       true,
		UserID:      userInfo.Data.ID,
		Username:    userInfo.Data.Username,
		DisplayName: userInfo.Data.DisplayName,
		Email:       userInfo.Data.Email,
		Quota:       int64(userInfo.Data.Quota - userInfo.Data.UsedQuota),
		UsedQuota:   int64(userInfo.Data.UsedQuota),
	})
}

// GetQuota manually fetches quota for an account
func (h *LightBridgeConnectHandler) GetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account ID"})
		return
	}

	var lbcConfigJSON sql.NullString
	err = h.db.QueryRow(`
		SELECT lightbridge_connect
		FROM accounts
		WHERE id = $1 AND deleted_at IS NULL
	`, accountID).Scan(&lbcConfigJSON)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if !lbcConfigJSON.Valid || lbcConfigJSON.String == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account has no LightBridge Connect configuration"})
		return
	}

	var config service.LightBridgeConnectConfig
	if err := json.Unmarshal([]byte(lbcConfigJSON.String), &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid configuration"})
		return
	}

	// Sync quota
	quotaInfo, err := h.lbcService.SyncNewAPIQuota(c.Request.Context(), &config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync quota: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"balance":      quotaInfo.Balance,
		"used":         quotaInfo.Used,
		"currency":     quotaInfo.Currency,
		"last_sync_at": quotaInfo.LastSyncAt,
	})
}

// SyncQuota manually syncs quota and updates database
func (h *LightBridgeConnectHandler) SyncQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account ID"})
		return
	}

	var lbcConfigJSON sql.NullString
	err = h.db.QueryRow(`
		SELECT lightbridge_connect
		FROM accounts
		WHERE id = $1 AND deleted_at IS NULL
	`, accountID).Scan(&lbcConfigJSON)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if !lbcConfigJSON.Valid || lbcConfigJSON.String == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account has no LightBridge Connect configuration"})
		return
	}

	var config service.LightBridgeConnectConfig
	if err := json.Unmarshal([]byte(lbcConfigJSON.String), &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid configuration"})
		return
	}

	// Get old balance
	oldBalance := int64(0)
	if config.Quota != nil {
		oldBalance = config.Quota.Balance
	}

	// Sync quota
	quotaInfo, err := h.lbcService.SyncNewAPIQuota(c.Request.Context(), &config)
	if err != nil {
		// Log failed sync
		h.db.Exec(`
			INSERT INTO lightbridge_connect_quota_logs
			(account_id, sync_type, sync_success, error_message)
			VALUES ($1, $2, $3, $4)
		`, accountID, "manual", false, err.Error())

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync quota: " + err.Error()})
		return
	}

	// Update config
	config.Quota = quotaInfo
	configJSON, _ := json.Marshal(config)

	_, err = h.db.Exec(`
		UPDATE accounts
		SET lightbridge_connect = $1, updated_at = NOW()
		WHERE id = $2
	`, configJSON, accountID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update account"})
		return
	}

	// Log successful sync
	h.db.Exec(`
		INSERT INTO lightbridge_connect_quota_logs
		(account_id, balance_before, balance_after, change_amount, sync_type, sync_success)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, accountID, oldBalance, quotaInfo.Balance, quotaInfo.Balance-oldBalance, "manual", true)

	// Check for alerts
	alert := h.lbcService.CheckQuotaAlert(&config, oldBalance, quotaInfo.Balance)
	if alert != nil {
		// Save alert
		h.db.Exec(`
			INSERT INTO lightbridge_connect_alerts
			(account_id, alert_type, severity, message, metadata)
			VALUES ($1, $2, $3, $4, $5)
		`, accountID, alert.Type, alert.Severity, alert.Message, "{}")

		// Send alert
		go h.lbcService.SendAlert(c.Request.Context(), accountID, &config, alert)

		// Auto-disable if exhausted and configured
		if alert.Type == "quota_exhausted" && config.Alert != nil && config.Alert.AutoDisableOnLow {
			h.db.Exec(`
				UPDATE accounts
				SET status = 'paused', updated_at = NOW()
				WHERE id = $1
			`, accountID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"balance":      quotaInfo.Balance,
		"used":         quotaInfo.Used,
		"currency":     quotaInfo.Currency,
		"last_sync_at": quotaInfo.LastSyncAt,
		"alert":        alert,
	})
}

// UpdateAlertConfig updates alert configuration
func (h *LightBridgeConnectHandler) UpdateAlertConfig(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account ID"})
		return
	}

	var req struct {
		Alert *service.AlertConfig `json:"alert" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var lbcConfigJSON sql.NullString
	err = h.db.QueryRow(`
		SELECT lightbridge_connect
		FROM accounts
		WHERE id = $1 AND deleted_at IS NULL
	`, accountID).Scan(&lbcConfigJSON)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	if !lbcConfigJSON.Valid || lbcConfigJSON.String == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account has no LightBridge Connect configuration"})
		return
	}

	var config service.LightBridgeConnectConfig
	if err := json.Unmarshal([]byte(lbcConfigJSON.String), &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid configuration"})
		return
	}

	// Update alert config
	config.Alert = req.Alert
	configJSON, _ := json.Marshal(config)

	_, err = h.db.Exec(`
		UPDATE accounts
		SET lightbridge_connect = $1, updated_at = NOW()
		WHERE id = $2
	`, configJSON, accountID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update configuration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
