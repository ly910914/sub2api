package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// UsesOAuthChatBridge reproduces an API-key Chat relay backed by an OAuth
// account, without a second HTTP request, scheduler slot or usage record.
// Probe results belong to API-key upstreams and must not enable this bridge.
func (a *Account) UsesOAuthChatBridge() bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth &&
		a.Extra[openai_compat.ExtraKeyResponsesMode] == string(openai_compat.ResponsesSupportModeForceChatCompletions)
}

// oauthChatBridgeWriter consumes the existing OAuth Chat handler's output
// synchronously. No goroutine shares a gin.Context, and only one SSE frame is
// buffered at a time. The underlying handler retains ownership of OAuth
// credentials, retries, rate-limit snapshots and upstream usage accounting.
type oauthChatBridgeWriter struct {
	gin.ResponseWriter
	nativeResponse json.RawMessage
	stream         bool
	anthropic      bool
	pending        []byte
	convertJSON    func([]byte) ([]byte, error)
	convertChunk   func(*apicompat.ChatCompletionsChunk) ([]byte, error)
	finish         func() ([]byte, error)
	err            error
	writeErr       error
	start          time.Time
	firstToken     *int
}

func (w *oauthChatBridgeWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *oauthChatBridgeWriter) Write(p []byte) (int, error) {
	if w.Status() >= http.StatusBadRequest {
		if w.anthropic {
			var body map[string]any
			if json.Unmarshal(p, &body) == nil {
				body["type"] = "error"
				out, err := json.Marshal(body)
				if err != nil {
					return 0, err
				}
				_, err = w.ResponseWriter.Write(out)
				return len(p), err
			}
		}
		return w.ResponseWriter.Write(p)
	}
	if w.err != nil {
		return 0, w.err
	}
	if !w.stream {
		out, err := w.convertJSON(p)
		if err == nil {
			_, err = w.ResponseWriter.Write(out)
			w.writeErr = err
		}
		w.err = err
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	for {
		i := bytes.Index(w.pending, []byte("\n\n"))
		if i < 0 {
			break
		}
		frame := w.pending[:i]
		w.pending = w.pending[i+2:]
		var out []byte
		var err error
		for _, line := range bytes.Split(frame, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if bytes.Equal(data, []byte("[DONE]")) {
				out, err = w.finish()
			} else {
				var envelope struct {
					Error json.RawMessage `json:"error"`
				}
				if err = json.Unmarshal(data, &envelope); err != nil {
					break
				}
				if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
					// Preserve upstream errors; never synthesize a successful terminal event.
					payload, marshalErr := json.Marshal(struct {
						Type  string          `json:"type"`
						Error json.RawMessage `json:"error"`
					}{"error", envelope.Error})
					err = marshalErr
					out = append(append([]byte("event: error\ndata: "), payload...), '\n', '\n')
				} else {
					var chunk apicompat.ChatCompletionsChunk
					if err = json.Unmarshal(data, &chunk); err != nil {
						break
					}
					out, err = w.convertChunk(&chunk)
					if w.firstToken == nil && chatChunkStartsResponsesOutput(&chunk) && len(out) > 0 {
						ms := int(time.Since(w.start).Milliseconds())
						w.firstToken = &ms
					}
				}
			}
			if err == nil && len(out) > 0 {
				_, err = w.ResponseWriter.Write(out)
				w.writeErr = err
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			w.err = err
			return 0, err
		}
	}
	return len(p), nil
}

func (s *OpenAIGatewayService) forwardOAuthChatBridge(
	ctx context.Context, c *gin.Context, account *Account,
	chatReq *apicompat.ChatCompletionsRequest, responsesReq *apicompat.ResponsesRequest,
	nativeBody []byte,
	defaultMappedModel string, start time.Time,
) (*OpenAIForwardResult, error) {
	writer := &oauthChatBridgeWriter{ResponseWriter: c.Writer, stream: chatReq.Stream, anthropic: responsesReq == nil, start: start}
	if responsesReq != nil {
		body, restorer, err := prepareOAuthChatResponses(nativeBody, chatReq.Model)
		if err != nil {
			writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return nil, err
		}
		nativeBody = body
		writer.convertChunk = func(chunk *apicompat.ChatCompletionsChunk) ([]byte, error) {
			if len(chunk.ResponsesEvent) == 0 {
				return nil, nil
			}
			return restorer.event(chunk.ResponsesEvent)
		}
		writer.finish = func() ([]byte, error) {
			if !restorer.terminal {
				return nil, fmt.Errorf("oauth chat bridge: missing terminal Responses event")
			}
			return []byte("data: [DONE]\n\n"), nil
		}
		writer.convertJSON = func(_ []byte) ([]byte, error) {
			if len(writer.nativeResponse) == 0 {
				return nil, fmt.Errorf("oauth chat bridge: missing native Responses body")
			}
			var response map[string]any
			if err := decodeOpenAIJSONUseNumber(writer.nativeResponse, &response); err != nil {
				return nil, err
			}
			restorer.restoreResponse(response)
			return json.Marshal(response)
		}
	} else {
		state := apicompat.NewChatCompletionsToAnthropicStreamState(chatReq.Model)
		// Serialize using the same direct CC -> Messages converter as the relay.
		writer.convertChunk = func(chunk *apicompat.ChatCompletionsChunk) ([]byte, error) {
			var out bytes.Buffer
			for _, event := range apicompat.ChatCompletionsChunkToAnthropicEvents(chunk, state) {
				frame, err := apicompat.ResponsesAnthropicEventToSSE(event)
				if err != nil {
					return nil, err
				}
				out.WriteString(frame)
			}
			return out.Bytes(), nil
		}
		writer.finish = func() ([]byte, error) {
			var out bytes.Buffer
			for _, event := range apicompat.FinalizeChatCompletionsAnthropicStream(state) {
				frame, err := apicompat.ResponsesAnthropicEventToSSE(event)
				if err != nil {
					return nil, err
				}
				out.WriteString(frame)
			}
			return out.Bytes(), nil
		}
		writer.convertJSON = func(data []byte) ([]byte, error) {
			var cc apicompat.ChatCompletionsResponse
			if err := json.Unmarshal(data, &cc); err != nil {
				return nil, err
			}
			return json.Marshal(apicompat.ChatCompletionsResponseToAnthropic(&cc, chatReq.Model))
		}
	}
	if chatReq.Stream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, err
	}
	if responsesReq != nil {
		body = nativeBody
	}
	logger.L().Debug("openai oauth: forwarding through chat compatibility bridge", zap.Int64("account_id", account.ID))
	// The inner side of the former relay always received /chat/completions.
	// Do not let an outer /responses/compact suffix select the native compact
	// endpoint. Restore the inbound request for logging and billing afterwards.
	originalRequest := c.Request
	c.Request = originalRequest.Clone(originalRequest.Context())
	c.Request.URL.Path = "/v1/chat/completions"
	c.Request.URL.RawPath = ""
	c.Writer = writer
	defer func() {
		c.Writer = writer.ResponseWriter
		c.Request = originalRequest
	}()
	result, err := s.ForwardAsChatCompletions(ctx, c, account, body, "", defaultMappedModel)
	if result != nil {
		result.Duration = time.Since(start)
		if chatReq.Stream {
			result.FirstTokenMs = writer.firstToken
		}
		result.ClientDisconnect = result.ClientDisconnect || writer.writeErr != nil
	}
	if err == nil && writer.err != nil {
		err = fmt.Errorf("oauth chat bridge: %w", writer.err)
	}
	return result, err
}
