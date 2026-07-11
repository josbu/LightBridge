package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesTerminalEventAddsCanonicalUsage(t *testing.T) {
	input := []byte(`{"type":"response.completed","response":{"id":"resp_grok","model":"grok-4.5","status":"completed","output":[]}}`)
	got, changed := normalizeOpenAIResponsesTerminalEvent(input)
	require.True(t, changed)
	assert.Equal(t, int64(0), gjson.GetBytes(got, "response.usage.input_tokens").Int())
	assert.Equal(t, int64(0), gjson.GetBytes(got, "response.usage.output_tokens").Int())
	assert.Equal(t, int64(0), gjson.GetBytes(got, "response.usage.total_tokens").Int())
	assert.True(t, gjson.GetBytes(got, "response.usage.input_tokens_details").IsObject())
	assert.True(t, gjson.GetBytes(got, "response.usage.output_tokens_details").IsObject())
}

func TestNormalizeOpenAIResponsesTerminalEventCopiesTopLevelUsage(t *testing.T) {
	input := []byte(`{"type":"response.done","id":"resp_flat","model":"grok-4.5","usage":{"prompt_tokens":11,"completion_tokens":7},"output":[]}`)
	got, changed := normalizeOpenAIResponsesTerminalEvent(input)
	require.True(t, changed)
	assert.Equal(t, "resp_flat", gjson.GetBytes(got, "response.id").String())
	assert.Equal(t, int64(11), gjson.GetBytes(got, "response.usage.input_tokens").Int())
	assert.Equal(t, int64(7), gjson.GetBytes(got, "response.usage.output_tokens").Int())
	assert.Equal(t, int64(18), gjson.GetBytes(got, "response.usage.total_tokens").Int())
}

func TestNormalizeOpenAIResponsesObjectAddsUsage(t *testing.T) {
	input := []byte(`{"id":"resp_1","object":"response","status":"completed","output":[]}`)
	got, changed := normalizeOpenAIResponsesObject(input)
	require.True(t, changed)
	assert.True(t, gjson.GetBytes(got, "usage").IsObject())
	assert.Equal(t, int64(0), gjson.GetBytes(got, "usage.input_tokens").Int())
}

func TestNormalizeOpenAIResponsesTerminalEventSupportsCancelledAlias(t *testing.T) {
	input := []byte(`{"type":"response.cancelled"}`)
	got, changed := normalizeOpenAIResponsesTerminalEvent(input)
	require.True(t, changed)
	assert.Equal(t, "incomplete", gjson.GetBytes(got, "response.status").String())
	assert.True(t, gjson.GetBytes(got, "response.output").IsArray())
	assert.Equal(t, int64(0), gjson.GetBytes(got, "response.usage.input_tokens").Int())
}
