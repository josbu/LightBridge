package xai

// Portions of the Grok Build compatibility behavior in this file are adapted
// from CLIProxyAPI (MIT License), Copyright (c) Luis Pater and Router-For.ME.
// The implementation has been rewritten to fit LightBridge's account and
// OpenAI Responses gateway architecture.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	GrokTokenAuthHeader        = "X-XAI-Token-Auth"
	GrokTokenAuthValue         = "xai-grok-cli"
	GrokClientVersionHeader    = "x-grok-client-version"
	GrokClientVersionValue     = "0.2.93"
	GrokClientIdentifierHeader = "x-grok-client-identifier"
	GrokClientIdentifierValue  = "grok-pager"
	GrokBuildUserAgent         = "grok-pager/0.2.93 grok-shell/0.2.93 (linux; x86_64)"
	GrokConversationHeader     = "x-grok-conv-id"

	MaxGrokEncryptedContentLen        = 8 * 1024 * 1024
	MinGrokEncryptedContentDecodedLen = 50
	MinGrokEncryptedContentEntropy    = 0.85
	MaxGrokTools                      = 200
)

// ResolveChatBaseURL selects the HTTP chat endpoint used by a Grok account.
// OAuth accounts default to Grok Build's CLI proxy. Explicit API mode keeps the
// official api.x.ai endpoint, while an explicitly configured custom endpoint is
// always honored.
func ResolveChatBaseURL(configuredBaseURL string, usingAPI bool) (string, error) {
	configuredBaseURL = strings.TrimSpace(configuredBaseURL)
	if usingAPI {
		return ValidatedBaseURL(configuredBaseURL)
	}
	if configuredBaseURL == "" || sameBaseURL(configuredBaseURL, DefaultBaseURL) {
		return ValidatedBaseURL(DefaultCLIBaseURL)
	}
	return ValidatedBaseURL(configuredBaseURL)
}

func BuildChatResponsesURL(configuredBaseURL string, usingAPI bool) (string, error) {
	baseURL, err := ResolveChatBaseURL(configuredBaseURL, usingAPI)
	if err != nil {
		return "", fmt.Errorf("invalid Grok chat base url: %w", err)
	}
	return strings.TrimRight(baseURL, "/") + "/responses", nil
}

func IsCLIChatProxyBaseURL(baseURL string) bool {
	return sameBaseURL(baseURL, DefaultCLIBaseURL)
}

func sameBaseURL(left, right string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(left), "/"), strings.TrimRight(strings.TrimSpace(right), "/"))
}

// ApplyChatHeaders applies the headers expected by either api.x.ai or the Grok
// Build CLI proxy. CLI identity headers are deliberately never sent to custom
// endpoints or official API mode.
func ApplyChatHeaders(req *http.Request, token string, stream, usingAPI bool, resolvedBaseURL, sessionID string) {
	if req == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	if strings.TrimSpace(sessionID) != "" {
		req.Header.Set(GrokConversationHeader, strings.TrimSpace(sessionID))
	}
	if !usingAPI && IsCLIChatProxyBaseURL(resolvedBaseURL) {
		req.Header.Set(GrokTokenAuthHeader, GrokTokenAuthValue)
		req.Header.Set(GrokClientVersionHeader, GrokClientVersionValue)
		req.Header.Set(GrokClientIdentifierHeader, GrokClientIdentifierValue)
		req.Header.Set("User-Agent", GrokBuildUserAgent)
		return
	}
	req.Header.Set("User-Agent", "lightbridge-grok/1.1")
}

// PatchOfficialXAIResponsesRequest applies only the protocol-neutral rewrite
// required for the official api.x.ai Responses endpoint. Build-proxy-specific
// workarounds must not leak into official API mode.
func PatchOfficialXAIResponsesRequest(body []byte, model string) ([]byte, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid json request body")
	}
	return sjson.SetBytes(body, "model", strings.TrimSpace(model))
}

