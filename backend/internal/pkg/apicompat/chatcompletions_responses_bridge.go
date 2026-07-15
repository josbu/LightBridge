package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponsesToChatCompletionsRequest converts a Responses API request into a
// Chat Completions request for upstreams that only implement
// /v1/chat/completions.
func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}

	messages, err := responsesInputToChatMessages(req.Instructions, req.Input)
	if err != nil {
		return nil, err
	}

	out := &ChatCompletionsRequest{
		Model:               req.Model,
		Messages:            messages,
		MaxCompletionTokens: req.MaxOutputTokens,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		Stream:              req.Stream,
		ParallelToolCalls:   req.ParallelToolCalls,
		ServiceTier:         req.ServiceTier,
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if len(req.Tools) > 0 {
		out.Tools = responsesToolsToChatTools(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		out.ToolChoice = responsesToolChoiceToChatToolChoice(req.ToolChoice)
	}

	return out, nil
}

func responsesInputToChatMessages(instructions string, inputRaw json.RawMessage) ([]ChatMessage, error) {
	var messages []ChatMessage
	if strings.TrimSpace(instructions) != "" {
		content, _ := json.Marshal(instructions)
		messages = append(messages, ChatMessage{
			Role:    "system",
			Content: content,
		})
	}

	inputRaw = bytesTrimSpace(inputRaw)
	if len(inputRaw) == 0 || string(inputRaw) == "null" {
		return messages, nil
	}

	var inputText string
	if err := json.Unmarshal(inputRaw, &inputText); err == nil {
		content, _ := json.Marshal(inputText)
		messages = append(messages, ChatMessage{
			Role:    "user",
			Content: content,
		})
		return messages, nil
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(inputRaw, &rawItems); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}

	for _, raw := range rawItems {
		raw = bytesTrimSpace(raw)
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			var text string
			if textErr := json.Unmarshal(raw, &text); textErr == nil {
				content, _ := json.Marshal(text)
				messages = append(messages, ChatMessage{Role: "user", Content: content})
				continue
			}
			return nil, fmt.Errorf("parse responses input item: %w", err)
		}

		role := chatCompletionsBridgeRole(rawString(item["role"]))
		itemType := rawString(item["type"])
		switch itemType {
		case "function_call":
			arguments := rawString(item["arguments"])
			if strings.TrimSpace(arguments) == "" {
				arguments = "{}"
			}
			callID := rawString(item["call_id"])
			if callID == "" {
				callID = rawString(item["id"])
			}
			name := strings.TrimSpace(rawString(item["name"]))
			if name == "" {
				return nil, fmt.Errorf("responses function_call name is empty")
			}
			appendResponsesFunctionCallToChatMessages(&messages, ChatToolCall{
				ID:   callID,
				Type: "function",
				Function: ChatFunctionCall{
					Name:      name,
					Arguments: arguments,
				},
			})
			continue
		case "function_call_output":
			content, err := responsesFunctionOutputToChatContent(item["output"])
			if err != nil {
				return nil, err
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: rawString(item["call_id"]),
				Content:    content,
			})
			continue
		case "input_text", "text":
			content, _ := json.Marshal(rawString(item["text"]))
			messages = append(messages, ChatMessage{Role: "user", Content: content})
			continue
		case "input_image":
			content, err := chatContentFromSingleResponsesPart(itemType, item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, ChatMessage{Role: "user", Content: content})
			continue
		}

		content := item["content"]
		if len(bytesTrimSpace(content)) == 0 {
			if text := rawString(item["text"]); text != "" {
				content, _ = json.Marshal(text)
			}
		}
		chatContent, err := responsesContentToChatContent(content, role)
		if err != nil {
			return nil, err
		}
		messages = append(messages, ChatMessage{
			Role:    role,
			Content: chatContent,
		})
	}

	return messages, nil
}

