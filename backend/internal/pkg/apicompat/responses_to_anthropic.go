package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Non-streaming: ResponsesResponse → AnthropicResponse
// ---------------------------------------------------------------------------

// ResponsesToAnthropic converts a Responses API response directly into an
// Anthropic Messages response. Reasoning output items are mapped to thinking
// blocks; function_call items become tool_use blocks.
func ResponsesToAnthropic(resp *ResponsesResponse, model string) *AnthropicResponse {
	out := &AnthropicResponse{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	var blocks []AnthropicContentBlock

	for _, item := range resp.Output {
		switch item.Type {
		case "reasoning":
			summaryText := ""
			for _, s := range item.Summary {
				if s.Type == "summary_text" && s.Text != "" {
					summaryText += s.Text
				}
			}
			if summaryText != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type:     "thinking",
					Thinking: summaryText,
				})
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					blocks = append(blocks, AnthropicContentBlock{
						Type: "text",
						Text: part.Text,
					})
				}
			}
		case "function_call":
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    fromResponsesCallID(item.CallID),
				Name:  item.Name,
				Input: sanitizeAnthropicToolUseInput(item.Name, item.Arguments),
			})
		case "web_search_call":
			toolUseID := "srvtoolu_" + item.ID
			query := ""
			if item.Action != nil {
				query = item.Action.Query
			}
			inputJSON, _ := json.Marshal(map[string]string{"query": query})
			blocks = append(blocks, AnthropicContentBlock{
				Type:  "server_tool_use",
				ID:    toolUseID,
				Name:  "web_search",
				Input: inputJSON,
			})
			emptyResults, _ := json.Marshal([]struct{}{})
			blocks = append(blocks, AnthropicContentBlock{
				Type:      "web_search_tool_result",
				ToolUseID: toolUseID,
				Content:   emptyResults,
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: ""})
	}
	out.Content = blocks

	out.StopReason = responsesStatusToAnthropicStopReason(resp.Status, resp.IncompleteDetails, blocks)

	if resp.Usage != nil {
		out.Usage = anthropicUsageFromResponsesUsage(resp.Usage)
	}

	return out
}

func anthropicUsageFromResponsesUsage(usage *ResponsesUsage) AnthropicUsage {
	if usage == nil {
		return AnthropicUsage{}
	}

	cachedTokens := 0
	if usage.InputTokensDetails != nil {
		cachedTokens = usage.InputTokensDetails.CachedTokens
	}

	inputTokens := usage.InputTokens - cachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	return AnthropicUsage{
		InputTokens:          inputTokens,
		OutputTokens:         usage.OutputTokens,
		CacheReadInputTokens: cachedTokens,
	}
}

func responsesStatusToAnthropicStopReason(status string, details *ResponsesIncompleteDetails, blocks []AnthropicContentBlock) string {
	switch status {
	case "incomplete":
		if details != nil && details.Reason == "max_output_tokens" {
			return "max_tokens"
		}
		return "end_turn"
	case "completed":
		if containsAnthropicToolUseBlock(blocks) {
			return "tool_use"
		}
		return "end_turn"
	default:
		return "end_turn"
	}
}