// PatchGrokBuildResponsesRequest normalizes client payloads for the Grok Build
// CLI proxy. previous_response_id is preserved here so the caller can decide
// whether to use native upstream continuation or LightBridge's stateless
// reasoning replay. The include request for encrypted reasoning is removed:
// Grok Build emits the encrypted blob in output events without accepting that
// OpenAI-specific include selector.
func PatchGrokBuildResponsesRequest(body []byte, model string) ([]byte, error) {
	out, err := PatchOfficialXAIResponsesRequest(body, model)
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"prompt_cache_retention", "safety_identifier", "stream_options"} {
		if gjson.GetBytes(out, field).Exists() {
			out, err = sjson.DeleteBytes(out, field)
			if err != nil {
				return nil, err
			}
		}
	}
	out = removeEncryptedReasoningInclude(out)
	out = normalizeTools(out)
	out = normalizeToolChoice(out)
	out = normalizeInputReasoningItems(out)
	out = sanitizeInputEncryptedContent(out)
	return out, nil
}

// PatchResponsesRequest remains as a compatibility alias for callers and tests
// that explicitly target the Grok Build proxy.
func PatchResponsesRequest(body []byte, model string) ([]byte, error) {
	return PatchGrokBuildResponsesRequest(body, model)
}

func removeEncryptedReasoningInclude(body []byte) []byte {
	include := gjson.GetBytes(body, "include")
	if !include.Exists() || !include.IsArray() {
		return body
	}
	kept := make([]string, 0, len(include.Array()))
	for _, item := range include.Array() {
		value := strings.TrimSpace(item.String())
		if value == "" || value == "reasoning.encrypted_content" {
			continue
		}
		kept = append(kept, value)
	}
	if len(kept) == 0 {
		updated, err := sjson.DeleteBytes(body, "include")
		if err == nil {
			return updated
		}
		return body
	}
	updated, err := sjson.SetBytes(body, "include", kept)
	if err != nil {
		return body
	}
	return updated
}

func normalizeTools(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if !tools.Exists() || !tools.IsArray() {
		return body
	}
	result := make([]json.RawMessage, 0, len(tools.Array()))
	changed := false
	for _, tool := range tools.Array() {
		if strings.EqualFold(strings.TrimSpace(tool.Get("type").String()), "namespace") {
			changed = true
			namespace := strings.TrimSpace(tool.Get("name").String())
			for _, nested := range tool.Get("tools").Array() {
				raw, keep, didChange := normalizeTool(nested, namespace)
				changed = changed || didChange
				if keep {
					result = append(result, raw)
				}
			}
			continue
		}
		raw, keep, didChange := normalizeTool(tool, "")
		changed = changed || didChange
		if keep {
			result = append(result, raw)
		}
	}
	if len(result) > MaxGrokTools {
		result = result[:MaxGrokTools]
		changed = true
	}
	if !changed {
		return body
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return body
	}
	updated, err := sjson.SetRawBytes(body, "tools", encoded)
	if err != nil {
		return body
	}
	return updated
}

func normalizeTool(tool gjson.Result, namespace string) (json.RawMessage, bool, bool) {
	toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
	if toolType == "tool_search" || toolType == "image_generation" {
		return nil, false, true
	}
	raw := []byte(tool.Raw)
	changed := false
	if toolType == "custom" {
		if strings.EqualFold(strings.TrimSpace(tool.Get("name").String()), "apply_patch") {
			return nil, false, true
		}
		var err error
		raw, err = sjson.SetBytes(raw, "type", "function")
		if err != nil {
			return json.RawMessage(tool.Raw), true, false
		}
		toolType = "function"
		changed = true
	}
	if toolType == "web_search" && gjson.GetBytes(raw, "external_web_access").Exists() {
		raw, _ = sjson.DeleteBytes(raw, "external_web_access")
		changed = true
	}
	if toolType == "function" {
		name := strings.TrimSpace(gjson.GetBytes(raw, "name").String())
		if name == "" {
			return nil, false, true
		}
		if strings.TrimSpace(gjson.GetBytes(raw, "description").String()) == "" {
			raw, _ = sjson.SetBytes(raw, "description", "Invoke "+name)
			changed = true
		}
		var parametersChanged bool
		raw, parametersChanged = normalizeFunctionParameters(raw)
		changed = changed || parametersChanged
	}
	// Codex Desktop's automation_update schema is known to hang the free/build
	// proxy. Restrict the workaround to the exact namespaced function.
	if toolType == "function" && strings.EqualFold(namespace, "codex_app") && strings.EqualFold(strings.TrimSpace(tool.Get("name").String()), "automation_update") {
		raw, _ = sjson.SetRawBytes(raw, "parameters", []byte(`{"type":"object","additionalProperties":true}`))
		raw, _ = sjson.SetBytes(raw, "strict", false)
		changed = true
	}
	return json.RawMessage(raw), true, changed
}