func appendResponsesFunctionCallToChatMessages(messages *[]ChatMessage, call ChatToolCall) {
	if messages == nil {
		return
	}
	if count := len(*messages); count > 0 && (*messages)[count-1].Role == "assistant" {
		(*messages)[count-1].ToolCalls = append((*messages)[count-1].ToolCalls, call)
		return
	}
	*messages = append(*messages, ChatMessage{
		Role:      "assistant",
		ToolCalls: []ChatToolCall{call},
	})
}

func responsesFunctionOutputToChatContent(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return json.Marshal("")
	}
	var output string
	if err := json.Unmarshal(raw, &output); err == nil {
		return json.Marshal(output)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("parse responses function_call_output: invalid JSON")
	}
	return json.Marshal(string(raw))
}

func chatCompletionsBridgeRole(role string) string {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return "user"
	}
	if strings.EqualFold(trimmed, "developer") {
		return "system"
	}
	return role
}

func responsesContentToChatContent(raw json.RawMessage, role string) (json.RawMessage, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		empty, _ := json.Marshal("")
		return empty, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return raw, nil
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(raw, &rawParts); err == nil {
		return responsesContentPartsToChatContent(rawParts, role)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		return chatContentFromSingleResponsesPart(rawString(obj["type"]), obj)
	}

	return raw, nil
}

func responsesContentPartsToChatContent(rawParts []json.RawMessage, role string) (json.RawMessage, error) {
	var textParts []string
	var chatParts []ChatContentPart
	hasNonText := false

	for _, rawPart := range rawParts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(rawPart, &part); err != nil {
			continue
		}
		partType := rawString(part["type"])
		switch partType {
		case "input_text", "output_text", "text", "":
			text := rawString(part["text"])
			if text == "" {
				continue
			}
			textParts = append(textParts, text)
			chatParts = append(chatParts, ChatContentPart{Type: "text", Text: text})
		case "input_image", "image_url":
			imageURL := rawString(part["image_url"])
			if imageURL == "" {
				imageURL = rawNestedString(part["image_url"], "url")
			}
			if imageURL == "" {
				continue
			}
			hasNonText = true
			chatParts = append(chatParts, ChatContentPart{
				Type:     "image_url",
				ImageURL: &ChatImageURL{URL: imageURL},
			})
		}
	}

	if !hasNonText {
		joined, _ := json.Marshal(strings.Join(textParts, "\n\n"))
		return joined, nil
	}
	if role != "user" {
		joined, _ := json.Marshal(strings.Join(textParts, "\n\n"))
		return joined, nil
	}
	if len(chatParts) == 0 {
		empty, _ := json.Marshal("")
		return empty, nil
	}
	return json.Marshal(chatParts)
}

func chatContentFromSingleResponsesPart(partType string, part map[string]json.RawMessage) (json.RawMessage, error) {
	switch partType {
	case "input_image", "image_url":
		imageURL := rawString(part["image_url"])
		if imageURL == "" {
			imageURL = rawNestedString(part["image_url"], "url")
		}
		return json.Marshal([]ChatContentPart{{
			Type:     "image_url",
			ImageURL: &ChatImageURL{URL: imageURL},
		}})
	default:
		return json.Marshal(rawString(part["text"]))
	}
}

func responsesToolsToChatTools(tools []ResponsesTool) []ChatTool {
	out := make([]ChatTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		out = append(out, ChatTool{
			Type: "function",
			Function: &ChatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
				Strict:      tool.Strict,
			},
		})
	}
	return out
}

func responsesToolChoiceToChatToolChoice(raw json.RawMessage) json.RawMessage {
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(raw, &choice); err != nil {
		return raw
	}
	if rawString(choice["type"]) != "function" {
		return raw
	}
	name := rawString(choice["name"])
	if name == "" {
		name = rawNestedString(choice["function"], "name")
	}
	if name == "" {
		return raw
	}
	out, err := json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]string{
			"name": name,
		},
	})
	if err != nil {
		return raw
	}
	return out
}

