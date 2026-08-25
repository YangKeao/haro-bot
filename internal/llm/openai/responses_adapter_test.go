package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestResponsesModeMapsMessagesToolsAndStream(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeResponseEvent(t, w, "response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "delta": "Checked sources. ",
			"item_id": "rs_1", "output_index": 0, "summary_index": 0, "sequence_number": 1,
		})
		writeResponseEvent(t, w, "response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "delta": "Result with citation.",
			"item_id": "msg_1", "output_index": 1, "content_index": 0, "sequence_number": 2, "logprobs": []any{},
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 1, "sequence_number": 3,
			"item": map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "Result with citation.", "annotations": []any{}, "logprobs": []any{}}},
			},
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 2, "sequence_number": 4,
			"item": map[string]any{
				"id": "fc_2", "type": "function_call", "status": "completed",
				"call_id": "call_2", "name": "save_result", "arguments": `{"ok":true}`,
			},
		})
		writeResponseEvent(t, w, "response.completed", map[string]any{
			"type": "response.completed", "sequence_number": 5,
			"response": map[string]any{
				"id": "resp_1", "object": "response", "created_at": 123, "status": "completed",
				"model": "gpt-5.6-luna", "output": []any{},
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14, "input_tokens_details": map[string]any{}, "output_tokens_details": map[string]any{}},
			},
		})
	}))
	defer server.Close()

	var deltas []llm.StreamEvent
	client := New(server.URL+"/v1", "test-key", WithAPIMode("responses"))
	result, err := client.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-5.6-luna",
		Messages: []llm.Message{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Inspect this", Images: []llm.ImageContent{{URL: "data:image/png;base64,AQID"}}},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.ToolCallFn{Name: "lookup", Arguments: `{}`}}}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"value":1}`},
		},
		Tools: []llm.Tool{{Type: "function", Function: llm.FunctionSpec{
			Name: "save_result", Description: "Save the result", Parameters: map[string]any{"type": "object"},
		}}},
		ReasoningEnabled: true,
		ReasoningEffort:  "high",
		HostedWebSearch:  true,
		StreamHandler:    func(event llm.StreamEvent) { deltas = append(deltas, event) },
	})
	if err != nil {
		t.Fatalf("responses chat failed: %v", err)
	}
	message := result.Choices[0].Message
	if message.Content != "Result with citation." || message.ReasoningContent != "Checked sources. " {
		t.Fatalf("unexpected message: %+v", message)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call_2" {
		t.Fatalf("unexpected tool calls: %+v", message.ToolCalls)
	}
	if result.Usage.TotalTokens != 14 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
	if len(deltas) != 3 || deltas[0].ReasoningDelta == "" || deltas[1].Delta == "" || deltas[2].Trace == nil {
		t.Fatalf("unexpected stream deltas: %+v", deltas)
	}
	if len(message.TraceSteps) != 2 || message.TraceSteps[0].Kind != "reasoning" || message.TraceSteps[1].ID != "call_2" {
		t.Fatalf("unexpected trace steps: %+v", message.TraceSteps)
	}

	tools, _ := requestBody["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected function and web search tools, got %#v", requestBody["tools"])
	}
	webTool, _ := tools[1].(map[string]any)
	if webTool["type"] != "web_search_preview" {
		t.Fatalf("unexpected web search tool: %#v", webTool)
	}
	include, _ := requestBody["include"].([]any)
	if len(include) != 1 || include[0] != "web_search_call.action.sources" {
		t.Fatalf("unexpected include: %#v", requestBody["include"])
	}
	input, _ := requestBody["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("expected replayed messages and tool items, got %#v", input)
	}
	if requestBody["store"] != false {
		t.Fatalf("responses requests must be stateless: %#v", requestBody["store"])
	}
}