func normalizeFunctionParameters(raw []byte) ([]byte, bool) {
	parameters := gjson.GetBytes(raw, "parameters")
	if !parameters.Exists() || !parameters.IsObject() {
		updated, err := sjson.SetRawBytes(raw, "parameters", []byte(`{"type":"object","additionalProperties":true}`))
		if err != nil {
			return raw, false
		}
		return updated, true
	}

	parameterType := strings.ToLower(strings.TrimSpace(parameters.Get("type").String()))
	if parameterType == "object" || validObjectSchemaUnion(parameters, "oneOf") || validObjectSchemaUnion(parameters, "anyOf") {
		return raw, false
	}

	// Several OpenAI-compatible clients omit the root type while still sending
	// an object schema. Preserve that schema and make the object constraint
	// explicit instead of discarding its properties.
	if parameterType == "" && (parameters.Get("properties").Exists() || parameters.Get("additionalProperties").Exists()) {
		updated, err := sjson.SetBytes(raw, "parameters.type", "object")
		if err != nil {
			return raw, false
		}
		return updated, true
	}

	updated, err := sjson.SetRawBytes(raw, "parameters", []byte(`{"type":"object","additionalProperties":true}`))
	if err != nil {
		return raw, false
	}
	return updated, true
}

func validObjectSchemaUnion(parameters gjson.Result, field string) bool {
	branches := parameters.Get(field)
	if !branches.Exists() || !branches.IsArray() || len(branches.Array()) == 0 {
		return false
	}
	for _, branch := range branches.Array() {
		if !branch.IsObject() || !strings.EqualFold(strings.TrimSpace(branch.Get("type").String()), "object") {
			return false
		}
	}
	return true
}

func normalizeToolChoice(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		return body
	}
	body, _ = sjson.DeleteBytes(body, "tools")
	body, _ = sjson.DeleteBytes(body, "tool_choice")
	body, _ = sjson.DeleteBytes(body, "parallel_tool_calls")
	return body
}

func normalizeInputReasoningItems(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body
	}

	updated := body
	for i, item := range input.Array() {
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			continue
		}
		for _, field := range []string{"content", "encrypted_content"} {
			path := fmt.Sprintf("input.%d.%s", i, field)
			value := gjson.GetBytes(updated, path)
			if value.Exists() && value.Type == gjson.Null {
				next, err := sjson.DeleteBytes(updated, path)
				if err != nil {
					return body
				}
				updated = next
			}
		}
	}
	return mergeAdjacentInputReasoningSummaries(updated)
}

func mergeAdjacentInputReasoningSummaries(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body
	}
	changed := false
	items := make([]json.RawMessage, 0, len(input.Array()))
	for _, item := range input.Array() {
		if len(items) > 0 && canMergeInputReasoningSummary(items[len(items)-1], item) {
			merged, ok := appendInputReasoningSummary(items[len(items)-1], item.Get("summary").Array())
			if ok {
				items[len(items)-1] = json.RawMessage(merged)
				changed = true
				continue
			}
		}
		items = append(items, json.RawMessage(item.Raw))
	}
	if !changed {
		return body
	}
	rawInput, err := json.Marshal(items)
	if err != nil {
		return body
	}
	updated, err := sjson.SetRawBytes(body, "input", rawInput)
	if err != nil {
		return body
	}
	return updated
}

func canMergeInputReasoningSummary(previous json.RawMessage, current gjson.Result) bool {
	previousItem := gjson.ParseBytes(previous)
	if previousItem.Get("type").String() != "reasoning" || current.Get("type").String() != "reasoning" {
		return false
	}
	if !previousItem.Get("summary").IsArray() || !current.Get("summary").IsArray() || len(current.Get("summary").Array()) == 0 {
		return false
	}
	for name := range current.Map() {
		if name != "type" && name != "summary" {
			return false
		}
	}
	return true
}

