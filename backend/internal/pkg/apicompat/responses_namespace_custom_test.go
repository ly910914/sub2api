package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeNamespaceCustomToolRoundTrip(t *testing.T) {
	var req map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}],"tool_choice":{"type":"custom","namespace":"functions","name":"exec"},"input":[{"type":"custom_tool_call","namespace":"functions","name":"exec","call_id":"call_1","input":"hello"},{"type":"custom_tool_call_output","call_id":"call_1","output":"ok"}]}`), &req))
	mapping, changed, err := AdaptResponsesClientTools(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, mapping.CustomTools["functions__exec"])
	require.Equal(t, "function", req["tools"].([]any)[0].(map[string]any)["type"])
	require.Equal(t, "functions__exec", req["tool_choice"].(map[string]any)["name"])
	require.Equal(t, "functions__exec", req["input"].([]any)[0].(map[string]any)["name"])
	restorer := NewResponsesClientToolStreamRestorer(mapping)
	added := restorer.Restore(ResponsesStreamEvent{Type: "response.output_item.added", Item: &ResponsesOutput{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "functions__exec"}})
	require.Len(t, added, 1)
	require.Equal(t, "custom_tool_call", added[0].Item.Type)
	require.Equal(t, "functions", added[0].Item.Namespace)
	require.Equal(t, "exec", added[0].Item.Name)
	done := restorer.Restore(ResponsesStreamEvent{Type: "response.output_item.done", Item: &ResponsesOutput{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "functions__exec", Arguments: `{"input":"hello"}`, Status: "completed"}})
	require.Len(t, done, 1)
	require.Equal(t, added[0].Item.ID, done[0].Item.ID)
	require.Equal(t, "functions", done[0].Item.Namespace)
	require.Equal(t, "hello", done[0].Item.Input)
	output := []ResponsesOutput{{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "functions__exec", Arguments: `{"input":"hello"}`}}
	restoreResponsesOutputClientTools(output, &mapping)
	require.Equal(t, "functions", output[0].Namespace)
	require.Equal(t, "exec", output[0].Name)
	payload, _, err := RestoreResponsesClientToolPayload([]byte(`{"output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"functions__exec","arguments":"{\"input\":\"hello\"}"}]}`), mapping)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"namespace":"functions"`)
}
