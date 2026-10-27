package service

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Adapt the HTTP bridge's SSE output to the already-upgraded WebSocket. Never
// write HTTP headers or bytes to the hijacked connection. This stays inside the
// existing WS turn loop so its admission hooks and replay store run once.
type oauthChatWSWriter struct {
	gin.ResponseWriter
	header  http.Header
	status  int
	size    int
	pending []byte
	send    func([]byte) error
}

func (w *oauthChatWSWriter) Header() http.Header { return w.header }
func (w *oauthChatWSWriter) Status() int         { return w.status }
func (w *oauthChatWSWriter) Size() int           { return w.size }
func (w *oauthChatWSWriter) Written() bool       { return w.size >= 0 }
func (w *oauthChatWSWriter) WriteHeader(code int) {
	if !w.Written() {
		w.status = code
	}
}
func (w *oauthChatWSWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}
func (w *oauthChatWSWriter) Flush()                            { w.WriteHeaderNow() }
func (w *oauthChatWSWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *oauthChatWSWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	if w.status >= http.StatusBadRequest {
		msg := gjson.GetBytes(p, "error.message").String()
		if err := w.send(buildOpenAIWSHTTPBridgeErrorEvent(w.status, msg)); err != nil {
			return 0, err
		}
		w.size += len(p)
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
		for _, line := range bytes.Split(frame, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			if err := w.send(data); err != nil {
				return 0, err
			}
		}
	}
	w.size += len(p)
	return len(p), nil
}

func (s *OpenAIGatewayService) forwardOAuthChatBridgeWSTurn(ctx context.Context, c *gin.Context, account *Account, body []byte, originalModel string, send func([]byte) error) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	var collector openAIWSToolCallReplayCollector
	var createdAtStamper responsesCreatedAtStamper
	var responseID, terminal string
	w := &oauthChatWSWriter{ResponseWriter: c.Writer, header: make(http.Header), status: http.StatusOK, size: -1}
	w.send = func(data []byte) error {
		eventType := gjson.GetBytes(data, "type").String()
		if eventType == "error" && responseID != "" {
			data = buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, createdAtStamper.Value(), data, gjson.GetBytes(data, "error.message").String())
			eventType = "response.failed"
		}
		if stamped, changed := createdAtStamper.Stamp(data, eventType); changed {
			data = stamped
		}
		if originalModel != "" && gjson.GetBytes(data, "response.model").Exists() {
			var err error
			data, err = sjson.SetBytes(data, "response.model", originalModel)
			if err != nil {
				return err
			}
		}
		// Native item IDs are stable. Retain item.done entries even when the
		// upstream omits output from its terminal snapshot.
		collector.AddEvent(eventType, data)
		if id := gjson.GetBytes(data, "response.id").String(); id != "" {
			responseID = id
		}
		if eventType == "response.completed" || eventType == "response.failed" || eventType == "response.incomplete" {
			terminal = eventType
		}
		err := send(data)
		if err == nil && (eventType == "error" || eventType == "response.failed") {
			markOpenAIWSClientVisibleFailure(c, eventType, data)
		}
		return err
	}
	c.Writer = w
	defer func() { c.Writer = w.ResponseWriter }()
	c.Set("openai_ws_http_bridge", true)
	result, err := s.forwardResponsesViaRawChatCompletions(ctx, c, account, body, startTime)
	if result != nil {
		result.OpenAIWSMode = true
		result.Model = originalModel
		if responseID != "" {
			result.RequestID = responseID
		}
		result.UpstreamTerminalEvent = terminal
		result.ResponseHeaders = cloneHeader(result.UpstreamHeaders)
		result.wsReplayInput = collector.Items()
		result.wsReplayInputExists = len(result.wsReplayInput) > 0
		result.wsAccountFailoverReplayInput = collector.AllItems()
	}
	return result, err
}
