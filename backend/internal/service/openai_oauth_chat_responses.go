package service

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func captureOAuthChatNativeResponse(c *gin.Context, payload string) {
	if bridge, ok := c.Writer.(*oauthChatBridgeWriter); ok && !bridge.anthropic {
		bridge.nativeResponse = json.RawMessage(gjson.Get(payload, "response").Raw)
	}
}

// Keep Responses history and options intact. Only client tool definitions,
// calls and results need the same lowering as a Chat relay.
func prepareOAuthChatResponses(body []byte, model string) ([]byte, *oauthChatResponsesRestorer, error) {
	var request map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &request); err != nil {
		return nil, nil, err
	}
	tools, _ := request["tools"].([]any)
	if input, ok := request["input"].([]any); ok {
		kept := make([]any, 0, len(input))
		for _, raw := range input {
			item, _ := raw.(map[string]any)
			if item["type"] == "additional_tools" {
				additional, ok := item["tools"].([]any)
				if !ok {
					return nil, nil, fmt.Errorf("additional_tools.tools must be an array")
				}
				tools = append(tools, additional...)
			} else {
				kept = append(kept, raw)
			}
		}
		request["input"] = kept
	}
	request["tools"] = tools
	r := newOAuthChatResponsesRestorer(model, nil)
	r.functions = make(map[string]bool)
	if len(tools) == 0 {
		// A native discovery continuation can carry definitions solely in
		// tool_search_output. Keep that contract when no tools were declared.
		return body, r, nil
	}
	var search map[string]any
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if tool["type"] == "function" {
			if name, ok := tool["name"].(string); ok {
				r.functions[name] = true
			}
		}
		if tool["type"] == "tool_search" {
			if tool["execution"] != "client" {
				// Hosted discovery must retain native namespaces and deferred tools.
				return body, r, nil
			}
			search = tool
		}
	}
	mapping, _, err := apicompat.AdaptResponsesClientTools(request)
	if err != nil {
		return nil, nil, err
	}
	r.custom = mapping.CustomTools
	r.toolSearch = mapping.ToolSearch
	r.namespaces = make(map[string]apicompat.NamespacedToolName, len(mapping.NamespaceTools))
	for name, ns := range mapping.NamespaceTools {
		r.namespaces[name] = apicompat.NamespacedToolName{Namespace: ns.Namespace, Name: ns.Name}
	}
	if lowered, ok := request["tools"].([]any); ok {
		for _, raw := range lowered {
			tool, _ := raw.(map[string]any)
			if search != nil && tool["name"] == "tool_search" {
				for _, key := range []string{"description", "parameters", "strict"} {
					if value, exists := search[key]; exists {
						tool[key] = value
					}
				}
			}
		}
	}
	encoded, err := json.Marshal(request)
	return encoded, r, err
}

// OAuth still speaks Responses upstream. Preserve those native events across
// the internal Chat boundary, adapting only the tools that Chat renamed.
type oauthChatResponsesRestorer struct {
	model      string
	custom     map[string]bool
	functions  map[string]bool
	namespaces map[string]apicompat.NamespacedToolName
	toolSearch bool
	tools      map[string]*oauthChatNativeTool
	sequence   int
	terminal   bool
}

type oauthChatNativeTool struct {
	item      map[string]any
	arguments string
	inputDone bool
}

func newOAuthChatResponsesRestorer(model string, tools []apicompat.ResponsesTool) *oauthChatResponsesRestorer {
	return &oauthChatResponsesRestorer{model: model, custom: apicompat.CustomToolNames(tools), functions: apicompat.FunctionToolNames(tools), namespaces: apicompat.NamespaceToolNames(tools), toolSearch: apicompat.HasToolSearchTool(tools), tools: make(map[string]*oauthChatNativeTool)}
}

func (r *oauthChatResponsesRestorer) restoreItem(item map[string]any) {
	if item["type"] != "function_call" {
		return
	}
	name, _ := item["name"].(string)
	args, _ := item["arguments"].(string)
	callID, _ := item["call_id"].(string)
	cc := &apicompat.ChatCompletionsResponse{Choices: []apicompat.ChatChoice{{Message: apicompat.ChatMessage{ToolCalls: []apicompat.ChatToolCall{{ID: callID, Function: apicompat.ChatFunctionCall{Name: name, Arguments: args}}}}}}}
	converted := apicompat.ChatCompletionsResponseToResponses(cc, r.model, r.custom, r.functions, r.toolSearch, r.namespaces)
	if len(converted.Output) != 1 {
		return
	}
	out := converted.Output[0]
	item["type"] = out.Type
	item["name"] = out.Name
	if out.Namespace != "" {
		item["namespace"] = out.Namespace
	}
	switch out.Type {
	case "custom_tool_call":
		delete(item, "arguments")
		item["input"] = out.Input
	case "tool_search_call":
		delete(item, "name")
		item["execution"] = "client"
		var value any
		if json.Unmarshal([]byte(out.Arguments), &value) == nil {
			item["arguments"] = value
		} else {
			item["arguments"] = out.Arguments
		}
	}
}