func appendInputReasoningSummary(previous json.RawMessage, currentSummary []gjson.Result) ([]byte, bool) {
	updated := []byte(previous)
	summary := gjson.GetBytes(updated, "summary")
	if !summary.IsArray() {
		return previous, false
	}
	nextIndex := len(summary.Array())
	for i, item := range currentSummary {
		next, err := sjson.SetRawBytes(updated, fmt.Sprintf("summary.%d", nextIndex+i), []byte(item.Raw))
		if err != nil {
			return previous, false
		}
		updated = next
	}
	return updated, true
}

func sanitizeInputEncryptedContent(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body
	}
	items := make([]json.RawMessage, 0, len(input.Array()))
	changed := false
	for _, item := range input.Array() {
		itemType := strings.TrimSpace(item.Get("type").String())
		if itemType != "reasoning" && itemType != "compaction" {
			items = append(items, json.RawMessage(item.Raw))
			continue
		}
		encrypted := item.Get("encrypted_content")
		if !encrypted.Exists() {
			items = append(items, json.RawMessage(item.Raw))
			continue
		}
		valid := encrypted.Type == gjson.String && IsValidGrokEncryptedContent(encrypted.String())
		if valid {
			items = append(items, json.RawMessage(item.Raw))
			continue
		}
		changed = true
		if itemType == "compaction" {
			continue
		}
		cleaned, err := sjson.DeleteBytes([]byte(item.Raw), "encrypted_content")
		if err != nil {
			items = append(items, json.RawMessage(item.Raw))
			continue
		}
		items = append(items, json.RawMessage(cleaned))
	}
	if !changed {
		return mergeAdjacentInputReasoningSummaries(body)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return body
	}
	updated, err := sjson.SetRawBytes(body, "input", encoded)
	if err != nil {
		return body
	}
	return mergeAdjacentInputReasoningSummaries(updated)
}

func IsValidGrokEncryptedContent(raw string) bool {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > MaxGrokEncryptedContentLen || strings.Contains(raw, "=") || strings.HasPrefix(raw, "gAAAA") {
		return false
	}
	for _, r := range raw {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/') {
			return false
		}
	}
	decoded, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil || len(decoded) < MinGrokEncryptedContentDecodedLen {
		return false
	}
	return byteEntropyRatio(decoded) >= MinGrokEncryptedContentEntropy
}

func byteEntropyRatio(buf []byte) float64 {
	if len(buf) == 0 {
		return 0
	}
	var counts [256]int
	for _, value := range buf {
		counts[value]++
	}
	n := float64(len(buf))
	entropy := 0.0
	for _, count := range counts {
		if count == 0 {
			continue
		}
		p := float64(count) / n
		entropy -= p * math.Log2(p)
	}
	maxSymbols := len(buf)
	if maxSymbols > 256 {
		maxSymbols = 256
	}
	if maxSymbols <= 1 {
		return 0
	}
	return entropy / math.Log2(float64(maxSymbols))
}

// NormalizeResponsesObject converts xAI-specific reasoning output into the
// standard OpenAI Responses reasoning-summary shape.
func NormalizeResponsesObject(body []byte) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}
	normalized := normalizeReasoningPayload(body)
	return normalized, !bytes.Equal(normalized, body)
}

func normalizeReasoningPayload(body []byte) []byte {
	normalized := body
	if item := gjson.GetBytes(normalized, "item"); item.Exists() && item.IsObject() {
		updated := normalizeReasoningItem([]byte(item.Raw))
		if !bytes.Equal(updated, []byte(item.Raw)) {
			normalized, _ = sjson.SetRawBytes(normalized, "item", updated)
		}
	}
	for _, path := range []string{"output", "response.output"} {
		output := gjson.GetBytes(normalized, path)
		if !output.Exists() || !output.IsArray() {
			continue
		}
		items := make([]json.RawMessage, 0, len(output.Array()))
		changed := false
		for _, item := range output.Array() {
			updated := normalizeReasoningItem([]byte(item.Raw))
			changed = changed || !bytes.Equal(updated, []byte(item.Raw))
			items = append(items, json.RawMessage(updated))
		}
		if changed {
			encoded, _ := json.Marshal(items)
			normalized, _ = sjson.SetRawBytes(normalized, path, encoded)
		}
	}
	return normalized
}