// ChatCompletionsResponseToResponses converts a non-streaming Chat Completions
// response into a Responses API response.
func ChatCompletionsResponseToResponses(resp *ChatCompletionsResponse, model string) *ResponsesResponse {
	id := ""
	if resp != nil {
		id = resp.ID
	}
	if id == "" {
		id = generateResponsesID()
	}

	out := &ResponsesResponse{
		ID:        id,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Model:     model,
		Status:    "completed",
		Usage:     &ResponsesUsage{},
	}
	if resp == nil {
		out.Output = []ResponsesOutput{emptyResponsesMessageOutput()}
		return out
	}
	if resp.Created > 0 {
		out.CreatedAt = resp.Created
	}
	if out.Model == "" {
		out.Model = resp.Model
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		out.Output = chatMessageToResponsesOutput(choice.Message)
		if choice.FinishReason == "length" {
			out.Status = "incomplete"
			out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	if len(out.Output) == 0 {
		out.Output = []ResponsesOutput{emptyResponsesMessageOutput()}
	}
	if resp.Usage != nil {
		out.Usage = ChatUsageToResponsesUsage(resp.Usage)
	}
	out.Usage = NormalizeResponsesUsage(out.Usage)
	return out
}

func chatMessageToResponsesOutput(message ChatMessage) []ResponsesOutput {
	var outputs []ResponsesOutput
	if message.ReasoningContent != "" {
		outputs = append(outputs, ResponsesOutput{
			Type: "reasoning",
			ID:   generateItemID(),
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: message.ReasoningContent,
			}},
		})
	}

	text := chatMessageContentText(message.Content)
	if text != "" || len(message.ToolCalls) == 0 {
		outputs = append(outputs, ResponsesOutput{
			Type: "message",
			ID:   generateItemID(),
			Role: "assistant",
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: text,
			}},
			Status: "completed",
		})
	}

	for _, toolCall := range message.ToolCalls {
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		outputs = append(outputs, ResponsesOutput{
			Type:      "function_call",
			ID:        generateItemID(),
			CallID:    toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: arguments,
			Status:    "completed",
		})
	}

	return outputs
}

func emptyResponsesMessageOutput() ResponsesOutput {
	return ResponsesOutput{
		Type:    "message",
		ID:      generateItemID(),
		Role:    "assistant",
		Content: []ResponsesContentPart{{Type: "output_text", Text: ""}},
		Status:  "completed",
	}
}

func chatMessageContentText(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}

// ChatUsageToResponsesUsage converts Chat Completions token usage to Responses
// usage shape.
func ChatUsageToResponsesUsage(usage *ChatUsage) *ResponsesUsage {
	if usage == nil {
		return nil
	}
	out := &ResponsesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
		out.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
		}
	}
	return out
}

// ChatCompletionsToResponsesStreamState tracks state while converting Chat
// Completions SSE chunks into Responses SSE events.
type ChatCompletionsToResponsesStreamState struct {
	ResponseID     string
	Model          string
	Created        int64
	SequenceNumber int
	CreatedSent    bool
	CompletedSent  bool

	messageItemID      string
	messageOutputIndex int
	messageDone        bool
	reasoningItemID    string
	reasoningOutputIdx int
	reasoningDone      bool
	nextOutputIndex    int
	text               strings.Builder
	reasoning          strings.Builder
	toolCalls          map[int]*chatToResponsesStreamTool
	toolCallIDs        map[string]int
	unindexedTools     map[int]int
	nextSyntheticIndex int
	outputOrder        []chatToResponsesOutputRef

	FinishReason string
	Usage        *ResponsesUsage
}

type chatToResponsesStreamTool struct {
	ChatIndex   int
	OutputIndex int
	ItemID      string
	CallID      string
	SyntheticID bool
	Name        string
	Arguments   string
	Done        bool
}

type chatToResponsesOutputRef struct {
	Kind      string
	ToolIndex int
}

