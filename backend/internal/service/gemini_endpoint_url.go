package service

import (
	"fmt"
	"strings"
)

// buildGeminiModelActionURL normalizes root, /v1, /v1beta, /models and full
// Gemini endpoint inputs before appending the requested model action.
func buildGeminiModelActionURL(base, model, action string, stream bool) string {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	target := fmt.Sprintf("%s/%s:%s", strings.TrimRight(buildGeminiModelsURL(base), "/"), model, strings.TrimSpace(action))
	if stream {
		target += "?alt=sse"
	}
	return target
}