func containsAnthropicToolUseBlock(blocks []AnthropicContentBlock) bool {
	for _, block := range blocks {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

func sanitizeAnthropicToolUseInput(name string, raw string) json.RawMessage {
	if name != "Read" || raw == "" {
		return json.RawMessage(raw)
	}

	var input map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return json.RawMessage(raw)
	}

	if pages, ok := input["pages"]; !ok || string(pages) != `""` {
		return json.RawMessage(raw)
	}

	delete(input, "pages")
	sanitized, err := json.Marshal(input)
	if err != nil {
		return json.RawMessage(raw)
	}
	return sanitized
}

// ---------------------------------------------------------------------------
// Streaming: ResponsesStreamEvent → []AnthropicStreamEvent (stateful converter)
// ---------------------------------------------------------------------------

// ResponsesEventToAnthropicState tracks state for converting a sequence of
// Responses SSE events directly into Anthropic SSE events.
type ResponsesEventToAnthropicState struct {
	MessageStartSent bool
	MessageStopSent  bool

	ContentBlockIndex   int
	ContentBlockOpen    bool
	CurrentBlockType    string // "text" | "thinking" | "tool_use"
	CurrentToolName     string
	CurrentToolArgs     string
	CurrentToolHadDelta bool
	HasToolCall         bool

	// OutputIndexToBlockIdx maps Responses output_index → Anthropic content block index.
	OutputIndexToBlockIdx map[int]int

	InputTokens          int
	OutputTokens         int
	CacheReadInputTokens int

	ResponseID string
	Model      string
	Created    int64
}

// NewResponsesEventToAnthropicState returns an initialised stream state.
func NewResponsesEventToAnthropicState() *ResponsesEventToAnthropicState {
	return &ResponsesEventToAnthropicState{
		OutputIndexToBlockIdx: make(map[int]int),
		Created:               time.Now().Unix(),
	}
}

// ResponsesEventToAnthropicEvents converts a single Responses SSE event into
// zero or more Anthropic SSE events, updating state as it goes.
func ResponsesEventToAnthropicEvents(
	evt *ResponsesStreamEvent,
	state *ResponsesEventToAnthropicState,
) []AnthropicStreamEvent {
	if evt == nil || state == nil || !isResponsesToAnthropicEvent(evt.Type) {
		return nil
	}

	NormalizeResponsesStreamEvent(evt)
	captureResponsesAnthropicStreamMetadata(evt, state)

	if evt.Type == "response.created" {
		return ensureAnthropicMessageStart(state)
	}

	// Several OpenAI-compatible gateways (notably new-api relaying xAI/Grok)
	// can omit response.created or start directly with a delta/terminal event.
	// Anthropic clients require message_start to be the first semantic event,
	// so synthesize it before every recognized event when necessary.
	events := ensureAnthropicMessageStart(state)
	var converted []AnthropicStreamEvent
	switch evt.Type {
	case "response.output_item.added":
		converted = resToAnthHandleOutputItemAdded(evt, state)
	case "response.output_text.delta":
		converted = resToAnthHandleTextDelta(evt, state)
	case "response.output_text.done":
		converted = resToAnthHandleBlockDone(state)
	case "response.function_call_arguments.delta":
		converted = resToAnthHandleFuncArgsDelta(evt, state)
	case "response.function_call_arguments.done":
		converted = resToAnthHandleFuncArgsDone(evt, state)
	case "response.output_item.done":
		converted = resToAnthHandleOutputItemDone(evt, state)
	case "response.reasoning_summary_text.delta":
		converted = resToAnthHandleReasoningDelta(evt, state)
	case "response.reasoning_summary_text.done":
		converted = resToAnthHandleBlockDone(state)
	// response.done 是 Realtime/WS 与项目透传路径使用的终止别名；
	// 普通 Responses HTTP SSE 的公开终止事件仍以 response.completed 为主。
	case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
		converted = resToAnthHandleCompleted(evt, state)
	}
	return append(events, converted...)
}

func isResponsesToAnthropicEvent(eventType string) bool {
	switch eventType {
	case "response.created",
		"response.output_item.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func captureResponsesAnthropicStreamMetadata(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) {
	if evt == nil || state == nil {
		return
	}
	if evt.Response != nil {
		if id := strings.TrimSpace(evt.Response.ID); id != "" {
			state.ResponseID = id
		}
		if state.Model == "" {
			state.Model = strings.TrimSpace(evt.Response.Model)
		}
		if evt.Response.Usage != nil {
			usage := anthropicUsageFromResponsesUsage(evt.Response.Usage)
			state.InputTokens = usage.InputTokens
			state.OutputTokens = usage.OutputTokens
			state.CacheReadInputTokens = usage.CacheReadInputTokens
		}
	}
	if evt.Usage != nil {
		usage := anthropicUsageFromResponsesUsage(evt.Usage)
		state.InputTokens = usage.InputTokens
		state.OutputTokens = usage.OutputTokens
		state.CacheReadInputTokens = usage.CacheReadInputTokens
	}
}

func ensureAnthropicMessageStart(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state == nil || state.MessageStartSent {
		return nil
	}
	state.MessageStartSent = true
	if strings.TrimSpace(state.ResponseID) == "" {
		state.ResponseID = fmt.Sprintf("msg_router_%d", state.Created)
	}
	if strings.TrimSpace(state.Model) == "" {
		state.Model = "unknown"
	}

	return []AnthropicStreamEvent{{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:      state.ResponseID,
			Type:    "message",
			Role:    "assistant",
			Content: []AnthropicContentBlock{},
			Model:   state.Model,
			Usage: AnthropicUsage{
				InputTokens:          state.InputTokens,
				OutputTokens:         0,
				CacheReadInputTokens: state.CacheReadInputTokens,
			},
		},
	}}
}

// FinalizeResponsesAnthropicStream emits synthetic termination events if the
// stream ended without a proper completion event.
func FinalizeResponsesAnthropicStream(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.MessageStartSent || state.MessageStopSent {
		return nil
	}

	var events []AnthropicStreamEvent
	events = append(events, closeCurrentBlock(state)...)

	stopReason := "end_turn"
	if state.HasToolCall {
		stopReason = "tool_use"
	}

	events = append(events,
		AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &AnthropicDelta{
				StopReason: stopReason,
			},
			Usage: &AnthropicUsage{
				InputTokens:          state.InputTokens,
				OutputTokens:         state.OutputTokens,
				CacheReadInputTokens: state.CacheReadInputTokens,
			},
		},
		AnthropicStreamEvent{Type: "message_stop"},
	)
	state.MessageStopSent = true
	return events
}

