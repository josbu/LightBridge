package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAnthropicMessagesURLNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://example.com/v1/messages?beta=true", buildAnthropicMessagesURL("https://example.com", false))
	require.Equal(t, "https://example.com/v1/messages?beta=true", buildAnthropicMessagesURL("https://example.com/v1", false))
	require.Equal(t, "https://example.com/v1/messages?beta=true", buildAnthropicMessagesURL("https://example.com/v1/messages", false))
	require.Equal(t, "https://example.com/v1/messages/count_tokens?beta=true", buildAnthropicMessagesURL("https://example.com/v1", true))
}
