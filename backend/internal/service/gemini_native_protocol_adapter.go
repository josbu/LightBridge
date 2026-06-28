package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/antigravity"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// GeminiGenerateContentToAnthropicMessages converts a Gemini generateContent
// request into the Anthropic Messages shape used by LightBridge's existing
// canonical bridges. Provider-specific Gemini fields that have no equivalent
// are intentionally dropped by this minimal adapter.
func GeminiGenerateContentToAnthropicMessages(body []byte, model string, stream bool) ([]byte, error) {
	var req antigravity.GeminiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	out := apicompat.AnthropicRequest{
		Model:     model,
		Messages:  make([]apicompat.AnthropicMessage, 0, len(req.Contents)),
		Stream:    stream,
		MaxTokens: 8192,
	}
	if req.GenerationConfig != nil {
		if req.GenerationConfig.MaxOutputTokens > 0 {
			out.MaxTokens = req.GenerationConfig.MaxOutputTokens
		}
		out.Temperature = req.GenerationConfig.Temperature
		out.TopP = req.GenerationConfig.TopP
		out.StopSeqs = req.GenerationConfig.StopSequences
		if req.GenerationConfig.ThinkingConfig != nil && req.GenerationConfig.ThinkingConfig.IncludeThoughts {
			out.Thinking = &apicompat.AnthropicThinking{
				Type:         "enabled",
				BudgetTokens: req.GenerationConfig.ThinkingConfig.ThinkingBudget,
			}
		}
	}
	if req.SystemInstruction != nil {
		if text := geminiContentText(*req.SystemInstruction); strings.TrimSpace(text) != "" {
			out.System, _ = json.Marshal(text)
		}
	}
	if len(req.Tools) > 0 {
		out.Tools = geminiToolsToAnthropic(req.Tools)
	}

	for _, content := range req.Contents {
		blocks := geminiPartsToAnthropicBlocks(content.Parts)
		if len(blocks) == 0 {
			continue
		}
		raw, _ := json.Marshal(blocks)
		role := "user"
		if strings.EqualFold(content.Role, "model") {
			role = "assistant"
		}
		out.Messages = append(out.Messages, apicompat.AnthropicMessage{Role: role, Content: raw})
	}
	if len(out.Messages) == 0 {
		raw, _ := json.Marshal([]apicompat.AnthropicContentBlock{{Type: "text", Text: ""}})
		out.Messages = append(out.Messages, apicompat.AnthropicMessage{Role: "user", Content: raw})
	}

	return json.Marshal(out)
}

func geminiContentText(content antigravity.GeminiContent) string {
	var parts []string
	for _, part := range content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func geminiPartsToAnthropicBlocks(parts []antigravity.GeminiPart) []apicompat.AnthropicContentBlock {
	out := make([]apicompat.AnthropicContentBlock, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != "":
			if part.Thought {
				out = append(out, apicompat.AnthropicContentBlock{Type: "thinking", Thinking: part.Text})
			} else {
				out = append(out, apicompat.AnthropicContentBlock{Type: "text", Text: part.Text})
			}
		case part.InlineData != nil:
			out = append(out, apicompat.AnthropicContentBlock{
				Type: "image",
				Source: &apicompat.AnthropicImageSource{
					Type:      "base64",
					MediaType: part.InlineData.MimeType,
					Data:      part.InlineData.Data,
				},
			})
		case part.FunctionCall != nil:
			input := json.RawMessage("{}")
			if part.FunctionCall.Args != nil {
				if b, err := json.Marshal(part.FunctionCall.Args); err == nil {
					input = b
				}
			}
			id := strings.TrimSpace(part.FunctionCall.ID)
			if id == "" {
				id = "call_" + randomHex(8)
			}
			out = append(out, apicompat.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  part.FunctionCall.Name,
				Input: input,
			})
		case part.FunctionResponse != nil:
			content := json.RawMessage(`"{}"`)
			if part.FunctionResponse.Response != nil {
				if b, err := json.Marshal(part.FunctionResponse.Response); err == nil {
					content = b
				}
			}
			id := strings.TrimSpace(part.FunctionResponse.ID)
			if id == "" {
				id = strings.TrimSpace(part.FunctionResponse.Name)
			}
			out = append(out, apicompat.AnthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: id,
				Content:   content,
			})
		}
	}
	return out
}

func geminiToolsToAnthropic(tools []antigravity.GeminiToolDeclaration) []apicompat.AnthropicTool {
	var out []apicompat.AnthropicTool
	for _, tool := range tools {
		for _, fn := range tool.FunctionDeclarations {
			params := json.RawMessage(`{"type":"object","properties":{}}`)
			if fn.Parameters != nil {
				if b, err := json.Marshal(fn.Parameters); err == nil {
					params = b
				}
			}
			out = append(out, apicompat.AnthropicTool{
				Name:        fn.Name,
				Description: fn.Description,
				InputSchema: params,
			})
		}
	}
	return out
}

// WriteCapturedAnthropicAsGemini converts a captured Anthropic Messages
// response into Gemini generateContent response format.
func WriteCapturedAnthropicAsGemini(c *gin.Context, status int, headers http.Header, body []byte, stream bool, model string) error {
	if status == 0 {
		status = http.StatusOK
	}
	if status >= 400 {
		c.Data(status, "application/json", body)
		return nil
	}
	if requestID := headers.Get("x-request-id"); requestID != "" {
		c.Header("x-request-id", requestID)
	}
	if stream {
		return writeCapturedAnthropicStreamAsGemini(c, body, model)
	}
	return writeCapturedAnthropicJSONAsGemini(c, body, model)
}

