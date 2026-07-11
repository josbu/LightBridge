//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/config"
	"github.com/stretchr/testify/require"
)

func newEmbedTokenAuthService() *AuthService {
	cfg := &config.Config{}
	cfg.JWT.Secret = "test-payment-embed-jwt-secret-32bytes"
	cfg.JWT.AccessTokenExpireMinutes = 60
	return NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestGeneratePaymentEmbedTokenCreatesScopedAudienceBoundToken(t *testing.T) {
	svc := newEmbedTokenAuthService()
	user := &User{
		ID:           42,
		Email:        "buyer@example.com",
		Role:         RoleUser,
		TokenVersion: 3,
	}

	token, audience, expiresIn, err := svc.GeneratePaymentEmbedToken(user, "HTTPS://Pay.Example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://pay.example.com", audience)
	require.Equal(t, int(paymentEmbedTokenTTL/time.Second), expiresIn)

	claims, err := svc.ValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, user.TokenVersion, claims.TokenVersion)
	require.Equal(t, JWTTokenScopePaymentEmbed, claims.Scope)
	require.Equal(t, []string{audience}, []string(claims.Audience))
	require.NotEmpty(t, claims.ID)
	require.Equal(t, "42", claims.Subject)
	require.NotNil(t, claims.ExpiresAt)
	require.WithinDuration(t, time.Now().Add(paymentEmbedTokenTTL), claims.ExpiresAt.Time, 3*time.Second)
}

func TestGeneratePaymentEmbedTokenValidatesAudienceOrigin(t *testing.T) {
	svc := newEmbedTokenAuthService()
	user := &User{ID: 1, Email: "buyer@example.com", Role: RoleUser}

	tests := []struct {
		name     string
		audience string
		want     string
		wantErr  bool
	}{
		{name: "https", audience: "https://pay.example.com", want: "https://pay.example.com"},
		{name: "https port", audience: "https://pay.example.com:8443/", want: "https://pay.example.com:8443"},
		{name: "localhost http", audience: "http://localhost:5173", want: "http://localhost:5173"},
		{name: "loopback http", audience: "http://127.0.0.1:5173", want: "http://127.0.0.1:5173"},
		{name: "remote http", audience: "http://pay.example.com", wantErr: true},
		{name: "path", audience: "https://pay.example.com/checkout", wantErr: true},
		{name: "query", audience: "https://pay.example.com?x=1", wantErr: true},
		{name: "fragment", audience: "https://pay.example.com/#x", wantErr: true},
		{name: "credentials", audience: "https://user:pass@pay.example.com", wantErr: true},
		{name: "relative", audience: "/checkout", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, _, err := svc.GeneratePaymentEmbedToken(user, tt.audience)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestScopedEmbedTokenCannotBeRefreshedIntoFullAccessToken(t *testing.T) {
	svc := newEmbedTokenAuthService()
	user := &User{ID: 1, Email: "buyer@example.com", Role: RoleUser}
	token, _, _, err := svc.GeneratePaymentEmbedToken(user, "https://pay.example.com")
	require.NoError(t, err)

	_, err = svc.RefreshToken(context.Background(), token)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidToken))
}

func TestTokenGenerationRequiresConfiguredSecret(t *testing.T) {
	svc := NewAuthService(nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &User{ID: 1, Email: "buyer@example.com", Role: RoleUser}

	_, err := svc.GenerateToken(user)
	require.Error(t, err)
	_, _, _, err = svc.GeneratePaymentEmbedToken(user, "https://pay.example.com")
	require.Error(t, err)
	_, err = svc.ValidateToken("anything")
	require.Error(t, err)
}
