//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func oauthChatTestAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-oauth-token", "chatgpt_account_id": "test-account", "model_mapping": map[string]any{"client-model": "gpt-5.5", "gpt-5.5": "must-not-map-twice"}},
		Extra:       map[string]any{"openai_responses_mode": "force_chat_completions", "openai_passthrough": true}}
}

func oauthChatTestUpstream() *httpUpstreamRecorder {
	payload := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_test","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_test","type":"message","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","item_id":"msg_test","output_index":0,"content_index":0,"delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_test","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"message","id":"msg_test","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
	}, "\n\n") + "\n\n"
	return &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"test-request"}}, Body: io.NopCloser(strings.NewReader(payload))}}
}

// Execute both real converters in a local API-key -> OAuth reference relay.
// Only the external provider is stubbed; no production traffic is generated.
type oauthChatReferenceRelay struct {
	svc     *OpenAIGatewayService
	account *Account
}

func (u *oauthChatReferenceRelay) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header = req.Header.Clone()
	_, err = u.svc.ForwardAsChatCompletions(req.Context(), c, u.account, body, "", "")
	if err != nil {
		return nil, err
	}
	return rec.Result(), nil
}
func (u *oauthChatReferenceRelay) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, n)
}

func normalizeOAuthBridgeJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal(data, &value))
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				if k == "id" || k == "item_id" || k == "created" || k == "created_at" {
					x[k] = "<generated>"
				} else {
					walk(item)
				}
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(value)
	return value
}
func normalizedOAuthBridgeOutput(t *testing.T, raw string, stream bool) any {
	t.Helper()
	if !stream {
		return normalizeOAuthBridgeJSON(t, []byte(raw))
	}
	var result []any
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
			result = append(result, normalizeOAuthBridgeJSON(t, []byte(strings.TrimPrefix(line, "data: "))))
		}
	}
	return result
}

func oauthBridgeTerminal(t *testing.T, raw string, stream bool) map[string]any {
	t.Helper()
	if stream {
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "data: {") {
				payload := strings.TrimPrefix(line, "data: ")
				if gjson.Get(payload, "type").String() == "response.completed" {
					raw = gjson.Get(payload, "response").Raw
					break
				}
			}
		}
	}
	response := normalizeOAuthBridgeJSON(t, []byte(raw)).(map[string]any)
	return map[string]any{"model": response["model"], "status": response["status"], "output": response["output"], "usage": response["usage"]}
}

func TestOAuthChatBridgeMatchesRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"client-model","input":"hi","stream":%v}`, stream))
				if path == "/v1/messages" || path == "/v1/chat/completions" {
					body = []byte(fmt.Sprintf(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"max_tokens":32,"stream":%v}`, stream))
				}
				run := func(svc *OpenAIGatewayService, a *Account) (string, *OpenAIForwardResult) {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
					originalWriter := c.Writer
					var result *OpenAIForwardResult
					var err error
					switch path {
					case "/v1/messages":
						result, err = svc.ForwardAsAnthropic(context.Background(), c, a, body, "", "")
					case "/v1/chat/completions":
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, a, body, "", "")
					default:
						result, err = svc.Forward(context.Background(), c, a, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Same(t, originalWriter, c.Writer)
					require.Equal(t, 200, rec.Code)
					return rec.Body.String(), result
				}
				directUp := oauthChatTestUpstream()
				directSvc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: directUp}
				direct, result := run(directSvc, oauthChatTestAccount())
				innerUp := oauthChatTestUpstream()
				innerSvc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: innerUp}
				innerAccount := oauthChatTestAccount()
				innerAccount.Extra = nil
				relaySvc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &oauthChatReferenceRelay{innerSvc, innerAccount}}
				relayAccount := forceChatResponsesFallbackAccount()
				relayAccount.Credentials = map[string]any{"api_key": "test-relay", "base_url": "https://relay.example.com"}
				reference, _ := run(relaySvc, relayAccount)
				if path == "/v1/responses" {
					require.Equal(t, oauthBridgeTerminal(t, reference, stream), oauthBridgeTerminal(t, direct, stream))
					require.Contains(t, gjson.GetBytes(directUp.lastBody, "input").Raw, "hi")
				} else {
					require.Equal(t, normalizedOAuthBridgeOutput(t, reference, stream), normalizedOAuthBridgeOutput(t, direct, stream))
					require.Equal(t, normalizeOAuthBridgeJSON(t, innerUp.lastBody), normalizeOAuthBridgeJSON(t, directUp.lastBody))
				}
				require.Len(t, directUp.requests, 1)
				require.Equal(t, chatgptCodexURL, directUp.lastReq.URL.String())
				require.Equal(t, "Bearer test-oauth-token", directUp.lastReq.Header.Get("Authorization"))
				require.Equal(t, "gpt-5.5", result.UpstreamModel)
				require.Equal(t, 5, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
			})
		}
	}
}

