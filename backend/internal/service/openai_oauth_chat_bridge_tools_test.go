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

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOAuthChatBridgeNamespacedCustomToolMatchesRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, transport := range []string{"json", "sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			stream := transport != "json"
			body := []byte(fmt.Sprintf(`{"model":"client-model","stream":%v,"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Run JavaScript"}]}]},{"role":"user","content":"Run Get-Location"}],"tool_choice":"required"}`, stream))
			newUpstream := func() *httpUpstreamRecorder {
				item := map[string]any{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "functions__exec", "arguments": `{"input":"await tools.exec_command({cmd: 'Get-Location'})"}`, "status": "completed"}
				added := map[string]any{"type": "function_call", "id": "fc_exec", "call_id": "call_exec", "name": "functions__exec", "arguments": ""}
				events := []any{
					map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_tools", "model": "gpt-5.5", "output": []any{}}},
					map[string]any{"type": "response.output_item.added", "output_index": 0, "item": added},
					map[string]any{"type": "response.function_call_arguments.delta", "output_index": 0, "item_id": "fc_exec", "delta": item["arguments"]},
					map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
					map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_tools", "model": "gpt-5.5", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 5, "output_tokens": 2, "total_tokens": 7}}},
				}
				var payload strings.Builder
				for _, event := range events {
					raw, err := json.Marshal(event)
					require.NoError(t, err)
					fmt.Fprintf(&payload, "data: %s\n\n", raw)
				}
				up := oauthChatTestUpstream()
				up.resp.Body = io.NopCloser(strings.NewReader(payload.String()))
				return up
			}
			run := func(svc *OpenAIGatewayService, account *Account) (string, *OpenAIForwardResult) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				var result *OpenAIForwardResult
				var err error
				if transport == "websocket" && account.UsesOAuthChatBridge() {
					result, err = svc.forwardOAuthChatBridgeWSTurn(context.Background(), c, account, body, "client-model", func(data []byte) error { fmt.Fprintf(rec, "data: %s\n\n", data); return nil })
				} else {
					result, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body, time.Now())
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				return rec.Body.String(), result
			}
			directUp := newUpstream()
			direct, result := run(&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: directUp}, oauthChatTestAccount())
			innerUp := newUpstream()
			innerAccount := oauthChatTestAccount()
			innerAccount.Extra = nil
			inner := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: innerUp}
			reference, _ := run(&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &oauthChatReferenceRelay{inner, innerAccount}}, forceChatResponsesFallbackAccount())
			require.Equal(t, oauthBridgeTerminal(t, reference, stream), oauthBridgeTerminal(t, direct, stream))
			require.Equal(t, "functions__exec", gjson.GetBytes(directUp.lastBody, "tools.0.name").String())
			var response apicompat.ResponsesResponse
			if stream {
				var itemID string
				for _, line := range strings.Split(direct, "\n") {
					if !strings.HasPrefix(line, "data: {") {
						continue
					}
					var event apicompat.ResponsesStreamEvent
					require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event))
					if event.Type == "response.output_item.added" {
						itemID = event.Item.ID
						require.Equal(t, "functions", event.Item.Namespace)
					}
					if event.Type == "response.completed" {
						response = *event.Response
						require.Equal(t, itemID, response.Output[0].ID)
					}
				}
			} else {
				require.NoError(t, json.Unmarshal([]byte(direct), &response))
			}
			require.Len(t, response.Output, 1)
			item := response.Output[0]
			require.Equal(t, "custom_tool_call", item.Type)
			require.Equal(t, "exec", item.Name)
			require.Equal(t, "functions", item.Namespace)
			require.Equal(t, "await tools.exec_command({cmd: 'Get-Location'})", item.Input)
			if transport == "websocket" {
				require.Len(t, result.wsReplayInput, 1)
				require.Contains(t, string(result.wsReplayInput[0]), `"namespace":"functions"`)
			}
		})
	}
}

func TestOAuthChatBridgeNativeCustomToolsUnchanged(t *testing.T) {
	for _, mode := range []string{"auto", "force_responses", "force_chat_completions"} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%v", mode, passthrough), func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.6-terra","stream":false,"instructions":"test","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Run code","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]}],"input":[{"type":"custom_tool_call","namespace":"functions","name":"exec","call_id":"ctc_1","input":"Get-Location"},{"type":"custom_tool_call_output","call_id":"ctc_1","output":"D:\\workspace"}]}`)
				up := oauthChatTestUpstream()
				account := newOpenAIOAuthNamespaceTestAccount()
				account.Extra = map[string]any{"openai_responses_mode": mode, "openai_passthrough": passthrough}
				c := newOpenAIRejectedFieldTestContext(body)
				_, err := newOpenAIRejectedFieldTestService(up).Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Len(t, up.bodies, 1)
				forwarded := up.bodies[0]
				require.JSONEq(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(forwarded, "tools").Raw)
				require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(forwarded, "input").Raw)
				require.NotContains(t, string(forwarded), "functions__exec")
			})
		}
	}
}
