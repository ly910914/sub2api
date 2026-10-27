//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOAuthChatBridgePreservesNativeRequest(t *testing.T) {
	body := []byte(`{"model":"client-model","reasoning":{"effort":"high","context":"all_turns","summary":"auto"},"include":["reasoning.encrypted_content"],"context_management":[{"type":"compaction","compact_threshold":12000}],"text":{"format":{"type":"json_schema","name":"result","schema":{"type":"object"}}},"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]},{"type":"web_search"},{"type":"image_generation"},{"type":"mcp","server_label":"docs","server_url":"https://example.com/mcp","require_approval":"always"}],"input":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"private summary"}],"encrypted_content":"opaque"},{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking"}]},{"type":"custom_tool_call","call_id":"call_1","namespace":"functions","name":"exec","input":"Get-Location"},{"type":"custom_tool_call_output","call_id":"call_1","output":[{"type":"input_text","text":"image result"},{"type":"input_image","image_url":"https://example.com/result.png"}]},{"type":"compaction","encrypted_content":"compacted"},{"type":"message","role":"user","content":"Keep literal <thinking> tags"}]}`)
	adapted, _, err := prepareOAuthChatResponses(body, "client-model")
	require.NoError(t, err)
	for _, path := range []string{"reasoning", "include", "context_management", "text", "input.0", "input.1", "input.3.output", "input.4", "input.5", "tools.1", "tools.2", "tools.3"} {
		require.JSONEq(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(adapted, path).Raw, path)
	}
	require.Equal(t, "functions__exec", gjson.GetBytes(adapted, "tools.0.name").String())
	require.Equal(t, "function_call", gjson.GetBytes(adapted, "input.2.type").String())
	require.Equal(t, "functions__exec", gjson.GetBytes(adapted, "input.2.name").String())
	require.JSONEq(t, `{"input":"Get-Location"}`, gjson.GetBytes(adapted, "input.2.arguments").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(adapted, "input.3.type").String())
	require.Equal(t, 1, strings.Count(string(adapted), "thinking"), "only user-authored tags survive")
	up := oauthChatTestUpstream()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, oauthChatTestAccount(), body, time.Now())
	require.NoError(t, err)
	require.Equal(t, "opaque", gjson.GetBytes(up.lastBody, "input.0.encrypted_content").String())
	require.Equal(t, "commentary", gjson.GetBytes(up.lastBody, "input.1.phase").String())
	require.NotContains(t, string(up.lastBody), "<thinking>private summary")
}

func TestOAuthChatBridgePluginDiscovery(t *testing.T) {
	for _, execution := range []string{"client", "server", ""} {
		t.Run(execution, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"client-model","input":"Find tools","tools":[{"type":"tool_search","execution":%q,"description":"Find project tools","parameters":{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"]}},{"type":"namespace","name":"plugin","tools":[{"type":"function","name":"lookup","defer_loading":true,"parameters":{"type":"object"}}]}]}`, execution))
			adapted, restorer, err := prepareOAuthChatResponses(body, "client-model")
			require.NoError(t, err)
			if execution != "client" {
				require.JSONEq(t, string(body), string(adapted))
				require.False(t, restorer.toolSearch)
				return
			}
			require.True(t, restorer.toolSearch)
			require.JSONEq(t, gjson.GetBytes(body, "tools.0.parameters").Raw, gjson.GetBytes(adapted, "tools.0.parameters").Raw)
			require.Equal(t, "plugin__lookup", gjson.GetBytes(adapted, "tools.1.name").String())
			require.False(t, gjson.GetBytes(adapted, "tools.1.defer_loading").Exists())
			frame, err := restorer.event([]byte(`{"type":"response.output_item.done","output_index":3,"item":{"type":"function_call","id":"fc_search","call_id":"call_search","name":"tool_search","arguments":"{\"goal\":\"files\"}","status":"completed"}}`))
			require.NoError(t, err)
			payload := strings.TrimSpace(strings.SplitN(string(frame), "data: ", 2)[1])
			require.Equal(t, "tool_search_call", gjson.Get(payload, "item.type").String())
			require.Equal(t, "client", gjson.Get(payload, "item.execution").String())
			require.Equal(t, "files", gjson.Get(payload, "item.arguments.goal").String())
		})
	}
	// Official client discovery continuation may omit top-level tools entirely.
	body := []byte(`{"model":"client-model","input":[{"type":"tool_search_output","execution":"client","status":"completed","call_id":"call_search","tools":[{"type":"namespace","name":"plugin","tools":[{"type":"custom","name":"run","defer_loading":true}]}]}]}`)
	adapted, restorer, err := prepareOAuthChatResponses(body, "client-model")
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(adapted))
	require.Empty(t, restorer.custom)
}

func TestOAuthChatBridgeNativeResponseFidelity(t *testing.T) {
	output := `[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Checking"}],"encrypted_content":"opaque"},{"type":"message","id":"msg_1","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"Done","annotations":[{"type":"url_citation","url":"https://example.com","title":"Source","start_index":0,"end_index":4}]}]},{"type":"mcp_approval_request","id":"approval_1","server_label":"docs","name":"read","arguments":"{}"}]`
	response := `{"id":"resp_native","object":"response","created_at":123,"model":"upstream-model","status":"completed","output":` + output + `,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7},"future_metadata":{"opaque":true}}`
	events := []string{
		`{"type":"response.created","response":{"id":"resp_native","object":"response","created_at":123,"model":"upstream-model","status":"in_progress","output":[]}}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Checking","future_field":"preserved"}`,
		`{"type":"response.output_item.done","output_index":0,"item":` + gjson.Get(output, "0").Raw + `}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","phase":"final_answer","status":"in_progress","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"Done"}`,
		`{"type":"response.mcp_call.in_progress","item_id":"mcp_1","output_index":2}`,
		`{"type":"response.completed","response":` + response + `}`,
	}
	for _, transport := range []string{"json", "sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			up := oauthChatTestUpstream()
			up.resp.Body = io.NopCloser(strings.NewReader("data: " + strings.Join(events, "\n\ndata: ") + "\n\n"))
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte(fmt.Sprintf(`{"model":"client-model","input":"hello","stream":%v}`, transport != "json"))
			var err error
			if transport == "websocket" {
				_, err = svc.forwardOAuthChatBridgeWSTurn(context.Background(), c, oauthChatTestAccount(), body, "client-model", func(data []byte) error { fmt.Fprintf(rec, "data: %s\n\n", data); return nil })
			} else {
				_, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, oauthChatTestAccount(), body, time.Now())
			}
			require.NoError(t, err)
			var terminal string
			if transport == "json" {
				terminal = rec.Body.String()
			} else {
				var got []string
				for _, line := range strings.Split(rec.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: {") {
						got = append(got, strings.TrimPrefix(line, "data: "))
					}
				}
				require.Len(t, got, len(events))
				for i, original := range events {
					var expected, actual map[string]any
					require.NoError(t, json.Unmarshal([]byte(original), &expected))
					require.NoError(t, json.Unmarshal([]byte(got[i]), &actual))
					expected["sequence_number"] = float64(i)
					if r, ok := expected["response"].(map[string]any); ok {
						r["model"] = "client-model"
					}
					require.Equal(t, expected, actual)
				}
				terminal = gjson.Get(got[len(got)-1], "response").Raw
			}
			require.JSONEq(t, output, gjson.Get(terminal, "output").Raw)
			require.True(t, gjson.Get(terminal, "future_metadata.opaque").Bool())
			require.Equal(t, int64(123), gjson.Get(terminal, "created_at").Int())
			require.Equal(t, "client-model", gjson.Get(terminal, "model").String())
			require.NotContains(t, rec.Body.String(), "<thinking>")
			require.NotContains(t, rec.Body.String(), "_responses_event")
		})
	}
}

