package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatBridgeNamespacedCustomToolRoundTrip(t *testing.T) {
	for _, placement := range []string{"tools", "children", "additional_tools"} {
		t.Run(placement, func(t *testing.T) {
			tool := ResponsesTool{Type: "namespace", Name: "functions", Tools: []ResponsesTool{
				{Type: "custom", Name: "exec", Description: "Execute JavaScript"},
				{Type: "function", Name: "wait", Parameters: json.RawMessage(`{"type":"object"}`)},
			}}
			if placement == "children" {
				tool.Children, tool.Tools = tool.Tools, nil
			}
			req := &ResponsesRequest{Model: "test", Input: json.RawMessage(`"inspect files"`), Tools: []ResponsesTool{tool}, ToolChoice: json.RawMessage(`{"type":"custom","namespace":"functions","name":"exec"}`)}
			if placement == "additional_tools" {
				req.Input, _ = json.Marshal([]any{map[string]any{"type": "additional_tools", "tools": req.Tools}, map[string]any{"role": "user", "content": "inspect files"}})
				req.Tools = nil
			}
			tools, err := EffectiveResponsesTools(req)
			require.NoError(t, err)
			chat, err := ResponsesToChatCompletionsRequest(req)
			require.NoError(t, err)
			require.Len(t, chat.Tools, 2, "the executable tool must survive conversion")
			require.Equal(t, "functions__exec", chat.Tools[0].Function.Name)
			require.JSONEq(t, customToolInputSchema, string(chat.Tools[0].Function.Parameters))
			require.JSONEq(t, `{"type":"function","function":{"name":"functions__exec"}}`, string(chat.ToolChoice))
			upstream, err := ChatCompletionsToResponses(chat)
			require.NoError(t, err)
			require.Equal(t, "functions__exec", upstream.Tools[0].Name)

			call := ChatToolCall{ID: "call_exec", Type: "function", Function: ChatFunctionCall{Name: "functions__exec", Arguments: `{"input":"await tools.exec_command({cmd: 'Get-Location'})"}`}}
			custom, functions, namespaces := CustomToolNames(tools), FunctionToolNames(tools), NamespaceToolNames(tools)
			result := ChatCompletionsResponseToResponses(&ChatCompletionsResponse{Choices: []ChatChoice{{Message: ChatMessage{ToolCalls: []ChatToolCall{call}}}}}, req.Model, custom, functions, false, namespaces)
			require.Len(t, result.Output, 1)
			assertCall := func(item ResponsesOutput) {
				t.Helper()
				require.Equal(t, "custom_tool_call", item.Type)
				require.Equal(t, "functions", item.Namespace)
				require.Equal(t, "exec", item.Name)
				require.Equal(t, "call_exec", item.CallID)
				require.Equal(t, "await tools.exec_command({cmd: 'Get-Location'})", item.Input)
			}
			assertCall(result.Output[0])

			state := NewChatCompletionsToResponsesStreamState(req.Model)
			state.CustomTools, state.FunctionTools, state.NamespaceTools = custom, functions, namespaces
			index := 0
			call.Index = &index
			events := ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{call}}}}}, state)
			events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
			var addedID, doneID string
			var gotInputDone, gotCompleted bool
			for _, event := range events {
				if event.Item != nil && event.Item.Type == "custom_tool_call" {
					wire, err := json.Marshal(event)
					require.NoError(t, err)
					var decoded struct{ Item ResponsesOutput }
					require.NoError(t, json.Unmarshal(wire, &decoded))
					require.Equal(t, "functions", decoded.Item.Namespace, "wire output must preserve routing namespace")
					require.Equal(t, "exec", decoded.Item.Name)
					if event.Type == "response.output_item.added" {
						addedID = event.Item.ID
					}
					if event.Type == "response.output_item.done" {
						doneID = event.Item.ID
						assertCall(decoded.Item)
					}
				}
				if event.Type == "response.custom_tool_call_input.done" {
					gotInputDone = true
					require.Equal(t, result.Output[0].Input, event.Input)
				}
				if event.Type == "response.completed" {
					gotCompleted = true
					require.Len(t, event.Response.Output, 1)
					assertCall(event.Response.Output[0])
					require.Equal(t, addedID, event.Response.Output[0].ID)
				}
			}
			require.NotEmpty(t, addedID)
			require.Equal(t, addedID, doneID)
			require.True(t, gotInputDone)
			require.True(t, gotCompleted)

			req.Input, _ = json.Marshal([]any{result.Output[0], map[string]any{"type": "custom_tool_call_output", "call_id": "call_exec", "output": "D:\\workspace"}})
			req.Tools = tools
			next, err := ResponsesToChatCompletionsRequest(req)
			require.NoError(t, err)
			require.Len(t, next.Messages, 2)
			require.Equal(t, "functions__exec", next.Messages[0].ToolCalls[0].Function.Name)
			require.Equal(t, "call_exec", next.Messages[1].ToolCallID)
		})
	}
}

func TestChatBridgeTerminalToolItemIdentity(t *testing.T) {
	for _, kind := range []string{"function", "custom", "tool_search"} {
		t.Run(kind, func(t *testing.T) {
			state := NewChatCompletionsToResponsesStreamState("test")
			name := "inspect"
			if kind == "custom" {
				state.CustomTools = map[string]bool{name: true}
			}
			if kind == "tool_search" {
				name = "tool_search"
				state.ToolSearchDeclared = true
			}
			index := 0
			events := ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{Index: &index, ID: "call_1", Function: ChatFunctionCall{Name: name, Arguments: `{"input":"pwd"}`}}}}}}}, state)
			events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
			var itemID string
			for _, event := range events {
				if event.Type == "response.output_item.added" {
					itemID = event.Item.ID
				}
				if event.Type == "response.completed" {
					require.NotEmpty(t, itemID)
					require.Len(t, event.Response.Output, 1)
					require.Equal(t, itemID, event.Response.Output[0].ID)
				}
			}
		})
	}
}

func TestChatBridgeTerminalOutputMatchesStreamIndices(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("test")
	index := 0
	text := "I inspected the directory."
	var events []ResponsesStreamEvent
	for _, delta := range []ChatDelta{
		{ReasoningContent: stringPtr("I should inspect the directory.")},
		{ToolCalls: []ChatToolCall{{Index: &index, ID: "call_1", Function: ChatFunctionCall{Name: "inspect", Arguments: "{}"}}}},
		{Content: &text},
	} {
		events = append(events, ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: delta}}}, state)...)
	}
	events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
	added := map[int]string{}
	for _, event := range events {
		if event.Type == "response.output_item.added" {
			added[event.OutputIndex] = event.Item.ID
		}
		if event.Type == "response.output_item.done" {
			require.Equal(t, added[event.OutputIndex], event.Item.ID)
		}
		if event.Type == "response.completed" {
			require.Len(t, event.Response.Output, len(added))
			for index, item := range event.Response.Output {
				require.Equal(t, added[index], item.ID, "terminal snapshot must retain the streamed identity and order")
			}
		}
	}
}