// NewChatCompletionsToResponsesStreamState returns an initialized stream state.
func NewChatCompletionsToResponsesStreamState(model string) *ChatCompletionsToResponsesStreamState {
	return &ChatCompletionsToResponsesStreamState{
		ResponseID:         generateResponsesID(),
		Model:              model,
		Created:            time.Now().Unix(),
		messageOutputIndex: -1,
		reasoningOutputIdx: -1,
		toolCalls:          make(map[int]*chatToResponsesStreamTool),
		toolCallIDs:        make(map[string]int),
		unindexedTools:     make(map[int]int),
		Usage:              &ResponsesUsage{},
	}
}

// ChatCompletionsChunkToResponsesEvents converts one Chat Completions stream
// chunk into zero or more Responses stream events.
func ChatCompletionsChunkToResponsesEvents(
	chunk *ChatCompletionsChunk,
	state *ChatCompletionsToResponsesStreamState,
) []ResponsesStreamEvent {
	if chunk == nil || state == nil {
		return nil
	}
	// Keep the generated resp_ ID stable. Chat Completions IDs commonly use a
	// chatcmpl_ prefix and must not replace an ID already exposed in
	// response.created.
	if state.ResponseID == "" {
		state.ResponseID = generateResponsesID()
	}
	if !state.CreatedSent && chunk.Created > 0 {
		state.Created = chunk.Created
	}
	if !state.CreatedSent && state.Model == "" && chunk.Model != "" {
		state.Model = chunk.Model
	}
	if chunk.Usage != nil {
		state.Usage = ChatUsageToResponsesUsage(chunk.Usage)
	}

	var events []ResponsesStreamEvent
	events = append(events, ensureChatToResponsesCreated(state)...)

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			events = append(events, ensureChatToResponsesMessageItem(state)...)
			_, _ = state.text.WriteString(*choice.Delta.Content)
			events = append(events, chatToResponsesEvent(state, "response.output_text.delta", &ResponsesStreamEvent{
				OutputIndex:  state.messageOutputIndex,
				ContentIndex: 0,
				Delta:        *choice.Delta.Content,
				ItemID:       state.messageItemID,
			}))
		}
		if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			events = append(events, ensureChatToResponsesReasoningItem(state)...)
			_, _ = state.reasoning.WriteString(*choice.Delta.ReasoningContent)
			events = append(events, chatToResponsesEvent(state, "response.reasoning_summary_text.delta", &ResponsesStreamEvent{
				OutputIndex:  state.reasoningOutputIdx,
				SummaryIndex: 0,
				Delta:        *choice.Delta.ReasoningContent,
				ItemID:       state.reasoningItemID,
			}))
		}
		for ordinal, toolCall := range choice.Delta.ToolCalls {
			idx := resolveChatToResponsesToolIndex(state, ordinal, toolCall)
			stored := ensureChatToResponsesTool(state, idx, toolCall)
			mergeChatToResponsesToolDelta(state, stored, toolCall)
		}
		// Do not expose a function_call item until its upstream stream is
		// complete. Some compatible routers split function names ("list" +
		// "_dir") or repeat cumulative argument snapshots. Grok Build binds the
		// tool implementation from output_item.added and will classify a partial
		// name as an unknown/"Other" tool for the rest of the session.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			state.FinishReason = *choice.FinishReason
			events = append(events, finalizeChatToResponsesOutputItems(state)...)
		}
	}

	return events
}