func TestOAuthChatBridgeCompactionUsesNativeEndpoint(t *testing.T) {
	up := oauthChatTestUpstream()
	up.resp.Header.Set("Content-Type", "application/json")
	up.resp.Body = io.NopCloser(strings.NewReader(`{"id":"cmp_1","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`))
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: up}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	_, err := svc.Forward(context.Background(), c, oauthChatTestAccount(), []byte(`{"model":"client-model","input":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(up.lastReq.URL.Path, "/responses/compact"))
	require.Equal(t, "response.compaction", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "opaque", gjson.Get(rec.Body.String(), "output.0.encrypted_content").String())
}

func TestOAuthChatBridgeCustomArgumentLifecycle(t *testing.T) {
	for _, withArgumentsDone := range []bool{false, true} {
		t.Run(fmt.Sprint(withArgumentsDone), func(t *testing.T) {
			_, restorer, err := prepareOAuthChatResponses([]byte(`{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`), "model")
			require.NoError(t, err)
			events := []string{
				`{"type":"response.output_item.added","output_index":4,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"functions__exec","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":4,"delta":"{\"input\":\"hello"}`,
				`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":4,"delta":" world\"}"}`,
			}
			if withArgumentsDone {
				events = append(events, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":4,"arguments":"{\"input\":\"hello world\"}"}`)
			}
			events = append(events, `{"type":"response.output_item.done","output_index":4,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"functions__exec","status":"completed"}}`)
			var output strings.Builder
			for _, event := range events {
				frame, err := restorer.event([]byte(event))
				require.NoError(t, err)
				output.Write(frame)
			}
			require.NotContains(t, output.String(), "function_call_arguments")
			require.Equal(t, 2, strings.Count(output.String(), "response.custom_tool_call_input.done"), "one event header and one JSON type")
			require.Contains(t, output.String(), `"input":"hello world"`)
			require.Equal(t, "hello world", restorer.tools["fc_1"].item["input"])
			require.Equal(t, "functions", restorer.tools["fc_1"].item["namespace"])
		})
	}
	_, restorer, err := prepareOAuthChatResponses([]byte(`{"tools":[{"type":"custom","name":"exec"},{"type":"function","name":"functions__exec"},{"type":"namespace","name":"functions","tools":[{"type":"function","name":"wait"}]}]}`), "model")
	require.NoError(t, err)
	item := map[string]any{"type": "function_call", "name": "functions__exec", "arguments": "{}", "call_id": "call_1"}
	restorer.restoreItem(item)
	require.Equal(t, "function_call", item["type"], "an explicitly declared function must win over inferred custom aliases")
}

func TestOAuthChatBridgePreservesIncompleteTerminal(t *testing.T) {
	_, restorer, err := prepareOAuthChatResponses([]byte(`{"input":"hello"}`), "model")
	require.NoError(t, err)
	frame, err := restorer.event([]byte(`{"type":"response.done","response":{"id":"r1","status":"incomplete","output":[],"incomplete_details":{"reason":"max_output_tokens"}}}`))
	require.NoError(t, err)
	require.True(t, restorer.terminal)
	require.Contains(t, string(frame), "response.incomplete")
	require.NotContains(t, string(frame), "response.completed")
	require.Contains(t, string(frame), "max_output_tokens")
}
