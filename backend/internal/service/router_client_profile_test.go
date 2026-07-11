package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectRouterClientProfile(t *testing.T) {
	tests := []struct {
		name       string
		userAgent  string
		headers    map[string]string
		wantKind   RouterClientKind
		wantStrict bool
		version    string
	}{
		{name: "claude code", userAgent: "claude-cli/2.1.63", headers: map[string]string{"X-App": "cli"}, wantKind: RouterClientClaudeCode, version: "2.1.63"},
		{name: "codex cli", userAgent: "codex_cli_rs/0.125.0", wantKind: RouterClientCodexCLI, wantStrict: true, version: "0.125.0"},
		{name: "codex app by originator", userAgent: "Mozilla/5.0", headers: map[string]string{"Originator": "Codex Desktop"}, wantKind: RouterClientCodexApp, wantStrict: true},
		{name: "opencode", userAgent: "opencode/1.2.3", wantKind: RouterClientOpenCode, wantStrict: true, version: "1.2.3"},
		{name: "generic", userAgent: "curl/8.0", wantKind: RouterClientUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/responses", nil)
			req.Header.Set("User-Agent", tt.userAgent)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			profile := DetectRouterClientProfile(req)
			assert.Equal(t, tt.wantKind, profile.Kind)
			assert.Equal(t, tt.wantStrict, profile.StrictResponsesTerminal)
			assert.Equal(t, tt.version, profile.Version)
			if tt.wantKind == RouterClientClaudeCode {
				assert.True(t, profile.StrictAnthropicStream)
				assert.True(t, profile.StrictAnthropicUsage)
			}
		})
	}
}

func TestRouterClientProfileContext(t *testing.T) {
	profile := RouterClientProfile{Kind: RouterClientOpenCode, StrictResponsesTerminal: true}
	ctx := WithRouterClientProfile(context.Background(), profile)
	require.Equal(t, profile, RouterClientProfileFromContext(ctx))
	assert.True(t, IsStrictResponsesClient(ctx))
}
