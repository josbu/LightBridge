package service

func buildAnthropicMessagesURL(base string, countTokens bool) string {
	endpoint := "/v1/messages"
	if countTokens {
		endpoint += "/count_tokens"
	}
	return buildOpenAIEndpointURL(base, endpoint) + "?beta=true"
}