func TestResponsesModePreservesReasoningAndHostedToolOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeResponseEvent(t, w, "response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "delta": "First thought.",
			"item_id": "rs_1", "output_index": 0, "summary_index": 0, "sequence_number": 1,
		})
		writeResponseEvent(t, w, "response.reasoning_summary_text.done", map[string]any{
			"type": "response.reasoning_summary_text.done", "text": "First thought.",
			"item_id": "rs_1", "output_index": 0, "summary_index": 0, "sequence_number": 2,
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 0, "sequence_number": 3,
			"item": map[string]any{"id": "rs_1", "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": "First thought."}}},
		})
		writeResponseEvent(t, w, "response.web_search_call.in_progress", map[string]any{
			"type": "response.web_search_call.in_progress", "item_id": "ws_1", "output_index": 1, "sequence_number": 4,
		})
		writeResponseEvent(t, w, "response.web_search_call.searching", map[string]any{
			"type": "response.web_search_call.searching", "item_id": "ws_1", "output_index": 1, "sequence_number": 5,
		})
		writeResponseEvent(t, w, "response.web_search_call.completed", map[string]any{
			"type": "response.web_search_call.completed", "item_id": "ws_1", "output_index": 1, "sequence_number": 6,
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 1, "sequence_number": 7,
			"item": map[string]any{
				"id": "ws_1", "type": "web_search_call", "status": "completed",
				"action": map[string]any{"type": "search", "query": "OpenAI title", "sources": []any{map[string]any{"title": "OpenAI", "url": "https://openai.com/"}}},
			},
		})
		writeResponseEvent(t, w, "response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "delta": "Second thought.",
			"item_id": "rs_2", "output_index": 2, "summary_index": 0, "sequence_number": 8,
		})
		writeResponseEvent(t, w, "response.reasoning_summary_text.done", map[string]any{
			"type": "response.reasoning_summary_text.done", "text": "Second thought.",
			"item_id": "rs_2", "output_index": 2, "summary_index": 0, "sequence_number": 9,
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 2, "sequence_number": 10,
			"item": map[string]any{"id": "rs_2", "type": "reasoning", "status": "completed", "summary": []any{map[string]any{"type": "summary_text", "text": "Second thought."}}},
		})
		writeResponseEvent(t, w, "response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "delta": "Final answer.", "item_id": "msg_1", "output_index": 3, "content_index": 0, "sequence_number": 11, "logprobs": []any{},
		})
		writeResponseEvent(t, w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 3, "sequence_number": 12,
			"item": map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Final answer.", "annotations": []any{}, "logprobs": []any{}}}},
		})
		writeResponseEvent(t, w, "response.completed", map[string]any{
			"type": "response.completed", "sequence_number": 13,
			"response": map[string]any{"id": "resp_trace", "object": "response", "created_at": 123, "status": "completed", "model": "gpt-5.6-luna", "output": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2, "input_tokens_details": map[string]any{}, "output_tokens_details": map[string]any{}}},
		})
	}))
	defer server.Close()

	var events []llm.StreamEvent
	client := New(server.URL+"/v1", "test-key", WithAPIMode("responses"))
	result, err := client.Chat(context.Background(), llm.ChatRequest{Model: "gpt-5.6-luna", Messages: []llm.Message{{Role: "user", Content: "search"}}, StreamHandler: func(event llm.StreamEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatalf("responses chat failed: %v", err)
	}
	message := result.Choices[0].Message
	if message.Content != "Final answer." || message.ReasoningContent != "First thought.Second thought." {
		t.Fatalf("unexpected message: %+v", message)
	}
	if len(message.TraceSteps) != 3 || message.TraceSteps[0].ID != "rs_1" || message.TraceSteps[1].ID != "ws_1" || message.TraceSteps[2].ID != "rs_2" {
		t.Fatalf("trace order was not preserved: %+v", message.TraceSteps)
	}
	if message.TraceSteps[1].ToolKind != "hosted" || message.TraceSteps[1].Detail == nil {
		t.Fatalf("hosted tool detail missing: %+v", message.TraceSteps[1])
	}
	var firstSeen []string
	seen := map[string]bool{}
	for _, event := range events {
		if event.Trace == nil || seen[event.Trace.Step.ID] {
			continue
		}
		seen[event.Trace.Step.ID] = true
		firstSeen = append(firstSeen, event.Trace.Step.ID)
	}
	if fmt.Sprint(firstSeen) != "[rs_1 ws_1 rs_2]" {
		t.Fatalf("unexpected live trace order: %v", firstSeen)
	}
}

func writeResponseEvent(t *testing.T, w http.ResponseWriter, event string, data any) {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
}