func TestOAuthChatBridgeModeAndCapabilities(t *testing.T) {
	a := oauthChatTestAccount()
	require.True(t, a.UsesOAuthChatBridge())
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(a))
	require.Equal(t, OpenAIWSIngressModeOff, a.ResolveOpenAIResponsesWebSocketV2Mode("off"))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	for _, mode := range []any{nil, "auto", "force_responses", false, []string{"force_chat_completions"}} {
		a.Extra["openai_responses_mode"] = mode
		require.False(t, a.UsesOAuthChatBridge())
		require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	}
	a.Extra["openai_responses_supported"] = false
	require.False(t, a.UsesOAuthChatBridge())
}

func TestOAuthChatBridgeWSTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	up := oauthChatTestUpstream()
	svc := &OpenAIGatewayService{httpUpstream: up, cfg: rawChatCompletionsTestConfig()}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	var events []string
	payload := []byte(`{"type":"response.create","model":"gpt-5.5","input":"hello"}`)
	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, oauthChatTestAccount(), "test-oauth-token", payload, len(payload), "client-model", "", "", "", "", 1, func(data []byte) error { events = append(events, string(data)); return nil })
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.OpenAIWSMode)
	require.NotEmpty(t, result.RequestID)
	require.Empty(t, rec.Body.String(), "must not write HTTP to an upgraded WebSocket")
	require.NotEmpty(t, events)
	require.Equal(t, "response.completed", gjson.Get(events[len(events)-1], "type").String())
	require.Equal(t, "gpt-5.5", gjson.GetBytes(up.lastBody, "model").String())
	require.NotEmpty(t, result.wsAccountFailoverReplayInput)
}

func TestOAuthChatBridgeWriterEmitsBeforeTerminal(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	w := &oauthChatBridgeWriter{ResponseWriter: c.Writer, stream: true, start: time.Now(), convertChunk: func(_ *apicompat.ChatCompletionsChunk) ([]byte, error) { return []byte("delta"), nil }, finish: func() ([]byte, error) { return []byte("done"), nil }}
	_, err := w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n"))
	require.NoError(t, err)
	require.Empty(t, rec.Body.String())
	_, err = w.Write([]byte("\n"))
	require.NoError(t, err)
	require.Equal(t, "delta", rec.Body.String())
	require.NotNil(t, w.firstToken)
}

func TestOAuthChatBridgeErrorsAndDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		payload    string
		disconnect bool
	}{
		{"invalid request", 400, `{"error":{"type":"invalid_request_error","message":"bad input"}}`, false},
		{"retry before output", 429, `{"error":{"type":"rate_limit_error","message":"rate limited"}}`, false},
		{"partial failure", 200, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" + "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"context_length_exceeded\",\"message\":\"input exceeds the context window\"}}}\n\n", false},
		{"disconnect", 200, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := oauthChatTestUpstream()
			if tc.payload != "" {
				up.resp.StatusCode = tc.status
				up.resp.Body = io.NopCloser(strings.NewReader(tc.payload))
			}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if tc.disconnect {
				c.Writer = &openAIChatFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
			}
			result, err := svc.forwardResponsesViaRawChatCompletions(context.Background(), c, oauthChatTestAccount(), []byte(`{"model":"client-model","input":"hi","stream":true}`), time.Now())
			require.Error(t, err)
			require.NotContains(t, rec.Body.String(), "response.completed")
			if tc.status == 429 {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.False(t, c.Writer.Written())
			}
			if tc.disconnect {
				require.NotNil(t, result)
				require.Equal(t, 5, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.True(t, result.ClientDisconnect)
			}
		})
	}
}

