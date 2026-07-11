package service

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeOpenAIResponsesTerminalEvent guarantees the minimum terminal event
// contract required by strict Responses clients such as Codex and OpenCode.
// It preserves all upstream-specific fields and only fills fields that are
// absent. Token counts are never fabricated: missing usage becomes explicit
// zeroes so clients can safely dereference response.usage.
func normalizeOpenAIResponsesTerminalEvent(data []byte) ([]byte, bool) {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return data, false
	}
	eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
	status := openAIResponsesStatusForTerminalEvent(eventType)
	if status == "" {
		return data, false
	}

	out := append([]byte(nil), data...)
	changed := false
	set := func(path string, value any) {
		updated, err := sjson.SetBytes(out, path, value)
		if err == nil && !bytes.Equal(updated, out) {
			out = updated
			changed = true
		}
	}
	setRaw := func(path string, raw string) {
		updated, err := sjson.SetRawBytes(out, path, []byte(raw))
		if err == nil && !bytes.Equal(updated, out) {
			out = updated
			changed = true
		}
	}

	response := gjson.GetBytes(out, "response")
	if !response.Exists() || !response.IsObject() {
		setRaw("response", `{}`)
	}

	// Some compatible gateways flatten response fields onto the event. Copy
	// them into the canonical response wrapper before applying defaults.
	for _, field := range []string{"id", "model", "status", "output", "incomplete_details", "error"} {
		if current := gjson.GetBytes(out, "response."+field); current.Exists() {
			continue
		}
		if top := gjson.GetBytes(out, field); top.Exists() {
			if top.Type == gjson.JSON {
				setRaw("response."+field, top.Raw)
			} else {
				set("response."+field, top.Value())
			}
		}
	}

	if !gjson.GetBytes(out, "response.object").Exists() {
		set("response.object", "response")
	}
	if strings.TrimSpace(gjson.GetBytes(out, "response.status").String()) == "" {
		set("response.status", status)
	}
	if output := gjson.GetBytes(out, "response.output"); !output.Exists() || !output.IsArray() {
		setRaw("response.output", `[]`)
	}

	out, usageChanged := normalizeOpenAIUsageAtPath(out, "response.usage", "usage")
	return out, changed || usageChanged
}

// normalizeOpenAIResponsesObject guarantees the minimum non-streaming response
// contract. It is intentionally used only for successful Responses payloads.
func normalizeOpenAIResponsesObject(data []byte) ([]byte, bool) {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return data, false
	}
	root := gjson.ParseBytes(data)
	if !root.IsObject() {
		return data, false
	}
	object := strings.TrimSpace(root.Get("object").String())
	if object != "" && object != "response" {
		return data, false
	}
	if object == "" && !root.Get("output").Exists() && !root.Get("status").Exists() && !root.Get("id").Exists() {
		return data, false
	}

	out := append([]byte(nil), data...)
	changed := false
	if object == "" {
		if updated, err := sjson.SetBytes(out, "object", "response"); err == nil {
			out = updated
			changed = true
		}
	}
	if output := gjson.GetBytes(out, "output"); !output.Exists() || !output.IsArray() {
		if updated, err := sjson.SetRawBytes(out, "output", []byte(`[]`)); err == nil {
			out = updated
			changed = true
		}
	}
	out, usageChanged := normalizeOpenAIUsageAtPath(out, "usage", "")
	return out, changed || usageChanged
}

func normalizeOpenAIUsageAtPath(data []byte, targetPath, fallbackPath string) ([]byte, bool) {
	out := append([]byte(nil), data...)
	changed := false
	target := gjson.GetBytes(out, targetPath)
	fallback := gjson.Result{}
	if fallbackPath != "" {
		fallback = gjson.GetBytes(out, fallbackPath)
	}

	usageSource := target
	if !usageSource.Exists() || !usageSource.IsObject() {
		usageSource = fallback
	}

	input := usageNumber(usageSource, "input_tokens", "prompt_tokens")
	output := usageNumber(usageSource, "output_tokens", "completion_tokens")
	total := usageNumber(usageSource, "total_tokens")
	if total == 0 && (input != 0 || output != 0) {
		total = input + output
	}
	cached := usageNumber(usageSource, "input_tokens_details.cached_tokens", "prompt_tokens_details.cached_tokens")
	reasoning := usageNumber(usageSource, "output_tokens_details.reasoning_tokens", "completion_tokens_details.reasoning_tokens")

	if !target.Exists() || !target.IsObject() {
		if updated, err := sjson.SetRawBytes(out, targetPath, []byte(`{}`)); err == nil {
			out = updated
			changed = true
		}
	}
	for path, value := range map[string]int64{
		targetPath + ".input_tokens":                           input,
		targetPath + ".output_tokens":                          output,
		targetPath + ".total_tokens":                           total,
		targetPath + ".input_tokens_details.cached_tokens":     cached,
		targetPath + ".output_tokens_details.reasoning_tokens": reasoning,
	} {
		if !gjson.GetBytes(out, path).Exists() {
			if updated, err := sjson.SetBytes(out, path, value); err == nil {
				out = updated
				changed = true
			}
		}
	}
	return out, changed
}

func usageNumber(usage gjson.Result, paths ...string) int64 {
	if !usage.Exists() || !usage.IsObject() {
		return 0
	}
	for _, path := range paths {
		value := usage.Get(path)
		if value.Exists() {
			return value.Int()
		}
	}
	return 0
}

func openAIResponsesStatusForTerminalEvent(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done":
		return "completed"
	case "response.incomplete", "response.cancelled", "response.canceled":
		return "incomplete"
	case "response.failed":
		return "failed"
	default:
		return ""
	}
}
