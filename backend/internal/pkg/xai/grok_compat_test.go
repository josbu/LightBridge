package xai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResolveChatBaseURLModes(t *testing.T) {
	t.Parallel()

	buildURL, err := ResolveChatBaseURL(DefaultBaseURL, false)
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL, buildURL)

	apiURL, err := ResolveChatBaseURL(DefaultBaseURL, true)
	require.NoError(t, err)
	require.Equal(t, DefaultBaseURL, apiURL)

	explicitCLI, err := ResolveChatBaseURL(DefaultCLIBaseURL, false)
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL, explicitCLI)
}

func TestApplyChatHeadersForBuildProxy(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, DefaultCLIBaseURL+"/responses", nil)
	require.NoError(t, err)

	ApplyChatHeaders(req, "token", true, false, DefaultCLIBaseURL, "conversation")

	require.Equal(t, "Bearer token", req.Header.Get("Authorization"))
	require.Equal(t, GrokTokenAuthValue, req.Header.Get(GrokTokenAuthHeader))
	require.Equal(t, GrokClientVersionValue, req.Header.Get(GrokClientVersionHeader))
	require.Equal(t, GrokClientIdentifierValue, req.Header.Get(GrokClientIdentifierHeader))
	require.Equal(t, GrokBuildUserAgent, req.Header.Get("User-Agent"))
	require.Equal(t, "conversation", req.Header.Get(GrokConversationHeader))
	require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
}

func TestPatchResponsesRequestNormalizesCodexTools(t *testing.T) {
	body := []byte(`{
		"model":"grok",
		"prompt_cache_retention":"24h",
		"stream_options":{"include_usage":true},
		"tools":[
			{"type":"custom","name":"apply_patch"},
			{"type":"namespace","name":"codex_app","tools":[{"type":"function","name":"automation_update","strict":true,"parameters":{"type":"object","properties":{"nested":{"type":"object"}}}}]},
			{"type":"custom","name":"shell"},
			{"type":"tool_search"}
		]
	}`)

	patched, err := PatchResponsesRequest(body, "grok-build-0.1")
	require.NoError(t, err)
	require.Equal(t, "grok-build-0.1", gjson.GetBytes(patched, "model").String())
	require.False(t, gjson.GetBytes(patched, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(patched, "stream_options").Exists())
	require.Len(t, gjson.GetBytes(patched, "tools").Array(), 2)
	require.Equal(t, "automation_update", gjson.GetBytes(patched, "tools.0.name").String())
	require.False(t, gjson.GetBytes(patched, "tools.0.strict").Bool())
	require.True(t, gjson.GetBytes(patched, "tools.0.parameters.additionalProperties").Bool())
	require.Equal(t, "function", gjson.GetBytes(patched, "tools.1.type").String())
	require.Equal(t, "shell", gjson.GetBytes(patched, "tools.1.name").String())
}

func TestNormalizeResponsesEventReasoningDone(t *testing.T) {
	events := NormalizeResponsesEvent([]byte(`{"type":"response.reasoning_text.done","content_index":2,"text":"thought"}`))
	require.Len(t, events, 2)
	require.Equal(t, "response.reasoning_summary_text.done", gjson.GetBytes(events[0], "type").String())
	require.Equal(t, int64(2), gjson.GetBytes(events[0], "summary_index").Int())
	require.Equal(t, "response.reasoning_summary_part.done", gjson.GetBytes(events[1], "type").String())
	require.Equal(t, "summary_text", gjson.GetBytes(events[1], "part.type").String())
	require.Equal(t, "thought", gjson.GetBytes(events[1], "part.text").String())
}

func TestNormalizeResponsesSSEStreamPatchesCompletedOutput(t *testing.T) {
	stream := strings.Join([]string{
		"event: response.output_item.done",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"r1","type":"reasoning","content":[{"type":"reasoning_text","text":"reason"}]}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
	}, "\n")

	reader := NormalizeResponsesSSEStream(strings.NewReader(stream))
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, `"summary":[{"text":"reason","type":"summary_text"}]`)
	require.Contains(t, text, `"response":{"id":"resp","output":[`)
}

func TestFreeUsageExhausted(t *testing.T) {
	require.True(t, FreeUsageExhausted([]byte(`{"code":"subscription:free-usage-exhausted","error":"Included free usage exhausted"}`)))
	require.False(t, FreeUsageExhausted([]byte(`{"error":{"message":"ordinary rate limit"}}`)))
}

func TestNormalizeResponsesSSEStreamObserverSeesPatchedCompletedOutput(t *testing.T) {
	stream := strings.Join([]string{
		"event: response.output_item.done",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_tool","output":[]}}`,
		"",
	}, "\n")

	var observedCompleted []byte
	reader := NormalizeResponsesSSEStreamWithObserver(strings.NewReader(stream), func(event []byte) {
		if gjson.GetBytes(event, "type").String() == "response.completed" {
			observedCompleted = append([]byte(nil), event...)
		}
	})
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	_, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NotEmpty(t, observedCompleted)
	require.Equal(t, "function_call", gjson.GetBytes(observedCompleted, "response.output.0.type").String())
	require.Equal(t, "call_1", gjson.GetBytes(observedCompleted, "response.output.0.call_id").String())
}

