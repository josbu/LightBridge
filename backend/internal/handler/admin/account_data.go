package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"log/slog"

	infraerrors "github.com/WilliamWang1721/LightBridge/internal/pkg/errors"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/openai"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/response"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	dataType                  = "LightBridge-data"
	legacyDataType            = "LightBridge-bundle"
	authconvDataTypeAlias     = "lightbridge"
	dataVersion               = 1
	dataPageCap               = 1000
	dataImportRemoteMaxBytes  = 20 << 20
	dataImportHTTPTimeout     = 30 * time.Second
	dataImportZipMaxFiles     = 200
	dataImportZipMaxFileBytes = 10 << 20
)

type DataPayload struct {
	Type       string        `json:"type,omitempty"`
	Version    int           `json:"version,omitempty"`
	ExportedAt string        `json:"exported_at"`
	Proxies    []DataProxy   `json:"proxies"`
	Accounts   []DataAccount `json:"accounts"`
}

type DataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Status   string `json:"status"`
}

// DataAccount 是管理员显式备份导出使用的账号结构，故意不走 dto.Account 的脱敏路径，
// Credentials 原文返回。这是"管理员备份"这一显式行为的一部分；如未来需要导出脱敏版本，
// 应新增独立结构而非修改这里。
type DataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes,omitempty"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra,omitempty"`
	ProxyKey           *string        `json:"proxy_key,omitempty"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier,omitempty"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired,omitempty"`
}

type DataImportRequest struct {
	Data                 json.RawMessage      `json:"data"`
	SourceURL            string               `json:"source_url"`
	SkipDefaultGroupBind *bool                `json:"skip_default_group_bind"`
	CompatibilityMode    *bool                `json:"compatibility_mode"`
	GroupIDs             []int64              `json:"group_ids"`
	AccountDefaults      *DataAccountDefaults `json:"account_defaults"`
}

type DataAccountDefaults struct {
	Concurrency        *int     `json:"concurrency"`
	Priority           *int     `json:"priority"`
	RateMultiplier     *float64 `json:"rate_multiplier"`
	AutoPauseOnExpired *bool    `json:"auto_pause_on_expired"`
}

type DataImportResult struct {
	ProxyCreated   int               `json:"proxy_created"`
	ProxyReused    int               `json:"proxy_reused"`
	ProxyFailed    int               `json:"proxy_failed"`
	AccountCreated int               `json:"account_created"`
	AccountFailed  int               `json:"account_failed"`
	Errors         []DataImportError `json:"errors,omitempty"`
}

type DataImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

func buildProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func (h *AccountHandler) ExportData(c *gin.Context) {
	ctx := c.Request.Context()

	selectedIDs, err := parseAccountIDs(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	accounts, err := h.resolveExportAccounts(ctx, selectedIDs, c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	includeProxies, err := parseIncludeProxies(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var proxies []service.Proxy
	if includeProxies {
		proxies, err = h.resolveExportProxies(ctx, accounts)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	} else {
		proxies = []service.Proxy{}
	}

	proxyKeyByID := make(map[int64]string, len(proxies))
	dataProxies := make([]DataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyByID[p.ID] = key
		dataProxies = append(dataProxies, DataProxy{
			ProxyKey: key,
			Name:     p.Name,
			Protocol: p.Protocol,
			Host:     p.Host,
			Port:     p.Port,
			Username: p.Username,
			Password: p.Password,
			Status:   p.Status,
		})
	}

	dataAccounts := make([]DataAccount, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		var proxyKey *string
		if acc.ProxyID != nil {
			if key, ok := proxyKeyByID[*acc.ProxyID]; ok {
				proxyKey = &key
			}
		}
		var expiresAt *int64
		if acc.ExpiresAt != nil {
			v := acc.ExpiresAt.Unix()
			expiresAt = &v
		}
		dataAccounts = append(dataAccounts, DataAccount{
			Name:               acc.Name,
			Notes:              acc.Notes,
			Platform:           acc.EffectivePlatform(),
			Type:               acc.Type,
			Credentials:        acc.Credentials,
			Extra:              acc.Extra,
			ProxyKey:           proxyKey,
			Concurrency:        acc.Concurrency,
			Priority:           acc.Priority,
			RateMultiplier:     acc.RateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &acc.AutoPauseOnExpired,
		})
	}

	payload := DataPayload{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    dataProxies,
		Accounts:   dataAccounts,
	}

	response.Success(c, payload)
}

func (h *AccountHandler) ImportData(c *gin.Context) {
	var req DataImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := validateDataImportOptions(req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	sourceURL := strings.TrimSpace(req.SourceURL)
	useCompatibilityMode := req.CompatibilityMode != nil && *req.CompatibilityMode

	executeAdminIdempotentJSON(c, "admin.accounts.import_data", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		dataPayloads, err := h.resolveImportDataPayloads(ctx, req.Data, sourceURL, useCompatibilityMode)
		if err != nil {
			return nil, err
		}
		return h.importDataPayloads(ctx, dataPayloads, req.SkipDefaultGroupBind, req.GroupIDs, req.AccountDefaults)
	})
}

func (h *AccountHandler) resolveImportDataPayloads(ctx context.Context, raw json.RawMessage, sourceURL string, compatibilityMode bool) ([]DataPayload, error) {
	if sourceURL != "" {
		downloaded, filename, err := downloadImportSourceURL(ctx, sourceURL)
		if err != nil {
			return nil, infraerrors.ServiceUnavailable("DATA_IMPORT_SOURCE_DOWNLOAD_FAILED", fmt.Sprintf("download import source failed: %s", err.Error())).WithCause(err)
		}
		return normalizeImportFileData(downloaded, filename, compatibilityMode)
	}
	payload, err := normalizeSingleImportDataPayload(raw, compatibilityMode)
	if err != nil {
		return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID", err.Error()).WithCause(err)
	}
	return []DataPayload{payload}, nil
}

func normalizeSingleImportDataPayload(raw json.RawMessage, compatibilityMode bool) (DataPayload, error) {
	dataPayload, err := normalizeImportDataPayload(raw)
	if err != nil && compatibilityMode {
		dataPayload, err = normalizeCompatibilityImportPayload(raw)
	}
	return dataPayload, err
}

func (h *AccountHandler) importDataPayloads(ctx context.Context, dataPayloads []DataPayload, skipDefaultGroupBindRaw *bool, groupIDs []int64, accountDefaults *DataAccountDefaults) (DataImportResult, error) {
	if len(dataPayloads) == 0 {
		return DataImportResult{}, errors.New("data is required")
	}
	merged := DataImportResult{}
	for _, dataPayload := range dataPayloads {
		result, err := h.importData(ctx, dataPayload, skipDefaultGroupBindRaw, groupIDs, accountDefaults)
		if err != nil {
			return merged, err
		}
		mergeDataImportResult(&merged, result)
	}
	return merged, nil
}

func mergeDataImportResult(target *DataImportResult, source DataImportResult) {
	target.ProxyCreated += source.ProxyCreated
	target.ProxyReused += source.ProxyReused
	target.ProxyFailed += source.ProxyFailed
	target.AccountCreated += source.AccountCreated
	target.AccountFailed += source.AccountFailed
	target.Errors = append(target.Errors, source.Errors...)
}

func downloadImportSourceURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", errors.New("source_url must be a valid http or https URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", errors.New("source_url is invalid")
	}
	req.Header.Set("Accept", "application/json,text/plain,application/zip,application/octet-stream,*/*")

	client := &http.Client{Timeout: dataImportHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download source_url failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download source_url returned %s", resp.Status)
	}
	if resp.ContentLength > dataImportRemoteMaxBytes {
		return nil, "", fmt.Errorf("download source_url exceeds %d bytes", dataImportRemoteMaxBytes)
	}

	limited := io.LimitReader(resp.Body, dataImportRemoteMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("read source_url response failed: %w", err)
	}
	if len(data) > dataImportRemoteMaxBytes {
		return nil, "", fmt.Errorf("download source_url exceeds %d bytes", dataImportRemoteMaxBytes)
	}

	filename := path.Base(parsed.Path)
	if filename == "." || filename == "/" {
		filename = ""
	}
	return data, filename, nil
}

func normalizeImportFileData(data []byte, filename string, compatibilityMode bool) ([]DataPayload, error) {
	if len(data) == 0 {
		err := errors.New("downloaded import file is empty")
		return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID", err.Error()).WithCause(err)
	}
	if isZipImportFile(data, filename) {
		return normalizeImportZipData(data, compatibilityMode)
	}
	payload, err := normalizeSingleImportDataPayload(json.RawMessage(data), compatibilityMode)
	if err != nil {
		return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID", err.Error()).WithCause(err)
	}
	return []DataPayload{payload}, nil
}

func normalizeImportZipData(data []byte, compatibilityMode bool) ([]DataPayload, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", "Invalid ZIP file").WithCause(err)
	}

	payloads := make([]DataPayload, 0)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !isImportableDataFilename(file.Name) {
			continue
		}
		if len(payloads) >= dataImportZipMaxFiles {
			err := fmt.Errorf("ZIP contains more than %d importable files", dataImportZipMaxFiles)
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", err.Error()).WithCause(err)
		}
		if file.UncompressedSize64 > dataImportZipMaxFileBytes {
			err := fmt.Errorf("ZIP entry %s exceeds %d bytes", file.Name, dataImportZipMaxFileBytes)
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", err.Error()).WithCause(err)
		}

		rc, err := file.Open()
		if err != nil {
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", fmt.Sprintf("open ZIP entry %s failed", file.Name)).WithCause(err)
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, dataImportZipMaxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil {
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", fmt.Sprintf("read ZIP entry %s failed", file.Name)).WithCause(readErr)
		}
		if closeErr != nil {
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", fmt.Sprintf("close ZIP entry %s failed", file.Name)).WithCause(closeErr)
		}
		if len(content) > dataImportZipMaxFileBytes {
			err := fmt.Errorf("ZIP entry %s exceeds %d bytes", file.Name, dataImportZipMaxFileBytes)
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", err.Error()).WithCause(err)
		}

		payload, err := normalizeSingleImportDataPayload(json.RawMessage(content), compatibilityMode)
		if err != nil {
			return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", fmt.Sprintf("ZIP entry %s: %s", file.Name, err.Error())).WithCause(err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		err := errors.New("ZIP contains no importable JSON/TXT files")
		return nil, infraerrors.BadRequest("DATA_IMPORT_INVALID_ZIP", err.Error()).WithCause(err)
	}
	return payloads, nil
}

func isZipImportFile(data []byte, filename string) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".zip") {
		return true
	}
	return len(data) >= 4 && bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04})
}

func isImportableDataFilename(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(normalized, ".json") || strings.HasSuffix(normalized, ".txt")
}

func (h *AccountHandler) importData(ctx context.Context, dataPayload DataPayload, skipDefaultGroupBindRaw *bool, groupIDs []int64, accountDefaults *DataAccountDefaults) (DataImportResult, error) {
	skipDefaultGroupBind := true
	if skipDefaultGroupBindRaw != nil {
		skipDefaultGroupBind = *skipDefaultGroupBindRaw
	}

	result := DataImportResult{}

	existingProxies, err := h.listAllProxies(ctx)
	if err != nil {
		return result, err
	}

	proxyKeyToID := make(map[string]int64, len(existingProxies))
	for i := range existingProxies {
		p := existingProxies[i]
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyToID[key] = p.ID
	}

	for i := range dataPayload.Proxies {
		item := dataPayload.Proxies[i]
		key := item.ProxyKey
		if key == "" {
			key = buildProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password)
		}
		if err := validateDataProxy(item); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  err.Error(),
			})
			continue
		}
		normalizedStatus := normalizeProxyStatus(item.Status)
		if existingID, ok := proxyKeyToID[key]; ok {
			proxyKeyToID[key] = existingID
			result.ProxyReused++
			if normalizedStatus != "" {
				if proxy, getErr := h.adminService.GetProxy(ctx, existingID); getErr == nil && proxy != nil && proxy.Status != normalizedStatus {
					_, _ = h.adminService.UpdateProxy(ctx, existingID, &service.UpdateProxyInput{
						Status: normalizedStatus,
					})
				}
			}
			continue
		}

		created, createErr := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:     defaultProxyName(item.Name),
			Protocol: item.Protocol,
			Host:     item.Host,
			Port:     item.Port,
			Username: item.Username,
			Password: item.Password,
		})
		if createErr != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  createErr.Error(),
			})
			continue
		}
		proxyKeyToID[key] = created.ID
		result.ProxyCreated++

		if normalizedStatus != "" && normalizedStatus != created.Status {
			_, _ = h.adminService.UpdateProxy(ctx, created.ID, &service.UpdateProxyInput{
				Status: normalizedStatus,
			})
		}
	}

	// 收集需要异步设置隐私的 Antigravity OAuth 账号
	var privacyAccounts []*service.Account

	for i := range dataPayload.Accounts {
		item := dataPayload.Accounts[i]
		applyDataAccountDefaults(&item, accountDefaults)
		if err := validateDataAccount(item); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}

		var proxyID *int64
		if item.ProxyKey != nil && *item.ProxyKey != "" {
			if id, ok := proxyKeyToID[*item.ProxyKey]; ok {
				proxyID = &id
			} else {
				result.AccountFailed++
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "account",
					Name:     item.Name,
					ProxyKey: *item.ProxyKey,
					Message:  "proxy_key not found",
				})
				continue
			}
		}

		enrichCredentialsFromIDToken(&item)

		accountInput := &service.CreateAccountInput{
			Name:                 item.Name,
			Notes:                item.Notes,
			Platform:             item.Platform,
			Type:                 item.Type,
			Credentials:          item.Credentials,
			Extra:                item.Extra,
			ProxyID:              proxyID,
			Concurrency:          item.Concurrency,
			Priority:             item.Priority,
			RateMultiplier:       item.RateMultiplier,
			GroupIDs:             groupIDs,
			ExpiresAt:            item.ExpiresAt,
			AutoPauseOnExpired:   item.AutoPauseOnExpired,
			SkipDefaultGroupBind: skipDefaultGroupBind,
		}

		created, err := h.adminService.CreateAccount(ctx, accountInput)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}
		// 收集 Antigravity OAuth 账号，稍后异步设置隐私
		if created.IsAntigravity() && created.Type == service.AccountTypeOAuth {
			privacyAccounts = append(privacyAccounts, created)
		}
		result.AccountCreated++
	}

	// 异步设置 Antigravity 隐私，避免大量导入时阻塞请求
	if len(privacyAccounts) > 0 {
		adminSvc := h.adminService
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("import_antigravity_privacy_panic", "recover", r)
				}
			}()
			bgCtx := context.Background()
			for _, acc := range privacyAccounts {
				adminSvc.ForceAntigravityPrivacy(bgCtx, acc)
			}
			slog.Info("import_antigravity_privacy_done", "count", len(privacyAccounts))
		}()
	}

	return result, nil
}

func (h *AccountHandler) listAllProxies(ctx context.Context) ([]service.Proxy, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Proxy
	for {
		items, total, err := h.adminService.ListProxies(ctx, page, pageSize, "", "", "", "created_at", "desc")
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) listAccountsFiltered(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Account
	for {
		items, total, err := h.adminService.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) resolveExportAccounts(ctx context.Context, ids []int64, c *gin.Context) ([]service.Account, error) {
	if len(ids) > 0 {
		accounts, err := h.adminService.GetAccountsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make([]service.Account, 0, len(accounts))
		for _, acc := range accounts {
			if acc == nil {
				continue
			}
			out = append(out, *acc)
		}
		return out, nil
	}

	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	search := strings.TrimSpace(c.Query("search"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if len(search) > 100 {
		search = search[:100]
	}

	groupID := int64(0)
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil || parsedGroupID <= 0 {
				return nil, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter")
			}
			groupID = parsedGroupID
		}
	}

	return h.listAccountsFiltered(ctx, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func (h *AccountHandler) resolveExportProxies(ctx context.Context, accounts []service.Account) ([]service.Proxy, error) {
	if len(accounts) == 0 {
		return []service.Proxy{}, nil
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range accounts {
		if accounts[i].ProxyID == nil {
			continue
		}
		id := *accounts[i].ProxyID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []service.Proxy{}, nil
	}

	return h.adminService.GetProxiesByIDs(ctx, ids)
}

func parseAccountIDs(c *gin.Context) ([]int64, error) {
	values := c.QueryArray("ids")
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query("ids"))
		if raw != "" {
			values = []string{raw}
		}
	}
	if len(values) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(values))
	for _, item := range values {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid account id: %s", part)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseIncludeProxies(c *gin.Context) (bool, error) {
	raw := strings.TrimSpace(strings.ToLower(c.Query("include_proxies")))
	if raw == "" {
		return true, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return true, fmt.Errorf("invalid include_proxies value: %s", raw)
	}
}

func validateDataHeader(payload DataPayload) error {
	if payload.Type != "" && payload.Type != dataType && payload.Type != legacyDataType && strings.ToLower(payload.Type) != authconvDataTypeAlias {
		return fmt.Errorf("unsupported data type: %s", payload.Type)
	}
	if payload.Version != 0 && payload.Version != dataVersion {
		return fmt.Errorf("unsupported data version: %d", payload.Version)
	}
	if payload.Proxies == nil {
		payload.Proxies = []DataProxy{}
	}
	if payload.Accounts == nil {
		return errors.New("accounts is required")
	}
	return nil
}

func validateDataImportOptions(req DataImportRequest) error {
	for _, id := range req.GroupIDs {
		if id <= 0 {
			return fmt.Errorf("group_id is invalid: %d", id)
		}
	}
	if req.AccountDefaults == nil {
		return nil
	}
	if req.AccountDefaults.Concurrency != nil && *req.AccountDefaults.Concurrency < 0 {
		return errors.New("default concurrency must be >= 0")
	}
	if req.AccountDefaults.Priority != nil && *req.AccountDefaults.Priority < 0 {
		return errors.New("default priority must be >= 0")
	}
	if req.AccountDefaults.RateMultiplier != nil && *req.AccountDefaults.RateMultiplier < 0 {
		return errors.New("default rate_multiplier must be >= 0")
	}
	return nil
}

func validateDataProxy(item DataProxy) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return errors.New("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.New("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	if item.Status != "" {
		normalizedStatus := normalizeProxyStatus(item.Status)
		if normalizedStatus != service.StatusActive && normalizedStatus != "inactive" {
			return fmt.Errorf("proxy status is invalid: %s", item.Status)
		}
	}
	return nil
}

func validateDataAccount(item DataAccount) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("account name is required")
	}
	if strings.TrimSpace(item.Platform) == "" {
		return errors.New("account platform is required")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("account type is required")
	}
	if len(item.Credentials) == 0 {
		return errors.New("account credentials is required")
	}
	switch item.Type {
	case service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey, service.AccountTypeUpstream:
	default:
		return fmt.Errorf("account type is invalid: %s", item.Type)
	}
	if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
		return errors.New("rate_multiplier must be >= 0")
	}
	if item.Concurrency < 0 {
		return errors.New("concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	return nil
}

func applyDataAccountDefaults(item *DataAccount, defaults *DataAccountDefaults) {
	if item == nil || defaults == nil {
		return
	}
	if defaults.Concurrency != nil {
		item.Concurrency = *defaults.Concurrency
	}
	if defaults.Priority != nil {
		item.Priority = *defaults.Priority
	}
	if defaults.RateMultiplier != nil {
		item.RateMultiplier = defaults.RateMultiplier
	}
	if defaults.AutoPauseOnExpired != nil {
		item.AutoPauseOnExpired = defaults.AutoPauseOnExpired
	}
}

func defaultProxyName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "imported-proxy"
	}
	return name
}

// enrichCredentialsFromIDToken performs best-effort extraction of user info fields
// (email, plan_type, chatgpt_account_id, etc.) from id_token in credentials.
// Only applies to OpenAI OAuth accounts. Skips expired token errors silently.
// Existing credential values are never overwritten — only missing fields are filled.
func enrichCredentialsFromIDToken(item *DataAccount) {
	if item.Credentials == nil {
		return
	}
	// Only enrich OpenAI OAuth accounts
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform != service.PlatformOpenAI {
		return
	}
	if strings.ToLower(strings.TrimSpace(item.Type)) != service.AccountTypeOAuth {
		return
	}

	idToken, _ := item.Credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}

	// DecodeIDToken skips expiry validation — safe for imported data
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		slog.Debug("import_enrich_id_token_decode_failed", "account", item.Name, "error", err)
		return
	}

	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}

	// Fill missing fields only (never overwrite existing values)
	setIfMissing := func(key, value string) {
		if value == "" {
			return
		}
		if existing, _ := item.Credentials[key].(string); existing == "" {
			item.Credentials[key] = value
		}
	}

	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
}

func normalizeProxyStatus(status string) string {
	normalized := strings.TrimSpace(strings.ToLower(status))
	switch normalized {
	case "":
		return ""
	case service.StatusActive:
		return service.StatusActive
	case "inactive", service.StatusDisabled:
		return "inactive"
	default:
		return normalized
	}
}
