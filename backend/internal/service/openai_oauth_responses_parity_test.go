//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Compare the public entry point, including request preparation, policies,
// native tools, wire events, usage and configured WebSocket routing.
func TestOAuthResponsesModeParity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requests := []struct {
		name, fields string
		lite         bool
	}{
		{"text", `"input":"hello"`, false},
		{"custom_namespace", `"input":[{"role":"user","content":"Run a command"}],"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}},{"type":"function","name":"wait","parameters":{"type":"object","properties":{}}}]}],"tool_choice":{"type":"custom","namespace":"functions","name":"exec"}`, false},
		{"lite", `"reasoning":{"context":"all_turns","effort":"medium"},"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]},{"role":"user","content":"hello"}]`, true},
		{"replay", `"reasoning":{"context":"all_turns","summary":"auto"},"input":[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"Checking"}]},{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking the fixture"}]},{"type":"custom_tool_call","id":"ctc_1","namespace":"functions","name":"exec","call_id":"call_1","input":"Get-Location"},{"type":"custom_tool_call_output","call_id":"call_1","output":[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"https://example.com/result.png"}]},{"type":"compaction","encrypted_content":"compacted"},{"role":"user","content":"Continue"}],"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]`, false},
		{"plugin_client", `"input":"Find tools","tools":[{"type":"tool_search","execution":"client","parameters":{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"]}},{"type":"namespace","name":"plugin","tools":[{"type":"function","name":"read","defer_loading":true,"parameters":{"type":"object"}}]}]`, false},
		{"plugin_hosted", `"input":"Read docs","tools":[{"type":"tool_search"},{"type":"mcp","server_label":"docs","server_url":"https://example.com/mcp","require_approval":"always","defer_loading":true},{"type":"web_search"},{"type":"image_generation"}]`, false},
		{"discovery_continuation", `"input":[{"type":"tool_search_call","execution":"client","call_id":"search1","arguments":{"goal":"fixture"}},{"type":"tool_search_output","execution":"client","call_id":"search1","status":"completed","tools":[{"type":"namespace","name":"fixture","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]}]`, false},
	}
	for _, client := range []string{"codex_cli_rs/0.141.0", "codex_app/2.1.0"} {
		for _, passthrough := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, tc := range requests {
					t.Run(fmt.Sprintf("%s/pass=%v/stream=%v/%s", client, passthrough, stream, tc.name), func(t *testing.T) {
						body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-terra","instructions":"Fixture","stream":%v,%s}`, stream, tc.fields))
						var referenceBody []byte
						var referenceOutput, referenceURL string
						var referenceUsage OpenAIUsage
						for _, mode := range []string{"force_responses", "auto", "force_chat_completions"} {
							account := newOpenAIOAuthNamespaceTestAccount()
							account.Extra = map[string]any{"openai_responses_mode": mode, "openai_passthrough": passthrough}
							up := oauthParityUpstream("completed")
							svc := newOpenAIRejectedFieldTestService(up)
							rec := httptest.NewRecorder()
							c, _ := gin.CreateTestContext(rec)
							c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
							c.Request.Header.Set("User-Agent", client)
							if tc.lite {
								c.Request.Header.Set(responsesLiteHeader, "true")
							}
							result, err := svc.Forward(context.Background(), c, account, body)
							require.NoError(t, err)
							require.NotNil(t, result)
							require.Equal(t, 200, rec.Code)
							require.Len(t, up.bodies, 1)
							require.NotContains(t, rec.Body.String(), "_responses_event")
							require.NotContains(t, string(up.lastBody), "<thinking>")
							if mode == "force_responses" {
								referenceBody, referenceOutput, referenceURL, referenceUsage = up.lastBody, rec.Body.String(), up.lastReq.URL.String(), result.Usage
							} else {
								require.JSONEq(t, string(referenceBody), string(up.lastBody), mode)
								require.Equal(t, referenceOutput, rec.Body.String(), mode)
								require.Equal(t, referenceURL, up.lastReq.URL.String(), mode)
								require.Equal(t, referenceUsage, result.Usage, mode)
							}
							if tc.name == "custom_namespace" {
								require.JSONEq(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(up.lastBody, "tools").Raw)
								require.NotContains(t, string(up.lastBody), "functions__exec")
							}
						}
						for _, marker := range []string{"encrypted_content", "commentary", "final_answer", "custom_tool_call", "mcp_approval_request", "url_citation", "future_field"} {
							require.Contains(t, referenceOutput, marker)
						}
					})
				}
			}
		}
	}
}