func normalizeReasoningItem(item []byte) []byte {
	if !gjson.ValidBytes(item) || gjson.GetBytes(item, "type").String() != "reasoning" {
		return item
	}
	if gjson.GetBytes(item, "summary").IsArray() {
		return item
	}
	content := gjson.GetBytes(item, "content")
	if !content.IsArray() {
		return item
	}
	summary := make([]map[string]any, 0, len(content.Array()))
	remaining := make([]json.RawMessage, 0, len(content.Array()))
	for _, part := range content.Array() {
		if part.Get("type").String() == "reasoning_text" {
			summary = append(summary, map[string]any{"type": "summary_text", "text": part.Get("text").String()})
		} else {
			remaining = append(remaining, json.RawMessage(part.Raw))
		}
	}
	if len(summary) == 0 {
		return item
	}
	encodedSummary, _ := json.Marshal(summary)
	item, _ = sjson.SetRawBytes(item, "summary", encodedSummary)
	if len(remaining) == 0 {
		item, _ = sjson.DeleteBytes(item, "content")
	} else {
		encodedRemaining, _ := json.Marshal(remaining)
		item, _ = sjson.SetRawBytes(item, "content", encodedRemaining)
	}
	return item
}

// NormalizeResponsesEvent may expand one xAI event into multiple standard
// Responses events (reasoning_text.done becomes text.done + part.done).
func NormalizeResponsesEvent(data []byte) [][]byte {
	if !gjson.ValidBytes(data) {
		return [][]byte{data}
	}
	eventType := gjson.GetBytes(data, "type").String()
	if eventType == "response.reasoning_text.done" {
		textDone := data
		textDone, _ = sjson.SetBytes(textDone, "type", "response.reasoning_summary_text.done")
		textDone = normalizeSummaryIndex(textDone)
		partDone := normalizeSingleEvent(data)
		return [][]byte{textDone, partDone}
	}
	return [][]byte{normalizeSingleEvent(data)}
}

func normalizeSingleEvent(data []byte) []byte {
	normalized := data
	switch gjson.GetBytes(normalized, "type").String() {
	case "response.reasoning_text.delta":
		normalized, _ = sjson.SetBytes(normalized, "type", "response.reasoning_summary_text.delta")
		normalized = normalizeSummaryIndex(normalized)
	case "response.reasoning_text.done":
		normalized, _ = sjson.SetBytes(normalized, "type", "response.reasoning_summary_part.done")
		normalized, _ = sjson.SetBytes(normalized, "part.type", "summary_text")
		if text := gjson.GetBytes(normalized, "text"); text.Exists() {
			normalized, _ = sjson.SetBytes(normalized, "part.text", text.String())
		}
		normalized, _ = sjson.DeleteBytes(normalized, "text")
		normalized = normalizeSummaryIndex(normalized)
	case "response.content_part.added":
		if gjson.GetBytes(normalized, "part.type").String() == "reasoning_text" {
			normalized, _ = sjson.SetBytes(normalized, "type", "response.reasoning_summary_part.added")
			normalized, _ = sjson.SetBytes(normalized, "part.type", "summary_text")
			normalized = normalizeSummaryIndex(normalized)
		}
	case "response.content_part.done":
		if gjson.GetBytes(normalized, "part.type").String() == "reasoning_text" {
			normalized, _ = sjson.SetBytes(normalized, "type", "response.reasoning_summary_part.done")
			normalized, _ = sjson.SetBytes(normalized, "part.type", "summary_text")
			normalized = normalizeSummaryIndex(normalized)
		}
	}
	return normalizeReasoningPayload(normalized)
}

func normalizeSummaryIndex(data []byte) []byte {
	contentIndex := gjson.GetBytes(data, "content_index")
	if contentIndex.Exists() && !gjson.GetBytes(data, "summary_index").Exists() {
		data, _ = sjson.SetRawBytes(data, "summary_index", []byte(contentIndex.Raw))
	}
	data, _ = sjson.DeleteBytes(data, "content_index")
	return data
}