// FinalizeChatCompletionsResponsesStream emits terminal Responses events.
func FinalizeChatCompletionsResponsesStream(state *ChatCompletionsToResponsesStreamState) []ResponsesStreamEvent {
	if state == nil || state.CompletedSent {
		return nil
	}
	var events []ResponsesStreamEvent
	events = append(events, ensureChatToResponsesCreated(state)...)
	if len(state.outputOrder) == 0 {
		events = append(events, ensureChatToResponsesMessageItem(state)...)
	}
	events = append(events, finalizeChatToResponsesOutputItems(state)...)

	status, incompleteDetails := state.responseStatus()
	terminalType := "response.completed"
	if status == "incomplete" {
		terminalType = "response.incomplete"
	}

	state.CompletedSent = true
	events = append(events, chatToResponsesEvent(state, terminalType, &ResponsesStreamEvent{
		Response: &ResponsesResponse{
			ID:                state.ResponseID,
			Object:            "response",
			CreatedAt:         state.Created,
			Model:             state.Model,
			Status:            status,
			Output:            state.chatOutput(),
			Usage:             NormalizeResponsesUsage(state.Usage),
			IncompleteDetails: incompleteDetails,
		},
	}))
	return events
}

func ensureChatToResponsesCreated(state *ChatCompletionsToResponsesStreamState) []ResponsesStreamEvent {
	if state.CreatedSent {
		return nil
	}
	state.CreatedSent = true
	return []ResponsesStreamEvent{chatToResponsesEvent(state, "response.created", &ResponsesStreamEvent{
		Response: &ResponsesResponse{
			ID:        state.ResponseID,
			Object:    "response",
			CreatedAt: state.Created,
			Model:     state.Model,
			Status:    "in_progress",
			Output:    []ResponsesOutput{},
		},
	})}
}

func ensureChatToResponsesMessageItem(state *ChatCompletionsToResponsesStreamState) []ResponsesStreamEvent {
	if state.messageItemID != "" {
		return nil
	}
	state.messageItemID = generateItemID()
	state.messageOutputIndex = state.nextIndex("message", -1)
	return []ResponsesStreamEvent{chatToResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
		OutputIndex: state.messageOutputIndex,
		Item: &ResponsesOutput{
			Type:    "message",
			ID:      state.messageItemID,
			Role:    "assistant",
			Content: []ResponsesContentPart{},
			Status:  "in_progress",
		},
	})}
}

func ensureChatToResponsesReasoningItem(state *ChatCompletionsToResponsesStreamState) []ResponsesStreamEvent {
	if state.reasoningItemID != "" {
		return nil
	}
	state.reasoningItemID = generateItemID()
	state.reasoningOutputIdx = state.nextIndex("reasoning", -1)
	return []ResponsesStreamEvent{chatToResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
		OutputIndex: state.reasoningOutputIdx,
		Item: &ResponsesOutput{
			Type:    "reasoning",
			ID:      state.reasoningItemID,
			Status:  "in_progress",
			Summary: []ResponsesSummary{},
		},
	})}
}

func ensureChatToResponsesTool(
	state *ChatCompletionsToResponsesStreamState,
	chatIndex int,
	toolCall ChatToolCall,
) *chatToResponsesStreamTool {
	if stored := state.toolCalls[chatIndex]; stored != nil {
		return stored
	}

	callID := strings.TrimSpace(toolCall.ID)
	syntheticID := callID == ""
	if callID == "" {
		callID = "call_" + strings.TrimPrefix(generateItemID(), "item_")
	}
	stored := &chatToResponsesStreamTool{
		ChatIndex:   chatIndex,
		OutputIndex: state.nextIndex("tool", chatIndex),
		ItemID:      generateItemID(),
		CallID:      callID,
		SyntheticID: syntheticID,
	}
	state.toolCalls[chatIndex] = stored
	return stored
}

func resolveChatToResponsesToolIndex(
	state *ChatCompletionsToResponsesStreamState,
	ordinal int,
	toolCall ChatToolCall,
) int {
	if toolCall.Index != nil {
		idx := *toolCall.Index
		state.unindexedTools[ordinal] = idx
		if callID := strings.TrimSpace(toolCall.ID); callID != "" {
			state.toolCallIDs[callID] = idx
		}
		return idx
	}
	if callID := strings.TrimSpace(toolCall.ID); callID != "" {
		if idx, ok := state.toolCallIDs[callID]; ok {
			return idx
		}
	}
	if idx, ok := state.unindexedTools[ordinal]; ok {
		if callID := strings.TrimSpace(toolCall.ID); callID == "" || state.toolCalls[idx] == nil || state.toolCalls[idx].SyntheticID {
			return idx
		}
	}

	idx := state.nextSyntheticIndex
	for state.toolCalls[idx] != nil {
		idx++
	}
	state.nextSyntheticIndex = idx + 1
	state.unindexedTools[ordinal] = idx
	if callID := strings.TrimSpace(toolCall.ID); callID != "" {
		state.toolCallIDs[callID] = idx
	}
	return idx
}

