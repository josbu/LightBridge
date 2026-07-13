package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type featureControlRepoStub struct {
	mu     sync.RWMutex
	values map[string]string
}

func (r *featureControlRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if value, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *featureControlRepoStub) GetValue(_ context.Context, key string) (string, error) {
	setting, err := r.Get(context.Background(), key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *featureControlRepoStub) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}

func (r *featureControlRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *featureControlRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *featureControlRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string, len(r.values))
	for key, value := range r.values {
		result[key] = value
	}
	return result, nil
}

func (r *featureControlRepoStub) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

func TestSettingHandlerProgressiveFeatureControlRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &featureControlRepoStub{values: map[string]string{}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
	router := gin.New()
	router.GET("/features", h.GetProgressiveFeatureControls)
	router.PUT("/features/:id", h.UpdateProgressiveFeatureControl)
	router.DELETE("/features/:id/override", h.ResetProgressiveFeatureControl)

	put := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/features/payment", bytes.NewBufferString(`{"enabled":true}`))
	putRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(put, putRequest)
	require.Equal(t, http.StatusOK, put.Code)
	var responseBody struct {
		Data service.ProgressiveFeatureControlOverview `json:"data"`
	}
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &responseBody))
	var payment *service.ProgressiveFeatureControlState
	for i := range responseBody.Data.Features {
		if responseBody.Data.Features[i].ID == service.ProgressiveFeaturePayment {
			payment = &responseBody.Data.Features[i]
		}
	}
	require.NotNil(t, payment)
	require.True(t, payment.Enabled)
	require.NotNil(t, payment.Override)
	require.True(t, *payment.Override)

	reset := httptest.NewRecorder()
	router.ServeHTTP(reset, httptest.NewRequest(http.MethodDelete, "/features/payment/override", nil))
	require.Equal(t, http.StatusOK, reset.Code)

	core := httptest.NewRecorder()
	coreRequest := httptest.NewRequest(http.MethodPut, "/features/core_gateway", bytes.NewBufferString(`{"enabled":false}`))
	coreRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(core, coreRequest)
	require.Equal(t, http.StatusBadRequest, core.Code)

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPut, "/features/payment", bytes.NewBufferString(`{}`))
	invalidRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalid, invalidRequest)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
}