func oauthParityUpstream(status string) *httpUpstreamRecorder {
	output := `[{"type":"reasoning","id":"rs_native","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"Checking"}]},{"type":"message","id":"msg_progress","role":"assistant","phase":"commentary","status":"completed","content":[{"type":"output_text","text":"Checking"}]},{"type":"custom_tool_call","id":"ctc_native","namespace":"functions","name":"exec","call_id":"call_native","input":"Get-Location","status":"completed"},{"type":"mcp_approval_request","id":"approval_native","server_label":"docs","name":"read","arguments":"{}"},{"type":"message","id":"msg_final","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"Done","annotations":[{"type":"url_citation","url":"https://example.com","title":"Source","start_index":0,"end_index":4}]}]}]`
	response := fmt.Sprintf(`{"id":"resp_native","object":"response","created_at":123,"model":"gpt-5.6-terra","status":%q,"output":%s,"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18},"future_field":{"counter":9007199254740993}}`, status, output)
	if status == "incomplete" {
		response = strings.TrimSuffix(response, "}") + `,"incomplete_details":{"reason":"max_output_tokens"}}`
	}
	events := []string{
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_native","model":"gpt-5.6-terra","object":"response","created_at":123,"status":"in_progress","output":[]}}`,
		`{"type":"response.reasoning_summary_text.delta","sequence_number":1,"item_id":"rs_native","output_index":0,"summary_index":0,"delta":"Checking"}`,
		`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_progress","output_index":1,"content_index":0,"delta":"Checking"}`,
		`{"type":"response.output_item.added","sequence_number":3,"output_index":2,"item":{"type":"custom_tool_call","id":"ctc_native","namespace":"functions","name":"exec","call_id":"call_native","input":"","status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.delta","sequence_number":4,"item_id":"ctc_native","output_index":2,"delta":"Get-Location"}`,
		`{"type":"response.output_item.done","sequence_number":5,"output_index":2,"item":` + gjson.Get(output, "2").Raw + `}`,
		`{"type":"response.mcp_call.in_progress","sequence_number":6,"item_id":"mcp_native","output_index":3}`,
		fmt.Sprintf(`{"type":"response.%s","sequence_number":7,"response":%s}`, status, response),
	}
	up := oauthChatTestUpstream()
	up.resp.Body = io.NopCloser(strings.NewReader("data: " + strings.Join(events, "\n\ndata: ") + "\n\n"))
	return up
}

func TestOAuthResponsesPolicyAndRoutingParity(t *testing.T) {
	for _, mode := range []string{"auto", "force_responses", "force_chat_completions"} {
		t.Run(mode, func(t *testing.T) {
			account := oauthChatTestAccount()
			account.Extra["openai_responses_mode"] = mode
			require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
			for _, wsMode := range []string{OpenAIWSIngressModeOff, OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
				account.Extra["openai_oauth_responses_websockets_v2_mode"] = wsMode
				require.Equal(t, wsMode, account.ResolveOpenAIResponsesWebSocketV2Mode("off"))
			}
			account.Extra["codex_cli_only"] = true
			up := oauthParityUpstream("completed")
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", "curl/8.0")
			_, err := newOpenAIRejectedFieldTestService(up).Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.6-terra","input":"hello"}`))
			require.ErrorContains(t, err, "codex_cli_only")
			require.Empty(t, up.bodies)
			require.Equal(t, http.StatusForbidden, c.Writer.Status())
		})
	}
}

func TestOAuthResponsesWebSocketWarmupCancelParity(t *testing.T) {
	for _, mode := range []string{"force_responses", "force_chat_completions"} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
			up := newStagedPassthroughConn()
			account := oauthChatTestAccount()
			account.Extra["openai_responses_mode"] = mode
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
			server, done := startPassthroughLifecycleServer(t, context.Background(), newPassthroughLifecycleService(cfg, up), account)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			first := []byte(`{"type":"response.create","model":"gpt-5.6-terra","generate":false,"store":false,"input":[{"role":"user","content":"hello"}],"tools":[{"type":"custom","name":"exec"}]}`)
			require.NoError(t, conn.Write(ctx, websocket.MessageText, first))
			forwarded := requirePassthroughUpstreamWrite(t, up, time.Second)
			require.True(t, gjson.GetBytes(forwarded, "generate").Exists())
			require.False(t, gjson.GetBytes(forwarded, "generate").Bool())
			require.JSONEq(t, gjson.GetBytes(first, "tools").Raw, gjson.GetBytes(forwarded, "tools").Raw)
			warmup := `{"type":"response.completed","response":{"id":"resp_warm","model":"gpt-5.6-terra","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`
			up.Send(warmup)
			_, frame, err := conn.Read(ctx)
			require.NoError(t, err)
			require.JSONEq(t, warmup, string(frame))
			followup := []byte(`{"type":"response.create","model":"gpt-5.6-terra","previous_response_id":"resp_warm","input":[{"role":"user","content":"continue"}]}`)
			require.NoError(t, conn.Write(ctx, websocket.MessageText, followup))
			forwarded = requirePassthroughUpstreamWrite(t, up, time.Second)
			require.Equal(t, "resp_warm", gjson.GetBytes(forwarded, "previous_response_id").String())
			up.Send(`{"type":"response.created","response":{"id":"resp_second","model":"gpt-5.6-terra"}}`)
			_, _, err = conn.Read(ctx)
			require.NoError(t, err)
			control := []byte(`{"type":"response.cancel","response_id":"resp_second"}`)
			require.NoError(t, conn.Write(ctx, websocket.MessageText, control))
			require.JSONEq(t, string(control), string(requirePassthroughUpstreamWrite(t, up, time.Second)))
			up.Send(`{"type":"response.cancelled","response":{"id":"resp_second","model":"gpt-5.6-terra","status":"cancelled","output":[]}}`)
			_, frame, err = conn.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "response.cancelled", gjson.GetBytes(frame, "type").String())
			require.NoError(t, conn.Close(websocket.StatusNormalClosure, "done"))
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("WebSocket did not close")
			}
		})
	}
}

func TestOAuthResponsesIncompleteParity(t *testing.T) {
	var previous string
	for _, mode := range []string{"force_responses", "force_chat_completions"} {
		account := oauthChatTestAccount()
		account.Extra["openai_responses_mode"] = mode
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := newOpenAIRejectedFieldTestService(oauthParityUpstream("incomplete")).Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.6-terra","input":"hello","stream":true}`))
		require.NoError(t, err)
		require.Contains(t, rec.Body.String(), "response.incomplete")
		require.Contains(t, rec.Body.String(), "max_output_tokens")
		require.NotContains(t, rec.Body.String(), "response.completed")
		if previous != "" {
			require.Equal(t, previous, rec.Body.String())
		}
		previous = rec.Body.String()
	}
}