func mergeChatToResponsesToolDelta(
	state *ChatCompletionsToResponsesStreamState,
	stored *chatToResponsesStreamTool,
	toolCall ChatToolCall,
) {
	if state == nil || stored == nil {
		return
	}
	if callID := strings.TrimSpace(toolCall.ID); callID != "" {
		state.toolCallIDs[callID] = stored.ChatIndex
		if stored.SyntheticID {
			stored.CallID = callID
			stored.SyntheticID = false
		}
	}
	stored.Name = mergeChatStreamFragment(stored.Name, strings.TrimSpace(toolCall.Function.Name))
	stored.Arguments = mergeChatStreamFragment(stored.Arguments, toolCall.Function.Arguments)
}

func mergeChatStreamFragment(current string, incoming string) string {
	if incoming == "" {
		return current
	}
	if current == "" {
		return incoming
	}
	if incoming == current || strings.HasPrefix(current, incoming) {
		return current
	}
	if strings.HasPrefix(incoming, current) {
		return incoming
	}
	return current + incoming
}

func addChatToResponsesToolItem(
	state *ChatCompletionsToResponsesStreamState,
	stored *chatToResponsesStreamTool,
) []ResponsesStreamEvent {
	return []ResponsesStreamEvent{chatToResponsesEvent(state, "response.output_item.added", &ResponsesStreamEvent{
		OutputIndex: stored.OutputIndex,
		Item: &ResponsesOutput{
			Type:      "function_call",
			ID:        stored.ItemID,
			CallID:    stored.CallID,
			Name:      stored.Name,
			Arguments: "",
			Status:    "in_progress",
		},
	})}
}

func finalizeChatToResponsesOutputItems(state *ChatCompletionsToResponsesStreamState) []ResponsesStreamEvent {
	if state == nil {
		return nil
	}
	var events []ResponsesStreamEvent
	outputStatus := state.outputStatus()
	for _, ref := range state.outputOrder {
		switch ref.Kind {
		case "message":
			if state.messageDone {
				continue
			}
			state.messageDone = true
			events = append(events, chatToResponsesEvent(state, "response.output_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.messageOutputIndex,
				ContentIndex: 0,
				Text:         state.text.String(),
				ItemID:       state.messageItemID,
			}))
			events = append(events, chatToResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
				OutputIndex: state.messageOutputIndex,
				Item:        state.messageOutput(outputStatus),
			}))
		case "reasoning":
			if state.reasoningDone {
				continue
			}
			state.reasoningDone = true
			events = append(events, chatToResponsesEvent(state, "response.reasoning_summary_text.done", &ResponsesStreamEvent{
				OutputIndex:  state.reasoningOutputIdx,
				SummaryIndex: 0,
				Text:         state.reasoning.String(),
				ItemID:       state.reasoningItemID,
			}))
			events = append(events, chatToResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
				OutputIndex: state.reasoningOutputIdx,
				Item:        state.reasoningOutput(outputStatus),
			}))
		case "tool":
			tool := state.toolCalls[ref.ToolIndex]
			if tool == nil || tool.Done {
				continue
			}
			tool.Done = true
			arguments := normalizedToolArguments(tool.Arguments)
			events = append(events, addChatToResponsesToolItem(state, tool)...)
			if tool.Arguments != "" {
				events = append(events, chatToResponsesEvent(state, "response.function_call_arguments.delta", &ResponsesStreamEvent{
					OutputIndex: tool.OutputIndex,
					Delta:       tool.Arguments,
					ItemID:      tool.ItemID,
				}))
			}
			events = append(events, chatToResponsesEvent(state, "response.function_call_arguments.done", &ResponsesStreamEvent{
				OutputIndex: tool.OutputIndex,
				ItemID:      tool.ItemID,
				Name:        tool.Name,
				Arguments:   arguments,
			}))
			events = append(events, chatToResponsesEvent(state, "response.output_item.done", &ResponsesStreamEvent{
				OutputIndex: tool.OutputIndex,
				Item:        state.toolOutput(tool, outputStatus),
			}))
		}
	}
	return events
}

