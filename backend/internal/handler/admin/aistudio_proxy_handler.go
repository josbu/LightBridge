package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/WilliamWang1721/LightBridge/internal/service/aistudio_proxy"
	"github.com/gin-gonic/gin"
)

// AistudioProxyHandler exposes admin endpoints for the LB-managed aistudio-api
// reverse-proxy feature: binding a Google account via cookie import, and (M3)
// guided browser login. It owns no state beyond references to the manager and
// the account service.
type AistudioProxyHandler struct {
	manager        *aistudio_proxy.Manager
	accountService *service.AccountService
	httpClient     *http.Client
}

// NewAistudioProxyHandler constructs the handler. accountService may be nil in
// tests; manager may be nil (endpoints return 503 with a clear message).
func NewAistudioProxyHandler(manager *aistudio_proxy.Manager, accountService *service.AccountService) *AistudioProxyHandler {
	return &AistudioProxyHandler{
		manager:        manager,
		accountService: accountService,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// SetManager allows late injection (used when the manager is created after the
// handler during DI bootstrap).
func (h *AistudioProxyHandler) SetManager(m *aistudio_proxy.Manager) {
	if h != nil {
		h.manager = m
	}
}

type aistudioImportCookiesRequest struct {
	Cookies string `json:"cookies" binding:"required"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}

// ImportCookies binds a Google account to an AIStudio reverse-proxy account by
// forwarding a cookie string to that account's aistudio-api subprocess.
//
// POST /api/v1/admin/aistudio-proxy/accounts/:id/import-cookies
func (h *AistudioProxyHandler) ImportCookies(c *gin.Context) {
	if h == nil || h.manager == nil {
		response.InternalError(c, "aistudio reverse-proxy runtime is not available on this server")
		return
	}

	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}

	var req aistudioImportCookiesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Cookies) == "" {
		response.BadRequest(c, "cookies are required")
		return
	}

	// Verify the target account is a Gemini Bearer (reverse-proxy) account.
	if h.accountService != nil {
		acc, err := h.accountService.GetByID(c.Request.Context(), accountID)
		if err != nil {
			response.BadRequest(c, "account not found: "+err.Error())
			return
		}
		if !acc.UsesBearerAuth() {
			response.BadRequest(c, "account is not an AIStudio reverse-proxy account")
			return
		}
	}

	// Ensure the subprocess is up; this also materializes the bearer token.
	inst, err := h.manager.EnsureRunning(c.Request.Context(), accountID)
	if err != nil {
		response.InternalError(c, "failed to start aistudio-api subprocess: "+err.Error())
		return
	}

	// Forward to the subprocess's /accounts/import-cookies endpoint.
	payload, _ := json.Marshal(map[string]any{
		"cookies": req.Cookies,
		"name":    req.Name,
		"email":   req.Email,
	})
	fwdReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, inst.BaseURL+"/accounts/import-cookies", bytes.NewReader(payload))
	if err != nil {
		response.InternalError(c, "failed to build request: "+err.Error())
		return
	}
	fwdReq.Header.Set("Content-Type", "application/json")
	fwdReq.Header.Set("Authorization", "Bearer "+inst.BearerToken)

	resp, err := h.httpClient.Do(fwdReq)
	if err != nil {
		response.InternalError(c, "aistudio-api import-cookies call failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		response.Error(c, resp.StatusCode, fmt.Sprintf("aistudio-api rejected cookies (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	// Surface the subprocess's JSON response (account_id, cookie_count, ...) as the data field.
	var data any
	_ = json.Unmarshal(body, &data)
	response.Success(c, data)
}

// Status reports the manager's view of all running aistudio-api instances.
//
// GET /api/v1/admin/aistudio-proxy/status
func (h *AistudioProxyHandler) Status(c *gin.Context) {
	if h == nil || h.manager == nil {
		response.Success(c, gin.H{"enabled": false, "instances": []any{}})
		return
	}
	response.Success(c, gin.H{"enabled": true, "instances": h.manager.Snapshot()})
}

// Stop stops a specific account's subprocess.
//
// POST /api/v1/admin/aistudio-proxy/accounts/:id/stop
func (h *AistudioProxyHandler) Stop(c *gin.Context) {
	if h == nil || h.manager == nil {
		response.InternalError(c, "aistudio reverse-proxy runtime is not available on this server")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	if err := h.manager.Stop(context.Background(), accountID); err != nil {
		response.InternalError(c, "failed to stop subprocess: "+err.Error())
		return
	}
	response.Success(c, gin.H{"stopped": accountID})
}

// RuntimeStatus reports whether the aistudio-api python runtime + browser are
// installed and ready on this host.
//
// GET /api/v1/admin/aistudio-proxy/runtime-status
func (h *AistudioProxyHandler) RuntimeStatus(c *gin.Context) {
	if h == nil || h.manager == nil || h.manager.Installer() == nil {
		response.Success(c, gin.H{"ready": false, "reason": "runtime module unavailable"})
		return
	}
	status, err := h.manager.Installer().Detect(c.Request.Context())
	if err != nil {
		response.InternalError(c, "detect failed: "+err.Error())
		return
	}
	response.Success(c, status)
}

// RuntimeInstall triggers a best-effort pip install + browser fetch, streaming
// progress lines to the client as SSE.
//
// POST /api/v1/admin/aistudio-proxy/runtime-install
func (h *AistudioProxyHandler) RuntimeInstall(c *gin.Context) {
	if h == nil || h.manager == nil || h.manager.Installer() == nil {
		response.InternalError(c, "runtime module unavailable")
		return
	}
	installer := h.manager.Installer()

	// Stream progress as SSE.
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	logFn := func(line string) {
		// one JSON object per SSE event; never write partial lines.
		data, _ := json.Marshal(map[string]string{"log": line})
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	res, err := installer.Install(c.Request.Context(), logFn)
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", errData)
		c.Writer.Flush()
		return
	}
	doneData, _ := json.Marshal(map[string]any{"done": true, "result": res})
	fmt.Fprintf(c.Writer, "data: %s\n\n", doneData)
	c.Writer.Flush()
}

// --- M3: guided Google login ---

type aistudioStartLoginRequest struct {
	Name string `json:"name"`
}

// StartLogin kicks off a headed-browser Google login in the account's
// aistudio-api subprocess. The user completes login in the browser window; the
// session is captured automatically. Poll status via LoginStatus.
//
// POST /api/v1/admin/aistudio-proxy/accounts/:id/login
func (h *AistudioProxyHandler) StartLogin(c *gin.Context) {
	if h == nil || h.manager == nil {
		response.InternalError(c, "aistudio reverse-proxy runtime is not available on this server")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	var req aistudioStartLoginRequest
	_ = c.ShouldBindJSON(&req)

	inst, err := h.manager.EnsureRunning(c.Request.Context(), accountID)
	if err != nil {
		response.InternalError(c, "failed to start subprocess: "+err.Error())
		return
	}

	payload, _ := json.Marshal(map[string]any{"name": req.Name})
	fwdReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, inst.BaseURL+"/accounts/login/start", bytes.NewReader(payload))
	if err != nil {
		response.InternalError(c, "build request failed: "+err.Error())
		return
	}
	fwdReq.Header.Set("Content-Type", "application/json")
	fwdReq.Header.Set("Authorization", "Bearer "+inst.BearerToken)

	resp, err := h.httpClient.Do(fwdReq)
	if err != nil {
		response.InternalError(c, "login start failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		response.Error(c, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	var data any
	_ = json.Unmarshal(body, &data)
	response.Success(c, data)
}

// LoginStatus polls the guided-login session status for an account.
//
// GET /api/v1/admin/aistudio-proxy/accounts/:id/login/status?session=...
func (h *AistudioProxyHandler) LoginStatus(c *gin.Context) {
	if h == nil || h.manager == nil {
		response.InternalError(c, "aistudio reverse-proxy runtime is not available on this server")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return
	}
	sessionID := strings.TrimSpace(c.Query("session"))
	if sessionID == "" {
		response.BadRequest(c, "session query param is required")
		return
	}
	inst, err := h.manager.EnsureRunning(c.Request.Context(), accountID)
	if err != nil {
		response.InternalError(c, "failed to start subprocess: "+err.Error())
		return
	}
	fwdReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, inst.BaseURL+"/accounts/login/status/"+sessionID, nil)
	if err != nil {
		response.InternalError(c, "build request failed: "+err.Error())
		return
	}
	fwdReq.Header.Set("Authorization", "Bearer "+inst.BearerToken)
	resp, err := h.httpClient.Do(fwdReq)
	if err != nil {
		response.InternalError(c, "login status failed: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		response.Error(c, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	var data any
	_ = json.Unmarshal(body, &data)
	response.Success(c, data)
}