// ResponsesAnthropicEventToSSE formats an AnthropicStreamEvent as an SSE line pair.
func ResponsesAnthropicEventToSSE(evt AnthropicStreamEvent) (string, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", evt.Type, data), nil
}

// --- internal handlers ---

func resToAnthHandleCreated(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	captureResponsesAnthropicStreamMetadata(evt, state)
	return ensureAnthropicMessageStart(state)
}

func resToAnthHandleOutputItemAdded(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Item == nil {
		return nil
	}

	switch evt.Item.Type {
	case "function_call":
		var events []AnthropicStreamEvent
		events = append(events, closeCurrentBlock(state)...)

		idx := state.ContentBlockIndex
		state.OutputIndexToBlockIdx[evt.OutputIndex] = idx
		state.ContentBlockOpen = true
		state.CurrentBlockType = "tool_use"
		state.CurrentToolName = evt.Item.Name
		state.CurrentToolArgs = ""
		state.CurrentToolHadDelta = false
		state.HasToolCall = true

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type:  "tool_use",
				ID:    fromResponsesCallID(evt.Item.CallID),
				Name:  evt.Item.Name,
				Input: json.RawMessage("{}"),
			},
		})
		return events

	case "reasoning":
		var events []AnthropicStreamEvent
		events = append(events, closeCurrentBlock(state)...)

		idx := state.ContentBlockIndex
		state.OutputIndexToBlockIdx[evt.OutputIndex] = idx
		state.ContentBlockOpen = true
		state.CurrentBlockType = "thinking"

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type:     "thinking",
				Thinking: "",
			},
		})
		return events

	case "message":
		return nil
	}

	return nil
}

func resToAnthHandleTextDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Delta == "" {
		return nil
	}

	var events []AnthropicStreamEvent

	if !state.ContentBlockOpen || state.CurrentBlockType != "text" {
		events = append(events, closeCurrentBlock(state)...)

		idx := state.ContentBlockIndex
		state.ContentBlockOpen = true
		state.CurrentBlockType = "text"

		events = append(events, AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: &idx,
			ContentBlock: &AnthropicContentBlock{
				Type: "text",
				Text: "",
			},
		})
	}

	idx := state.ContentBlockIndex
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &AnthropicDelta{
			Type: "text_delta",
			Text: evt.Delta,
		},
	})
	return events
}

func resToAnthHandleFuncArgsDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Delta == "" {
		return nil
	}

	if state.CurrentBlockType == "tool_use" && state.CurrentToolName == "Read" {
		state.CurrentToolArgs += evt.Delta
		return nil
	}
	if state.CurrentBlockType == "tool_use" {
		state.CurrentToolHadDelta = true
	}

	blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]
	if !ok {
		return nil
	}

	return []AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &blockIdx,
		Delta: &AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: evt.Delta,
		},
	}}
}

func resToAnthHandleFuncArgsDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.CurrentBlockType != "tool_use" {
		return resToAnthHandleBlockDone(state)
	}

	raw := evt.Arguments
	if raw == "" {
		raw = state.CurrentToolArgs
	}
	if raw == "" || state.CurrentToolHadDelta {
		return closeCurrentBlock(state)
	}
	if state.CurrentToolName == "Read" {
		sanitized := sanitizeAnthropicToolUseInput(state.CurrentToolName, raw)
		if len(sanitized) == 0 {
			return closeCurrentBlock(state)
		}
		raw = string(sanitized)
	}

	idx := state.ContentBlockIndex
	events := []AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &AnthropicDelta{
			Type:        "input_json_delta",
			PartialJSON: raw,
		},
	}}
	events = append(events, closeCurrentBlock(state)...)
	return events
}

func resToAnthHandleReasoningDelta(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Delta == "" {
		return nil
	}

	blockIdx, ok := state.OutputIndexToBlockIdx[evt.OutputIndex]
	if !ok {
		return nil
	}

	return []AnthropicStreamEvent{{
		Type:  "content_block_delta",
		Index: &blockIdx,
		Delta: &AnthropicDelta{
			Type:     "thinking_delta",
			Thinking: evt.Delta,
		},
	}}
}

func resToAnthHandleBlockDone(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.ContentBlockOpen {
		return nil
	}
	return closeCurrentBlock(state)
}

func resToAnthHandleOutputItemDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if evt.Item == nil {
		return nil
	}

	// Handle web_search_call → synthesize server_tool_use + web_search_tool_result blocks.
	if evt.Item.Type == "web_search_call" && evt.Item.Status == "completed" {
		return resToAnthHandleWebSearchDone(evt, state)
	}

	if state.ContentBlockOpen {
		return closeCurrentBlock(state)
	}
	return nil
}

// resToAnthHandleWebSearchDone converts an OpenAI web_search_call output item
// into Anthropic server_tool_use + web_search_tool_result content block pairs.
// This allows Claude Code to count the searches performed.
func resToAnthHandleWebSearchDone(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	var events []AnthropicStreamEvent
	events = append(events, closeCurrentBlock(state)...)

	toolUseID := "srvtoolu_" + evt.Item.ID
	query := ""
	if evt.Item.Action != nil {
		query = evt.Item.Action.Query
	}
	inputJSON, _ := json.Marshal(map[string]string{"query": query})

	// Emit server_tool_use block (start + stop).
	idx1 := state.ContentBlockIndex
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx1,
		ContentBlock: &AnthropicContentBlock{
			Type:  "server_tool_use",
			ID:    toolUseID,
			Name:  "web_search",
			Input: inputJSON,
		},
	})
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx1,
	})
	state.ContentBlockIndex++

	// Emit web_search_tool_result block (start + stop).
	// Content is empty because OpenAI does not expose individual search results;
	// the model consumes them internally and produces text output.
	emptyResults, _ := json.Marshal([]struct{}{})
	idx2 := state.ContentBlockIndex
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_start",
		Index: &idx2,
		ContentBlock: &AnthropicContentBlock{
			Type:      "web_search_tool_result",
			ToolUseID: toolUseID,
			Content:   emptyResults,
		},
	})
	events = append(events, AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: &idx2,
	})
	state.ContentBlockIndex++

	return events
}