func (state *ChatCompletionsToResponsesStreamState) chatOutput() []ResponsesOutput {
	outputs := make([]ResponsesOutput, 0, len(state.outputOrder))
	outputStatus := state.outputStatus()
	for _, ref := range state.outputOrder {
		switch ref.Kind {
		case "message":
			outputs = append(outputs, *state.messageOutput(outputStatus))
		case "reasoning":
			outputs = append(outputs, *state.reasoningOutput(outputStatus))
		case "tool":
			if tool := state.toolCalls[ref.ToolIndex]; tool != nil {
				outputs = append(outputs, *state.toolOutput(tool, outputStatus))
			}
		}
	}
	return outputs
}

func (state *ChatCompletionsToResponsesStreamState) messageOutput(status string) *ResponsesOutput {
	return &ResponsesOutput{
		Type: "message",
		ID:   state.messageItemID,
		Role: "assistant",
		Content: []ResponsesContentPart{{
			Type: "output_text",
			Text: state.text.String(),
		}},
		Status: status,
	}
}

func (state *ChatCompletionsToResponsesStreamState) reasoningOutput(status string) *ResponsesOutput {
	return &ResponsesOutput{
		Type:   "reasoning",
		ID:     state.reasoningItemID,
		Status: status,
		Summary: []ResponsesSummary{{
			Type: "summary_text",
			Text: state.reasoning.String(),
		}},
	}
}

func (state *ChatCompletionsToResponsesStreamState) toolOutput(tool *chatToResponsesStreamTool, status string) *ResponsesOutput {
	return &ResponsesOutput{
		Type:      "function_call",
		ID:        tool.ItemID,
		CallID:    tool.CallID,
		Name:      tool.Name,
		Arguments: normalizedToolArguments(tool.Arguments),
		Status:    status,
	}
}

func (state *ChatCompletionsToResponsesStreamState) nextIndex(kind string, toolIndex int) int {
	index := state.nextOutputIndex
	state.nextOutputIndex++
	state.outputOrder = append(state.outputOrder, chatToResponsesOutputRef{Kind: kind, ToolIndex: toolIndex})
	return index
}

func (state *ChatCompletionsToResponsesStreamState) responseStatus() (string, *ResponsesIncompleteDetails) {
	switch state.FinishReason {
	case "length":
		return "incomplete", &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		return "incomplete", &ResponsesIncompleteDetails{Reason: "content_filter"}
	default:
		return "completed", nil
	}
}

func (state *ChatCompletionsToResponsesStreamState) outputStatus() string {
	status, _ := state.responseStatus()
	return status
}

func normalizedToolArguments(arguments string) string {
	if strings.TrimSpace(arguments) == "" {
		return "{}"
	}
	return arguments
}

func chatToResponsesEvent(
	state *ChatCompletionsToResponsesStreamState,
	eventType string,
	template *ResponsesStreamEvent,
) ResponsesStreamEvent {
	seq := state.SequenceNumber
	state.SequenceNumber++
	evt := *template
	evt.Type = eventType
	evt.SequenceNumber = seq
	return evt
}

func rawString(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func rawNestedString(raw json.RawMessage, key string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return rawString(obj[key])
}

func bytesTrimSpace(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}
