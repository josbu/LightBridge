package admin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dataResponse struct {
	Code int         `json:"code"`
	Data dataPayload `json:"data"`
}

type dataPayload struct {
	Type     string        `json:"type"`
	Version  int           `json:"version"`
	Proxies  []dataProxy   `json:"proxies"`
	Accounts []dataAccount `json:"accounts"`
}

type dataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type dataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
	Concurrency int            `json:"concurrency"`
	Priority    int            `json:"priority"`
}

func setupAccountDataRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	router.GET("/api/v1/admin/accounts/data", h.ExportData)
	router.POST("/api/v1/admin/accounts/data", h.ImportData)
	return router, adminSvc
}

func TestExportDataIncludesSecrets(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       12,
			Name:     "orphan",
			Protocol: "https",
			Host:     "10.0.0.1",
			Port:     443,
			Username: "o",
			Password: "p",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Extra:       map[string]any{"note": "x"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Empty(t, resp.Data.Type)
	require.Equal(t, 0, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "pass", resp.Data.Proxies[0].Password)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, "secret", resp.Data.Accounts[0].Credentials["token"])
}

func TestExportDataWithoutProxies(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 0)
	require.Len(t, resp.Data.Accounts, 1)
	require.Nil(t, resp.Data.Accounts[0].ProxyKey)
}

func TestExportDataPassesAccountFiltersAndSort(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 1, Name: "acc-1", Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?platform=openai&type=oauth&status=active&group=12&privacy_mode=blocked&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Equal(t, "openai", adminSvc.lastListAccounts.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccounts.accountType)
	require.Equal(t, "active", adminSvc.lastListAccounts.status)
	require.Equal(t, int64(12), adminSvc.lastListAccounts.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccounts.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccounts.search)
	require.Equal(t, "priority", adminSvc.lastListAccounts.sortBy)
	require.Equal(t, "desc", adminSvc.lastListAccounts.sortOrder)
}

func TestExportDataSelectedIDsOverrideFilters(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?ids=1,2&platform=openai&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 2)
	require.Equal(t, 0, adminSvc.lastListAccounts.calls)
}

func TestImportDataReusesProxyAndSkipsDefaultGroup(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "1.2.3.4",
			Port:     1080,
			Username: "u",
			Password: "p",
			Status:   service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdProxies, 0)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].SkipDefaultGroupBind)
}

func TestImportDataAcceptsCLIProxyAPICodexJSON(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":            "codex",
			"email":           "user@example.com",
			"account_id":      "acc-cpa",
			"plan_type":       "plus",
			"access_token":    "access-cpa",
			"refresh_token":   "refresh-cpa",
			"session_token":   "session-cpa",
			"expired":         "2026-07-08T00:00:00Z",
			"concurrency":     12,
			"priority":        3,
			"rate_multiplier": 1.25,
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, "user@example.com", created.Name)
	require.Equal(t, service.PlatformOpenAI, created.Platform)
	require.Equal(t, service.AccountTypeOAuth, created.Type)
	require.Equal(t, "access-cpa", created.Credentials["access_token"])
	require.Equal(t, "refresh-cpa", created.Credentials["refresh_token"])
	require.Equal(t, "session-cpa", created.Credentials["session_token"])
	require.Equal(t, "acc-cpa", created.Credentials["chatgpt_account_id"])
	require.Equal(t, "user@example.com", created.Credentials["email"])
	require.Equal(t, "plus", created.Credentials["chatgpt_plan_type"])
	require.Equal(t, true, created.Credentials["id_token_synthetic"])
	require.Equal(t, "cliproxyapi", created.Extra["import_source"])
	require.Equal(t, 12, created.Concurrency)
	require.Equal(t, 3, created.Priority)
	require.NotNil(t, created.RateMultiplier)
	require.Equal(t, 1.25, *created.RateMultiplier)
	require.True(t, created.SkipDefaultGroupBind)
}

func TestImportDataAcceptsCLIProxyAPISingleAccountJSON(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	dataPayload := map[string]any{
		"data": map[string]any{
			"account":      "acc-single",
			"accessToken":  "access-single",
			"refreshToken": "refresh-single",
			"sessionToken": "session-single",
		},
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, "acc-single", created.Name)
	require.Equal(t, "acc-single", created.Credentials["chatgpt_account_id"])
	require.Equal(t, "access-single", created.Credentials["access_token"])
	require.Equal(t, "refresh-single", created.Credentials["refresh_token"])
	require.Equal(t, "session-single", created.Credentials["session_token"])
}