func TestOAuthChatBridgeSpecialInputsMatchRelay(t *testing.T) {
	for _, input := range []string{
		`{"input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`,
		`{"input":"hello","tools":[{"type":"image_generation"},{"type":"web_search"}]}`,
		`{"input":[{"type":"reasoning","id":"rs_test","summary":[{"type":"summary_text","text":"thinking"}]},{"type":"function_call","call_id":"call_1","name":"inspect","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"done"}],"tools":[{"type":"function","name":"inspect","parameters":{"type":"object","properties":{}}}]}`,
	} {
		t.Run(input[:20], func(t *testing.T) {
			body := []byte(`{"model":"client-model","stream":false,` + input[1:])
			run := func(svc *OpenAIGatewayService, a *Account) string {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, err := svc.forwardResponsesViaRawChatCompletions(context.Background(), c, a, body, time.Now())
				require.NoError(t, err)
				return rec.Body.String()
			}
			directUp := oauthChatTestUpstream()
			direct := run(&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: directUp}, oauthChatTestAccount())
			innerUp := oauthChatTestUpstream()
			inner := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: innerUp}
			account := oauthChatTestAccount()
			account.Extra = nil
			reference := run(&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &oauthChatReferenceRelay{inner, account}}, forceChatResponsesFallbackAccount())
			require.Equal(t, oauthBridgeTerminal(t, reference, false), oauthBridgeTerminal(t, direct, false))
			require.NotContains(t, string(directUp.lastBody), "<thinking>")
			for _, tool := range gjson.Get(input, "tools").Array() {
				require.Contains(t, gjson.GetBytes(directUp.lastBody, "tools.#.type").Raw, tool.Get("type").String())
			}
			if gjson.Get(input, "input.0.type").String() == "reasoning" {
				require.Equal(t, "thinking", gjson.GetBytes(directUp.lastBody, "input.0.summary.0.text").String())
			}
		})
	}
}

func TestOAuthChatBridgeWebSocketReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	first := oauthChatTestUpstream().resp
	first.Body = io.NopCloser(strings.NewReader(
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"inspect","arguments":""}}` + "\n\n" +
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{}"}` + "\n\n" +
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"inspect","arguments":"{}","status":"completed"}}` + "\n\n" +
			`data: {"type":"response.completed","response":{"id":"resp_tools","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"inspect","arguments":"{}","status":"completed"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}` + "\n\n"))
	up := &httpUpstreamRecorder{responses: []*http.Response{first, oauthChatTestUpstream().resp}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: up}
	account := oauthChatTestAccount()
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		_, first, err := conn.Read(r.Context())
		if err != nil {
			done <- err
			return
		}
		done <- svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, "test-oauth-token", first, nil)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	readCompleted := func() []byte {
		for {
			_, data, err := conn.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(data, "type").String() == "response.completed" {
				return data
			}
			require.NotEqual(t, "error", gjson.GetBytes(data, "type").String(), string(data))
		}
	}
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"client-model","input":"inspect","tools":[{"type":"function","name":"inspect","parameters":{"type":"object","properties":{}}}]}`)))
	completed := readCompleted()
	require.Equal(t, "client-model", gjson.GetBytes(completed, "response.model").String())
	require.Contains(t, string(completed), "call_1")
	next := fmt.Sprintf(`{"type":"response.create","model":"client-model","previous_response_id":%q,"input":[{"type":"function_call_output","call_id":"call_1","output":"done"}]}`, gjson.GetBytes(completed, "response.id").String())
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(next)))
	readCompleted()
	_ = conn.Close(websocket.StatusNormalClosure, "done")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("WebSocket loop did not finish")
	}
	require.Len(t, up.bodies, 2)
	for _, body := range up.bodies {
		require.Equal(t, "gpt-5.5", gjson.GetBytes(body, "model").String())
	}
	require.Contains(t, string(up.bodies[1]), "function_call")
	require.Contains(t, string(up.bodies[1]), "function_call_output")
	var calls, outputs []gjson.Result
	for _, item := range gjson.GetBytes(up.bodies[1], "input").Array() {
		switch item.Get("type").String() {
		case "function_call":
			calls = append(calls, item)
		case "function_call_output":
			outputs = append(outputs, item)
		}
	}
	require.Len(t, calls, 1)
	require.Len(t, outputs, 1)
	require.Equal(t, calls[0].Get("call_id").String(), outputs[0].Get("call_id").String())
	require.NotContains(t, string(up.bodies[1]), "previous_response_id")
}

// Responses keeps the complete Lite contract; Chat/Messages still strip it.
func TestOAuthChatBridgePreservesProtocolSpecificLiteContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "websocket"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", path, stream), func(t *testing.T) {
				up := oauthChatTestUpstream()
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set(responsesLiteHeader, "true")
				originalRequest := c.Request
				body := []byte(fmt.Sprintf(`{"model":"client-model","input":"111","reasoning":{"effort":"high","context":"all_turns"},"stream":%v}`, stream))
				var result *OpenAIForwardResult
				var err error
				switch path {
				case "/v1/messages", "/v1/chat/completions":
					body = []byte(fmt.Sprintf(`{"model":"client-model","messages":[{"role":"user","content":"111"}],"max_tokens":32,"stream":%v}`, stream))
					c.Request.URL.Path = path
					if path == "/v1/messages" {
						result, err = svc.ForwardAsAnthropic(context.Background(), c, oauthChatTestAccount(), body, "", "")
					} else {
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, oauthChatTestAccount(), body, "", "")
					}
				case "websocket":
					result, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, oauthChatTestAccount(), "test-oauth-token", body, len(body), "client-model", "", "", "", "", 1, func([]byte) error { return nil })
				default:
					result, err = svc.Forward(context.Background(), c, oauthChatTestAccount(), body)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, up.requests, 1)
				if path == "/v1/responses" || path == "websocket" {
					require.Equal(t, "true", up.lastReq.Header.Get(responsesLiteHeader))
					require.Equal(t, "all_turns", gjson.GetBytes(up.lastBody, "reasoning.context").String())
				} else {
					require.Empty(t, up.lastReq.Header.Get(responsesLiteHeader))
					require.False(t, gjson.GetBytes(up.lastBody, "reasoning.context").Exists())
				}
				require.Same(t, originalRequest, c.Request)
				require.Equal(t, "true", c.GetHeader(responsesLiteHeader), "do not mutate the original request or a subsequent failover attempt")
			})
		}
	}
}

func TestOAuthChatBridgeNativeResponsesKeepsLiteContract(t *testing.T) {
	for _, model := range []string{"gpt-5.4", "gpt-5.5"} {
		for _, mode := range []string{"auto", "force_responses"} {
			for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
				t.Run(model+"/"+mode+path, func(t *testing.T) {
					up := oauthChatTestUpstream()
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, path, nil)
					c.Request.Header.Set(responsesLiteHeader, "true")
					account := oauthChatTestAccount()
					account.Extra = map[string]any{"openai_responses_mode": mode}
					account.Credentials["model_mapping"] = map[string]any{"client-model": model}
					body := []byte(`{"model":"client-model","input":"111","reasoning":{"effort":"high","context":"all_turns"},"stream":true}`)
					var err error
					if path == "/v1/responses" {
						_, err = svc.Forward(context.Background(), c, account, body)
					} else {
						_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
					}
					require.NoError(t, err)
					if model == "gpt-5.5" {
						require.Empty(t, up.lastReq.Header.Get(responsesLiteHeader))
					} else {
						require.Equal(t, "true", up.lastReq.Header.Get(responsesLiteHeader))
					}
					require.Equal(t, "all_turns", gjson.GetBytes(up.lastBody, "reasoning.context").String())
				})
			}
		}
	}
}
