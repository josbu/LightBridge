package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildGeminiModelActionURLNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   string
		stream bool
		want   string
	}{
		{"root", "https://example.com", false, "https://example.com/v1beta/models/gemini-2.5-pro:generateContent"},
		{"v1", "https://example.com/v1", false, "https://example.com/v1beta/models/gemini-2.5-pro:generateContent"},
		{"v1beta", "https://example.com/v1beta", true, "https://example.com/v1beta/models/gemini-2.5-pro:generateContent?alt=sse"},
		{"full endpoint", "https://example.com/api/v1beta/models/old:generateContent", false, "https://example.com/api/v1beta/models/gemini-2.5-pro:generateContent"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, buildGeminiModelActionURL(tt.base, "models/gemini-2.5-pro", "generateContent", tt.stream))
		})
	}
}