func TestPatchResponsesRequestRemovesEncryptedReasoningIncludeAndNormalizesReasoningInput(t *testing.T) {
	body := []byte(`{
		"model":"grok-4.5",
		"include":["reasoning.encrypted_content","web_search_call.action.sources"],
		"input":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"first"}],"content":null,"encrypted_content":null},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"second"}]},
			{"type":"message","role":"user","content":"continue"}
		]
	}`)

	patched, err := PatchResponsesRequest(body, "grok-4.5")
	require.NoError(t, err)
	require.Equal(t, "web_search_call.action.sources", gjson.GetBytes(patched, "include.0").String())
	require.Len(t, gjson.GetBytes(patched, "include").Array(), 1)
	require.Len(t, gjson.GetBytes(patched, "input").Array(), 2)
	require.False(t, gjson.GetBytes(patched, "input.0.content").Exists())
	require.False(t, gjson.GetBytes(patched, "input.0.encrypted_content").Exists())
	require.Equal(t, "first", gjson.GetBytes(patched, "input.0.summary.0.text").String())
	require.Equal(t, "second", gjson.GetBytes(patched, "input.0.summary.1.text").String())
	require.Equal(t, "message", gjson.GetBytes(patched, "input.1.type").String())
}

func TestPatchResponsesRequestDeletesIncludeWhenOnlyEncryptedReasoningRequested(t *testing.T) {
	patched, err := PatchResponsesRequest([]byte(`{
		"model":"grok-4.5",
		"include":["reasoning.encrypted_content"],
		"input":"hello"
	}`), "grok-4.5")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(patched, "include").Exists())
}

func TestPatchResponsesRequestNormalizesFunctionToolSchemas(t *testing.T) {
	body := []byte(`{
		"model":"grok-4.5",
		"tools":[
			{"type":"function","name":"array_schema","parameters":{"type":"array","items":{"type":"string"}}},
			{"type":"function","name":"implicit_object","description":"","parameters":{"properties":{"query":{"type":"string"}},"required":["query"]}},
			{"type":"function","name":"union_schema","description":"Choose an object","parameters":{"oneOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"number"}}}]}},
			{"type":"function","name":"","description":"invalid","parameters":{"type":"object"}}
		]
	}`)

	patched, err := PatchResponsesRequest(body, "grok-4.5")
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(patched, "tools").Array(), 3)

	require.Equal(t, "Invoke array_schema", gjson.GetBytes(patched, "tools.0.description").String())
	require.Equal(t, "object", gjson.GetBytes(patched, "tools.0.parameters.type").String())
	require.True(t, gjson.GetBytes(patched, "tools.0.parameters.additionalProperties").Bool())

	require.Equal(t, "Invoke implicit_object", gjson.GetBytes(patched, "tools.1.description").String())
	require.Equal(t, "object", gjson.GetBytes(patched, "tools.1.parameters.type").String())
	require.Equal(t, "string", gjson.GetBytes(patched, "tools.1.parameters.properties.query.type").String())

	require.Equal(t, "Choose an object", gjson.GetBytes(patched, "tools.2.description").String())
	require.Equal(t, "object", gjson.GetBytes(patched, "tools.2.parameters.oneOf.0.type").String())
	require.Equal(t, "object", gjson.GetBytes(patched, "tools.2.parameters.oneOf.1.type").String())
}

func TestPatchResponsesRequestCapsToolCount(t *testing.T) {
	tools := make([]map[string]any, 0, MaxGrokTools+5)
	for i := 0; i < MaxGrokTools+5; i++ {
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        fmt.Sprintf("tool_%03d", i),
			"description": "Test tool",
			"parameters":  map[string]any{"type": "object"},
		})
	}
	body, err := json.Marshal(map[string]any{
		"model": "grok-4.5",
		"tools": tools,
	})
	require.NoError(t, err)

	patched, err := PatchResponsesRequest(body, "grok-4.5")
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(patched, "tools").Array(), MaxGrokTools)
	require.Equal(t, "tool_199", gjson.GetBytes(patched, "tools.199.name").String())
}

func TestPatchOfficialXAIResponsesRequestPreservesOfficialFields(t *testing.T) {
	body := []byte(`{
		"model":"client-model",
		"include":["reasoning.encrypted_content"],
		"prompt_cache_retention":"24h",
		"safety_identifier":"tenant-1",
		"stream_options":{"include_usage":true},
		"tools":[{"type":"custom","name":"apply_patch","description":"Apply patch"}]
	}`)

	patched, err := PatchOfficialXAIResponsesRequest(body, "grok-4.5")
	require.NoError(t, err)
	require.Equal(t, "grok-4.5", gjson.GetBytes(patched, "model").String())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(patched, "include.0").String())
	require.Equal(t, "24h", gjson.GetBytes(patched, "prompt_cache_retention").String())
	require.Equal(t, "tenant-1", gjson.GetBytes(patched, "safety_identifier").String())
	require.True(t, gjson.GetBytes(patched, "stream_options.include_usage").Bool())
	require.Equal(t, "custom", gjson.GetBytes(patched, "tools.0.type").String())
}
