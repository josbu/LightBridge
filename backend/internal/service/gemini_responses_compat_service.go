package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// ForwardAsResponses serves OpenAI Responses clients through Gemini accounts.
// It reuses the stable Responses <-> Anthropic and Anthropic <-> Gemini bridges
// instead of introducing a parallel Gemini-specific canonical converter.
func (s *GeminiMessagesCompatService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Failed to parse request body"}})
		return nil, err
	}
	if strings.TrimSpace(responsesReq.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "model is required"}})
		return nil, fmt.Errorf("model is required")
	}

	originalModel := responsesReq.Model
	clientStream := responsesReq.Stream
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&responsesReq)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return nil, err
	}
	anthropicReq.Stream = clientStream

	claudeBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal responses compat request: %w", err)
	}

	capture, rec := cloneGinContextForProtocolCapture(c, ctx, "/v1/messages", claudeBody)
	result, err := s.Forward(ctx, capture, account, claudeBody)
	if err != nil {
		copyCapturedResponse(c, rec)
		return result, err
	}

	if clientStream {
		if err := writeCapturedAnthropicStreamAsResponses(c, rec.Body.Bytes(), originalModel); err != nil {
			return result, err
		}
	} else {
		if err := writeCapturedAnthropicJSONAsResponses(c, rec, originalModel); err != nil {
			return result, err
		}
	}
	return result, nil
}

func cloneGinContextForProtocolCapture(parent *gin.Context, ctx context.Context, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	capture, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
	if parent != nil {
		if parent.Request != nil {
			req.Header = parent.Request.Header.Clone()
		}
		if len(parent.Keys) > 0 {
			capture.Keys = make(map[string]any, len(parent.Keys))
			for k, v := range parent.Keys {
				capture.Keys[k] = v
			}
		}
	}
	req.Header.Set("Content-Type", "application/json")
	capture.Request = req
	return capture, rec
}

func copyCapturedResponse(c *gin.Context, rec *httptest.ResponseRecorder) {
	if c == nil || rec == nil {
		return
	}
	for key, values := range rec.Header() {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	status := rec.Code
	if status == 0 {
		status = http.StatusBadGateway
	}
	contentType := rec.Header().Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(status, contentType, rec.Body.Bytes())
}

func writeCapturedAnthropicJSONAsResponses(c *gin.Context, rec *httptest.ResponseRecorder, originalModel string) error {
	var anthropicResp apicompat.AnthropicResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &anthropicResp); err != nil {
		copyCapturedResponse(c, rec)
		return err
	}
	responsesResp := apicompat.AnthropicToResponsesResponse(&anthropicResp)
	if responsesResp.Model == "" {
		responsesResp.Model = originalModel
	}
	if requestID := rec.Header().Get("x-request-id"); requestID != "" {
		c.Header("x-request-id", requestID)
	}
	c.JSON(http.StatusOK, responsesResp)
	return nil
}

func writeCapturedAnthropicStreamAsResponses(c *gin.Context, data []byte, originalModel string) error {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	state := apicompat.NewAnthropicEventToResponsesState()
	state.Model = originalModel
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)

	eventName := ""
	var dataLines []string
	flushEvent := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if payload == "" || payload == "[DONE]" {
			eventName = ""
			return nil
		}
		var evt apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return err
		}
		if evt.Type == "" {
			evt.Type = eventName
		}
		for _, responsesEvt := range apicompat.AnthropicEventToResponsesEvents(&evt, state) {
			line, err := apicompat.ResponsesEventToSSE(responsesEvt)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(c.Writer, line); err != nil {
				return err
			}
		}
		eventName = ""
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flushEvent(); err != nil {
		return err
	}
	for _, responsesEvt := range apicompat.FinalizeAnthropicResponsesStream(state) {
		line, err := apicompat.ResponsesEventToSSE(responsesEvt)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(c.Writer, line); err != nil {
			return err
		}
	}
	return nil
}