func writeCapturedAnthropicJSONAsGemini(c *gin.Context, body []byte, model string) error {
	var resp apicompat.AnthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}
	geminiResp := anthropicResponseToGemini(&resp, model)
	c.JSON(http.StatusOK, geminiResp)
	return nil
}

func anthropicResponseToGemini(resp *apicompat.AnthropicResponse, model string) map[string]any {
	parts := make([]map[string]any, 0)
	if resp != nil {
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				parts = append(parts, map[string]any{"text": block.Text})
			case "thinking":
				parts = append(parts, map[string]any{"text": block.Thinking, "thought": true})
			case "image":
				if block.Source != nil {
					parts = append(parts, map[string]any{
						"inlineData": map[string]any{
							"mimeType": block.Source.MediaType,
							"data":     block.Source.Data,
						},
					})
				}
			case "tool_use":
				var args any = map[string]any{}
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &args)
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": block.Name,
						"args": args,
						"id":   block.ID,
					},
				})
			}
		}
	}
	if len(parts) == 0 {
		parts = append(parts, map[string]any{"text": ""})
	}
	finishReason := "STOP"
	if resp != nil {
		finishReason = anthropicStopReasonToGemini(resp.StopReason)
	}
	out := map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role":  "model",
					"parts": parts,
				},
				"finishReason": finishReason,
				"index":        0,
			},
		},
		"modelVersion": model,
	}
	if resp != nil {
		out["usageMetadata"] = map[string]any{
			"promptTokenCount":     resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens,
			"candidatesTokenCount": resp.Usage.OutputTokens,
			"totalTokenCount":      resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.OutputTokens,
		}
		if strings.TrimSpace(resp.ID) != "" {
			out["responseId"] = resp.ID
		}
	}
	return out
}

func writeCapturedAnthropicStreamAsGemini(c *gin.Context, data []byte, model string) error {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var evt apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return err
		}
		chunks := anthropicStreamEventToGeminiChunks(&evt, model)
		for _, chunk := range chunks {
			b, _ := json.Marshal(chunk)
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", b); err != nil {
				return err
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func anthropicStreamEventToGeminiChunks(evt *apicompat.AnthropicStreamEvent, model string) []map[string]any {
	if evt == nil {
		return nil
	}
	switch evt.Type {
	case "content_block_start":
		if evt.ContentBlock == nil {
			return nil
		}
		switch evt.ContentBlock.Type {
		case "text":
			if evt.ContentBlock.Text == "" {
				return nil
			}
			return []map[string]any{geminiTextStreamChunk(evt.ContentBlock.Text, "", model)}
		case "thinking":
			if evt.ContentBlock.Thinking == "" {
				return nil
			}
			chunk := geminiTextStreamChunk(evt.ContentBlock.Thinking, "", model)
			if candidates, ok := chunk["candidates"].([]any); ok && len(candidates) > 0 {
				if cand, ok := candidates[0].(map[string]any); ok {
					if content, ok := cand["content"].(map[string]any); ok {
						if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
							if part, ok := parts[0].(map[string]any); ok {
								part["thought"] = true
							}
						}
					}
				}
			}
			return []map[string]any{chunk}
		case "tool_use":
			var args any = map[string]any{}
			if len(evt.ContentBlock.Input) > 0 {
				_ = json.Unmarshal(evt.ContentBlock.Input, &args)
			}
			return []map[string]any{geminiFunctionCallStreamChunk(evt.ContentBlock.Name, evt.ContentBlock.ID, args, model)}
		}
	case "content_block_delta":
		if evt.Delta == nil {
			return nil
		}
		switch evt.Delta.Type {
		case "text_delta":
			if evt.Delta.Text == "" {
				return nil
			}
			return []map[string]any{geminiTextStreamChunk(evt.Delta.Text, "", model)}
		case "thinking_delta":
			if evt.Delta.Thinking == "" {
				return nil
			}
			return []map[string]any{geminiTextStreamChunk(evt.Delta.Thinking, "", model)}
		}
	case "message_delta":
		finish := ""
		if evt.Delta != nil {
			finish = anthropicStopReasonToGemini(evt.Delta.StopReason)
		}
		chunk := geminiTextStreamChunk("", finish, model)
		if evt.Usage != nil {
			chunk["usageMetadata"] = map[string]any{
				"promptTokenCount":     evt.Usage.InputTokens + evt.Usage.CacheReadInputTokens + evt.Usage.CacheCreationInputTokens,
				"candidatesTokenCount": evt.Usage.OutputTokens,
				"totalTokenCount":      evt.Usage.InputTokens + evt.Usage.CacheReadInputTokens + evt.Usage.CacheCreationInputTokens + evt.Usage.OutputTokens,
			}
		}
		return []map[string]any{chunk}
	}
	return nil
}

func geminiTextStreamChunk(text, finishReason, model string) map[string]any {
	candidate := map[string]any{
		"content": map[string]any{
			"role":  "model",
			"parts": []any{map[string]any{"text": text}},
		},
		"index": 0,
	}
	if finishReason != "" {
		candidate["finishReason"] = finishReason
	}
	return map[string]any{
		"candidates":   []any{candidate},
		"modelVersion": model,
	}
}

func geminiFunctionCallStreamChunk(name, id string, args any, model string) map[string]any {
	return map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role": "model",
					"parts": []any{map[string]any{
						"functionCall": map[string]any{
							"name": name,
							"args": args,
							"id":   id,
						},
					}},
				},
				"index": 0,
			},
		},
		"modelVersion": model,
	}
}

func anthropicStopReasonToGemini(reason string) string {
	switch strings.TrimSpace(reason) {
	case "max_tokens":
		return "MAX_TOKENS"
	case "stop_sequence", "end_turn", "":
		return "STOP"
	case "tool_use":
		return "STOP"
	default:
		return strings.ToUpper(reason)
	}
}