func TestImportDataCompatibilityModeExtractsNestedJSON(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	dataPayload := map[string]any{
		"data": map[string]any{
			"metadata": map[string]any{
				"owner": "ignored",
			},
			"token_blob": map[string]any{
				"Refresh Token":           "refresh-nested",
				"AccessToken":             "access-nested",
				"chatgpt_account_id":      "acc-nested",
				"email":                   "nested@example.com",
				"chatgpt_plan_type":       "team",
				"expires_at":              "2026-07-08T00:00:00Z",
				"unsupported_extra_field": "ignored",
			},
		},
		"compatibility_mode": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, "nested@example.com", created.Name)
	require.Equal(t, service.PlatformOpenAI, created.Platform)
	require.Equal(t, service.AccountTypeOAuth, created.Type)
	require.Equal(t, "refresh-nested", created.Credentials["refresh_token"])
	require.Equal(t, "access-nested", created.Credentials["access_token"])
	require.Equal(t, "acc-nested", created.Credentials["chatgpt_account_id"])
	require.Equal(t, "nested@example.com", created.Credentials["email"])
	require.Equal(t, "team", created.Credentials["chatgpt_plan_type"])
	require.Equal(t, "compatibility", created.Extra["import_source"])
	require.NotNil(t, created.ExpiresAt)
}

func TestImportDataCompatibilityModeExtractsTXT(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	dataPayload := map[string]any{
		"data":               "refresh token: refresh-text\naccess_token=access-text\nemail: text@example.com\naccount id: acc-text",
		"compatibility_mode": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, "text@example.com", created.Name)
	require.Equal(t, "refresh-text", created.Credentials["refresh_token"])
	require.Equal(t, "access-text", created.Credentials["access_token"])
	require.Equal(t, "acc-text", created.Credentials["chatgpt_account_id"])
	require.Equal(t, "text@example.com", created.Credentials["email"])
}

func TestImportDataAppliesGroupIDsAndAccountDefaults(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"access_token": "x"},
					"concurrency": 1,
					"priority":    1,
				},
			},
		},
		"group_ids": []int64{10, 11},
		"account_defaults": map[string]any{
			"concurrency":           24,
			"priority":              7,
			"rate_multiplier":       0.5,
			"auto_pause_on_expired": false,
		},
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, []int64{10, 11}, created.GroupIDs)
	require.Equal(t, 24, created.Concurrency)
	require.Equal(t, 7, created.Priority)
	require.NotNil(t, created.RateMultiplier)
	require.Equal(t, 0.5, *created.RateMultiplier)
	require.NotNil(t, created.AutoPauseOnExpired)
	require.False(t, *created.AutoPauseOnExpired)
}

func TestImportDataFromSourceURLJSON(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"type":"LightBridge-data",
			"version":1,
			"proxies":[],
			"accounts":[{
				"name":"url-json",
				"platform":"openai",
				"type":"oauth",
				"credentials":{"access_token":"url-access"},
				"concurrency":10,
				"priority":1
			}]
		}`)
	}))
	defer source.Close()

	dataPayload := map[string]any{
		"source_url":              source.URL + "/accounts.json",
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	created := adminSvc.createdAccounts[0]
	require.Equal(t, "url-json", created.Name)
	require.Equal(t, "url-access", created.Credentials["access_token"])
	require.True(t, created.SkipDefaultGroupBind)
}

func TestImportDataFromSourceURLZIP(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	zipData := buildAccountImportTestZip(t, map[string]string{
		"first.json": `{
			"type":"LightBridge-data",
			"version":1,
			"proxies":[],
			"accounts":[{
				"name":"zip-json",
				"platform":"openai",
				"type":"oauth",
				"credentials":{"access_token":"zip-access"},
				"concurrency":10,
				"priority":1
			}]
		}`,
		"tokens.txt": "refresh_token=zip-refresh\nemail=zip@example.com",
		"ignored.md": "# ignored",
	})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipData)
	}))
	defer source.Close()

	dataPayload := map[string]any{
		"source_url":         source.URL + "/bundle.zip",
		"compatibility_mode": true,
		"group_ids":          []int64{10},
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 2)
	require.Equal(t, "zip-json", adminSvc.createdAccounts[0].Name)
	require.Equal(t, "zip-access", adminSvc.createdAccounts[0].Credentials["access_token"])
	require.Equal(t, []int64{10}, adminSvc.createdAccounts[0].GroupIDs)
	require.Equal(t, "zip@example.com", adminSvc.createdAccounts[1].Name)
	require.Equal(t, "zip-refresh", adminSvc.createdAccounts[1].Credentials["refresh_token"])
	require.Equal(t, []int64{10}, adminSvc.createdAccounts[1].GroupIDs)
}

func buildAccountImportTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, content)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
