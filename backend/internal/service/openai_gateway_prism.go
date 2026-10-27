package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/prismbridge"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Prism is a distinct upstream. Route before OAuth rewriting/token refresh and
// never return a failover error after a potentially executed browser turn.
func (s *OpenAIGatewayService) forwardPrism(ctx context.Context, c *gin.Context, account *Account, body []byte, protocol, defaultModel string) (*OpenAIForwardResult, error) {
	start := time.Now()
	beginUpstreamResponseModelObservation(c)
	ClearActualOpenAIUpstreamEndpoint(c)
	fail := func(status int, message string) (*OpenAIForwardResult, error) {
		if protocol == "messages" {
			writeAnthropicError(c, status, "prism_error", message)
		} else {
			c.JSON(status, gin.H{"error": gin.H{"type": "prism_error", "message": message}})
		}
		return nil, errors.New(message)
	}
	if !gjson.ValidBytes(body) {
		return fail(400, "Invalid JSON request")
	}
	if isOpenAIResponsesCompactPath(c) {
		return fail(400, "Prism does not support /responses/compact; use client-side compaction")
	}
	if gjson.GetBytes(body, "previous_response_id").String() != "" {
		return fail(400, "Prism requires replaying message history; previous_response_id is not supported")
	}
	if gjson.GetBytes(body, "background").Bool() {
		return fail(400, "Prism does not support background responses")
	}
	if gjson.GetBytes(body, "tools").IsArray() {
		for _, tool := range gjson.GetBytes(body, "tools").Array() {
			switch tool.Get("type").String() {
			case "image_generation", "web_search", "web_search_preview", "computer_use_preview":
				return fail(400, "Prism supports client-executed tools and image input; this hosted tool is not supported")
			}
		}
	}
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		return fail(400, "model is required")
	}
	if restriction := s.detectCodexClientRestriction(c, account, body); restriction.Enabled && !restriction.Matched {
		return fail(403, CodexClientRestrictionMessage(restriction))
	}
	model := resolveOpenAIForwardModel(account, originalModel, defaultModel)
	endpoint := "/v1/responses"
	if protocol == "chat" || protocol == "messages" {
		endpoint = "/v1/chat/completions"
	}
	if protocol == "messages" {
		var input apicompat.AnthropicRequest
		if json.Unmarshal(body, &input) != nil {
			return fail(400, "Invalid Messages request")
		}
		chat, err := apicompat.AnthropicToChatCompletionsRequest(&input)
		if err != nil {
			return fail(400, err.Error())
		}
		body, err = json.Marshal(chat)
		if err != nil {
			return fail(400, "Cannot convert Messages request")
		}
	}
	var err error
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "Cannot set Prism model")
	}
	body, _, err = ApplyOpenAIReasoningEffortPolicyFromContext(ctx, body)
	if err != nil {
		return fail(403, err.Error())
	}
	// Disable server-file interpretation in the worker. URLs must be supplied as
	// data URIs so browser-side image upload cannot access internal network hosts.
	if err := validatePrismImages(gjson.ParseBytes(body)); err != nil {
		return fail(400, err.Error())
	}
	cookie, err := normalizePrismCookie(account.GetCredential(prismCookieKey))
	if err != nil {
		return fail(400, err.Error())
	}
	proxy := ""
	if account.ProxyID != nil {
		if account.Proxy == nil {
			return fail(503, "Prism account proxy is unavailable")
		}
		proxy = account.Proxy.URL()
	}
	lease, err := prismbridge.Default.Acquire(ctx, account.ID, cookie, proxy)
	if err != nil {
		return fail(503, err.Error())
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	defer func() {
		if ctx.Err() != nil {
			lease.Abort()
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lease.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return fail(500, "Cannot create Prism request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+lease.Key)
	// Derive the tenant from the authenticated API key, never client-supplied
	// X-User-Id/user. Missing auth identity disables continuation in the worker.
	if keyID := getAPIKeyIDFromContext(c); keyID > 0 {
		req.Header.Set("X-User-Id", fmt.Sprintf("sub2api:%d:%d", account.ID, keyID))
	} else {
		for _, field := range []string{"user", "metadata.user", "metadata.user_id", "metadata.tenant"} {
			body, _ = sjson.DeleteBytes(body, field)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	SetOpsUpstreamModel(c, model)
	resp, err := prismbridge.Client.Do(req)
	if err != nil {
		lease.Abort()
		return fail(502, "Prism request failed or timed out; the turn may have executed and was not retried")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		message := gjson.GetBytes(payload, "error.message").String()
		if message == "" {
			message = "Prism returned an unsuccessful response"
		}
		return fail(resp.StatusCode, message)
	}
	return s.handlePrismResponse(c, resp, body, protocol, originalModel, model, start, lease.Abort)
}

func (s *OpenAIGatewayService) handlePrismResponse(c *gin.Context, resp *http.Response, body []byte, protocol, originalModel, model string, start time.Time, abort func()) (*OpenAIForwardResult, error) {
	endpoint := "/v1/responses"
	if protocol != "responses" {
		endpoint = "/v1/chat/completions"
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: endpoint, Stream: stream}
	if effort := gjson.GetBytes(body, "reasoning.effort").String(); effort != "" {
		result.ReasoningEffort = &effort
	} else if effort = gjson.GetBytes(body, "reasoning_effort").String(); effort != "" {
		result.ReasoningEffort = &effort
	}
	defer func() { result.Duration = time.Since(start) }()
	if !stream {
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024+1))
		if err != nil || len(payload) > 32*1024*1024 || !gjson.ValidBytes(payload) {
			abort()
			c.JSON(502, gin.H{"error": gin.H{"type": "prism_error", "message": "Invalid or truncated Prism response"}})
			return nil, errors.New("invalid or truncated Prism response")
		}
		inspectPrismPayload(result, payload, protocol)
		if protocol == "messages" {
			if gjson.GetBytes(payload, "error").IsObject() {
				writeAnthropicError(c, 502, "prism_error", gjson.GetBytes(payload, "error.message").String())
				return result, errors.New("Prism turn failed")
			}
			var chat apicompat.ChatCompletionsResponse
			if err := json.Unmarshal(payload, &chat); err != nil {
				return result, err
			}
			c.JSON(200, apicompat.ChatCompletionsResponseToAnthropic(&chat, originalModel))
			return result, nil
		}
		c.Data(resp.StatusCode, "application/json", payload)
		if gjson.GetBytes(payload, "error").Exists() || gjson.GetBytes(payload, "status").String() == "failed" {
			return result, errors.New("Prism turn failed")
		}
		return result, nil
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	terminal, failed := false, false
	state := apicompat.NewChatCompletionsToAnthropicStreamState(originalModel)
	emit := func(events []apicompat.AnthropicStreamEvent) error {
		for _, event := range events {
			frame, err := apicompat.ResponsesAnthropicEventToSSE(event)
			if err != nil {
				return err
			}
			if _, err = c.Writer.Write([]byte(frame)); err != nil {
				return err
			}
		}
		c.Writer.Flush()
		return nil
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.HasPrefix(line, []byte("data:")) {
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if bytes.Equal(payload, []byte("[DONE]")) {
				if protocol == "chat" || protocol == "messages" {
					terminal = true
				}
			} else if gjson.ValidBytes(payload) {
				inspectPrismPayload(result, payload, protocol)
				typ := gjson.GetBytes(payload, "type").String()
				if typ == "response.completed" || typ == "response.done" {
					terminal = true
				}
				if typ == "response.failed" || typ == "error" || gjson.GetBytes(payload, "error").Exists() {
					failed = true
				}
				if protocol == "messages" && !failed {
					var chunk apicompat.ChatCompletionsChunk
					if err := json.Unmarshal(payload, &chunk); err != nil {
						abort()
						return result, err
					}
					if err := emit(apicompat.ChatCompletionsChunkToAnthropicEvents(&chunk, state)); err != nil {
						abort()
						result.ClientDisconnect = true
						return result, err
					}
				}
				if result.FirstTokenMs == nil && (gjson.GetBytes(payload, "delta").String() != "" || gjson.GetBytes(payload, "choices.0.delta.content").String() != "" || gjson.GetBytes(payload, "choices.0.delta.tool_calls").Exists()) {
					ms := int(time.Since(start).Milliseconds())
					result.FirstTokenMs = &ms
				}
			}
		}
		if protocol == "messages" {
			if bytes.HasPrefix(line, []byte(":")) {
				if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
					abort()
					result.ClientDisconnect = true
					return result, err
				}
				c.Writer.Flush()
			}
			continue
		}
		if _, err := c.Writer.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			abort()
			result.ClientDisconnect = true
			return result, err
		}
		if len(line) == 0 {
			c.Writer.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		abort()
		return result, fmt.Errorf("Prism stream interrupted: %w", err)
	}
	if failed || !terminal {
		abort()
		if protocol == "messages" {
			_, _ = c.Writer.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"prism_error\",\"message\":\"Prism turn failed or ended without completion\"}}\n\n"))
			c.Writer.Flush()
		}
		return result, errors.New("Prism stream failed or ended without completion; not retried")
	}
	if protocol == "messages" {
		if err := emit(apicompat.FinalizeChatCompletionsAnthropicStream(state)); err != nil {
			result.ClientDisconnect = true
			return result, err
		}
	}
	return result, nil
}

func validatePrismImages(value gjson.Result) error {
	if value.IsObject() {
		typ := value.Get("type").String()
		if typ == "input_image" || typ == "image_url" {
			url := value.Get("image_url").String()
			if value.Get("image_url").IsObject() {
				url = value.Get("image_url.url").String()
			}
			if !strings.HasPrefix(url, "data:image/") {
				return errors.New("Prism image input requires a data:image/... base64 URL")
			}
		}
	}
	var err error
	if value.IsObject() || value.IsArray() {
		value.ForEach(func(_, child gjson.Result) bool { err = validatePrismImages(child); return err == nil })
	}
	return err
}

func inspectPrismPayload(result *OpenAIForwardResult, payload []byte, protocol string) {
	value := gjson.ParseBytes(payload)
	if value.Get("response").IsObject() {
		value = value.Get("response")
	}
	if model := value.Get("model").String(); model != "" {
		result.UpstreamResponseModel = model
	}
	if id := value.Get("id").String(); id != "" {
		result.ResponseID = id
	}
	usage := value.Get("usage")
	if !usage.IsObject() {
		return
	}
	if protocol == "chat" || protocol == "messages" {
		result.Usage.InputTokens = int(usage.Get("prompt_tokens").Int())
		result.Usage.OutputTokens = int(usage.Get("completion_tokens").Int())
		result.Usage.CacheReadInputTokens = int(usage.Get("prompt_tokens_details.cached_tokens").Int())
	} else {
		result.Usage.InputTokens = int(usage.Get("input_tokens").Int())
		result.Usage.OutputTokens = int(usage.Get("output_tokens").Int())
		result.Usage.CacheReadInputTokens = int(usage.Get("input_tokens_details.cached_tokens").Int())
	}
}