func (r *oauthChatResponsesRestorer) restoreResponse(response map[string]any) {
	response["model"] = r.model
	if output, ok := response["output"].([]any); ok {
		for _, raw := range output {
			if item, ok := raw.(map[string]any); ok {
				r.restoreItem(item)
			}
		}
	}
}

func (r *oauthChatResponsesRestorer) event(raw []byte) ([]byte, error) {
	var event map[string]any
	if err := decodeOpenAIJSONUseNumber(raw, &event); err != nil {
		return nil, err
	}
	var events []map[string]any
	kind, _ := event["type"].(string)
	if item, ok := event["item"].(map[string]any); ok && item["type"] == "function_call" {
		id, _ := item["id"].(string)
		args, _ := item["arguments"].(string)
		tool := r.tools[id]
		if tool == nil {
			tool = &oauthChatNativeTool{}
			r.tools[id] = tool
		}
		if kind == "response.output_item.done" && args == "" && tool.arguments != "" {
			args = tool.arguments
			item["arguments"] = args
		}
		r.restoreItem(item)
		if kind == "response.output_item.added" && item["type"] == "custom_tool_call" {
			item["input"] = ""
		}
		tool.item = item
		if args != "" {
			tool.arguments = args
		}
		if kind == "response.output_item.done" && item["type"] == "custom_tool_call" && !tool.inputDone {
			events = append(events, r.customInputEvents(tool, id, event["output_index"])...)
		}
	}
	if kind == "response.function_call_arguments.delta" || kind == "response.function_call_arguments.done" {
		id, _ := event["item_id"].(string)
		if tool := r.tools[id]; tool != nil {
			if kind == "response.function_call_arguments.delta" {
				delta, _ := event["delta"].(string)
				tool.arguments += delta
			} else if args, ok := event["arguments"].(string); ok {
				tool.arguments = args
			}
			switch tool.item["type"] {
			case "custom_tool_call":
				if kind == "response.function_call_arguments.done" {
					events = append(events, r.customInputEvents(tool, id, event["output_index"])...)
				}
				return r.encode(events)
			case "tool_search_call":
				return nil, nil
			default:
				if _, exists := event["name"]; exists {
					event["name"] = tool.item["name"]
				}
			}
		}
	}
	if response, ok := event["response"].(map[string]any); ok {
		r.restoreResponse(response)
	}
	if kind == "response.completed" || kind == "response.done" || kind == "response.incomplete" || kind == "response.failed" {
		r.terminal = true
		if kind == "response.done" {
			status := "completed"
			if response, ok := event["response"].(map[string]any); ok {
				if s, ok := response["status"].(string); ok && (s == "incomplete" || s == "failed") {
					status = s
				}
			}
			event["type"] = "response." + status
		}
	}
	events = append(events, event)
	return r.encode(events)
}

func (r *oauthChatResponsesRestorer) customInputEvents(tool *oauthChatNativeTool, id string, index any) []map[string]any {
	if tool.inputDone {
		return nil
	}
	tool.inputDone = true
	// The stored item already has its client identity. Parse the wrapper without
	// resolving the flattened name a second time.
	input := tool.arguments
	var wrapper struct {
		Input *string `json:"input"`
	}
	if json.Unmarshal([]byte(tool.arguments), &wrapper) == nil && wrapper.Input != nil {
		input = *wrapper.Input
	}
	if input == "{}" {
		input = ""
	}
	tool.item["input"] = input
	var events []map[string]any
	if input != "" {
		events = append(events, map[string]any{"type": "response.custom_tool_call_input.delta", "item_id": id, "output_index": index, "delta": input})
	}
	events = append(events, map[string]any{"type": "response.custom_tool_call_input.done", "item_id": id, "output_index": index, "call_id": tool.item["call_id"], "name": tool.item["name"], "input": input})
	return events
}

func (r *oauthChatResponsesRestorer) encode(events []map[string]any) ([]byte, error) {
	var result []byte
	for _, event := range events {
		event["sequence_number"] = r.sequence
		r.sequence++
		data, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		result = append(result, []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event["type"], data))...)
	}
	return result, nil
}