func NormalizedEventName(data []byte, fallback string) string {
	if eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String()); eventType != "" {
		return eventType
	}
	switch strings.TrimSpace(fallback) {
	case "response.reasoning_text.delta":
		return "response.reasoning_summary_text.delta"
	case "response.reasoning_text.done":
		return "response.reasoning_summary_part.done"
	default:
		return strings.TrimSpace(fallback)
	}
}

// NormalizeResponsesSSEStream returns a streaming reader that normalizes Grok
// Build SSE frames without buffering the whole response.
func NormalizeResponsesSSEStream(src io.Reader) io.ReadCloser {
	return NormalizeResponsesSSEStreamWithObserver(src, nil)
}

// NormalizeResponsesSSEStreamWithObserver normalizes Grok Build SSE frames and
// invokes observer for each normalized JSON event before it is written. The
// callback is synchronous and must stay lightweight; callers that persist
// replay state should use bounded, non-blocking or best-effort storage.
func NormalizeResponsesSSEStreamWithObserver(src io.Reader, observer func([]byte)) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer func() { _ = writer.Close() }()
		scanner := bufio.NewScanner(src)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		var eventName string
		var dataLines []string
		outputItems := map[int]json.RawMessage{}
		flush := func() error {
			if len(dataLines) == 0 {
				eventName = ""
				return nil
			}
			dataText := strings.Join(dataLines, "\n")
			dataLines = nil
			if strings.TrimSpace(dataText) == "[DONE]" {
				_, err := io.WriteString(writer, "data: [DONE]\n\n")
				eventName = ""
				return err
			}
			data := []byte(dataText)
			if gjson.ValidBytes(data) {
				if gjson.GetBytes(data, "type").String() == "response.output_item.done" {
					index := int(gjson.GetBytes(data, "output_index").Int())
					if item := gjson.GetBytes(data, "item"); item.Exists() && item.IsObject() {
						outputItems[index] = json.RawMessage(normalizeReasoningItem([]byte(item.Raw)))
					}
				}
				if gjson.GetBytes(data, "type").String() == "response.completed" {
					output := gjson.GetBytes(data, "response.output")
					if (!output.Exists() || (output.IsArray() && len(output.Array()) == 0)) && len(outputItems) > 0 {
						indexes := make([]int, 0, len(outputItems))
						for index := range outputItems {
							indexes = append(indexes, index)
						}
						sort.Ints(indexes)
						items := make([]json.RawMessage, 0, len(indexes))
						for _, index := range indexes {
							items = append(items, outputItems[index])
						}
						encoded, _ := json.Marshal(items)
						data, _ = sjson.SetRawBytes(data, "response.output", encoded)
					}
				}
			}
			for _, normalized := range NormalizeResponsesEvent(data) {
				if observer != nil {
					observer(append([]byte(nil), normalized...))
				}
				name := NormalizedEventName(normalized, eventName)
				if name != "" {
					if _, err := io.WriteString(writer, "event: "+name+"\n"); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(writer, "data: "+string(normalized)+"\n\n"); err != nil {
					return err
				}
			}
			eventName = ""
			return nil
		}
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if err := flush(); err != nil {
					return
				}
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case strings.HasPrefix(line, ":"):
				if _, err := io.WriteString(writer, line+"\n\n"); err != nil {
					return
				}
			}
		}
		_ = flush()
		if err := scanner.Err(); err != nil {
			_ = writer.CloseWithError(err)
		}
	}()
	if closer, ok := src.(io.Closer); ok {
		return &grokNormalizedReadCloser{ReadCloser: reader, source: closer}
	}
	return reader
}

type grokNormalizedReadCloser struct {
	io.ReadCloser
	source io.Closer
}

func (r *grokNormalizedReadCloser) Close() error {
	readErr := r.ReadCloser.Close()
	sourceErr := r.source.Close()
	if readErr != nil {
		return readErr
	}
	return sourceErr
}

func FreeUsageExhausted(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "code").String()))
	message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.message").String()))
	if message == "" {
		message = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error").String()))
	}
	if message == "" {
		message = strings.ToLower(string(body))
	}
	return strings.Contains(code, "free-usage-exhausted") || strings.Contains(message, "free-usage-exhausted") || strings.Contains(message, "included free usage")
}

func CredentialBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err == nil {
			return parsed != 0
		}
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	}
	return fallback
}