func resToAnthHandleCompleted(evt *ResponsesStreamEvent, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if state.MessageStopSent {
		return nil
	}

	var events []AnthropicStreamEvent
	// Some compatible gateways emit only a terminal event containing the full
	// response.output array. Reconstruct content blocks when no incremental
	// block has been observed; otherwise terminal-only Grok/new-api responses
	// look like an empty Anthropic message even though the upstream answered.
	if state.ContentBlockIndex == 0 && !state.ContentBlockOpen && evt.Response != nil {
		events = append(events, emitAnthropicTerminalOutput(evt.Response, state)...)
	}
	events = append(events, closeCurrentBlock(state)...)

	stopReason := "end_turn"
	if evt.Usage != nil {
		usage := anthropicUsageFromResponsesUsage(evt.Usage)
		state.InputTokens = usage.InputTokens
		state.OutputTokens = usage.OutputTokens
		state.CacheReadInputTokens = usage.CacheReadInputTokens
	}
	if evt.Response != nil {
		if evt.Response.Usage != nil {
			usage := anthropicUsageFromResponsesUsage(evt.Response.Usage)
			state.InputTokens = usage.InputTokens
			state.OutputTokens = usage.OutputTokens
			state.CacheReadInputTokens = usage.CacheReadInputTokens
		}
		switch evt.Response.Status {
		case "incomplete":
			if evt.Response.IncompleteDetails != nil && evt.Response.IncompleteDetails.Reason == "max_output_tokens" {
				stopReason = "max_tokens"
			}
		case "completed":
			if state.HasToolCall {
				stopReason = "tool_use"
			}
		}
	}

	events = append(events,
		AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &AnthropicDelta{
				StopReason: stopReason,
			},
			Usage: &AnthropicUsage{
				InputTokens:          state.InputTokens,
				OutputTokens:         state.OutputTokens,
				CacheReadInputTokens: state.CacheReadInputTokens,
			},
		},
		AnthropicStreamEvent{Type: "message_stop"},
	)
	state.MessageStopSent = true
	return events
}

func emitAnthropicTerminalOutput(resp *ResponsesResponse, state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if resp == nil || state == nil || len(resp.Output) == 0 {
		return nil
	}

	converted := ResponsesToAnthropic(resp, state.Model)
	if converted == nil {
		return nil
	}

	var events []AnthropicStreamEvent
	for _, block := range converted.Content {
		idx := state.ContentBlockIndex
		switch block.Type {
		case "text":
			events = append(events, AnthropicStreamEvent{
				Type:  "content_block_start",
				Index: &idx,
				ContentBlock: &AnthropicContentBlock{
					Type: "text",
					Text: "",
				},
			})
			if block.Text != "" {
				events = append(events, AnthropicStreamEvent{
					Type:  "content_block_delta",
					Index: &idx,
					Delta: &AnthropicDelta{Type: "text_delta", Text: block.Text},
				})
			}

		case "thinking":
			events = append(events, AnthropicStreamEvent{
				Type:  "content_block_start",
				Index: &idx,
				ContentBlock: &AnthropicContentBlock{
					Type:     "thinking",
					Thinking: "",
				},
			})
			if block.Thinking != "" {
				events = append(events, AnthropicStreamEvent{
					Type:  "content_block_delta",
					Index: &idx,
					Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: block.Thinking},
				})
			}

		case "tool_use":
			state.HasToolCall = true
			events = append(events, AnthropicStreamEvent{
				Type:  "content_block_start",
				Index: &idx,
				ContentBlock: &AnthropicContentBlock{
					Type:  "tool_use",
					ID:    block.ID,
					Name:  block.Name,
					Input: json.RawMessage("{}"),
				},
			})
			raw := strings.TrimSpace(string(block.Input))
			if raw != "" && raw != "{}" {
				events = append(events, AnthropicStreamEvent{
					Type:  "content_block_delta",
					Index: &idx,
					Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: raw},
				})
			}

		default:
			continue
		}
		events = append(events, AnthropicStreamEvent{Type: "content_block_stop", Index: &idx})
		state.ContentBlockIndex++
	}
	return events
}

func closeCurrentBlock(state *ResponsesEventToAnthropicState) []AnthropicStreamEvent {
	if !state.ContentBlockOpen {
		return nil
	}
	idx := state.ContentBlockIndex
	state.ContentBlockOpen = false
	state.ContentBlockIndex++
	state.CurrentToolName = ""
	state.CurrentToolArgs = ""
	state.CurrentToolHadDelta = false
	return []AnthropicStreamEvent{{
		Type:  "content_block_stop",
		Index: &idx,
	}}
}
