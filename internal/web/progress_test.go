package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestWebProgressPreservesTraceOrderAcrossModelAndLocalTools(t *testing.T) {
	ctx := context.Background()
	progress := newWebProgress()
	turn := &agent.TurnState{Index: 1}

	progress.OnLLMDelta(ctx, turn, llm.StreamEvent{ReasoningDelta: "first", Trace: &llm.TraceEvent{
		Phase: "delta", Sequence: 1, Delta: "first", Step: llm.TraceStep{ID: "rs_1", Kind: "reasoning", Status: "running"},
	}})
	progress.OnLLMDelta(ctx, turn, llm.StreamEvent{Trace: &llm.TraceEvent{
		Phase: "started", Sequence: 2, Step: llm.TraceStep{ID: "ws_1", Kind: "tool", ToolKind: "hosted", Name: "web_search", Status: "running"},
	}})
	progress.OnLLMDelta(ctx, turn, llm.StreamEvent{Trace: &llm.TraceEvent{
		Phase: "completed", Sequence: 3, Step: llm.TraceStep{ID: "ws_1", Kind: "tool", ToolKind: "hosted", Name: "web_search", Status: "completed", Detail: map[string]any{"query": "example"}},
	}})
	progress.OnLLMDelta(ctx, turn, llm.StreamEvent{ReasoningDelta: "second", Trace: &llm.TraceEvent{
		Phase: "delta", Sequence: 4, Delta: "second", Step: llm.TraceStep{ID: "rs_2", Kind: "reasoning", Status: "running"},
	}})
	call := llm.ToolCall{ID: "call_1", Type: "function", Function: llm.ToolCallFn{Name: "lookup", Arguments: `{"q":"x"}`}}
	if err := progress.OnToolCalls(ctx, turn, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}}); err != nil {
		t.Fatalf("tool calls: %v", err)
	}
	if err := progress.OnToolResults(ctx, turn, []agent.StoredMessage{{
		EntryID:  1,
		Message:  llm.Message{Role: "tool", ToolCallID: call.ID, Content: "model result"},
		Metadata: &memory.MessageMetadata{ToolCallID: call.ID, Status: "ok", DisplayContent: "display result", StructuredContent: map[string]any{"ok": true}},
	}}); err != nil {
		t.Fatalf("tool results: %v", err)
	}

	wantNames := []string{"reasoning.started", "reasoning.delta", "tool.started", "tool.completed", "reasoning.started", "reasoning.delta", "reasoning.completed", "tool.started", "tool.completed"}
	var gotNames []string
	var lastSequence int64
	for range wantNames {
		event := <-progress.events
		gotNames = append(gotNames, event.Name)
		data, ok := event.Data.(map[string]any)
		if !ok {
			t.Fatalf("unexpected event data: %#v", event.Data)
		}
		sequence, ok := data["sequence"].(int64)
		if !ok || sequence <= lastSequence {
			t.Fatalf("sequence is not monotonic: %#v", data)
		}
		lastSequence = sequence
	}
	if fmt.Sprint(gotNames) != fmt.Sprint(wantNames) {
		t.Fatalf("unexpected event order: %v", gotNames)
	}

	_, _, trace := progress.partial()
	if len(trace) != 4 || trace[0].ID != "rs_1" || trace[1].ID != "ws_1" || trace[2].ID != "rs_2" || trace[3].ID != "call_1" {
		t.Fatalf("unexpected trace order: %+v", trace)
	}
	if trace[3].Result != "display result" || trace[3].Status != "completed" || trace[3].Detail == nil {
		t.Fatalf("tool result was not merged: %+v", trace[3])
	}
}

func TestWebProgressLegacyReasoningCreatesSeparateStepsPerTurn(t *testing.T) {
	progress := newWebProgress()
	ctx := context.Background()
	progress.OnLLMDelta(ctx, &agent.TurnState{Index: 1}, llm.StreamEvent{ReasoningDelta: "one"})
	_ = progress.OnToolCalls(ctx, &agent.TurnState{Index: 1}, llm.Message{ToolCalls: []llm.ToolCall{{ID: "call", Function: llm.ToolCallFn{Name: "tool", Arguments: `{}`}}}})
	progress.OnLLMDelta(ctx, &agent.TurnState{Index: 2}, llm.StreamEvent{ReasoningDelta: "two"})

	_, reasoning, trace := progress.partial()
	if reasoning != "onetwo" || len(trace) != 3 {
		t.Fatalf("unexpected legacy trace: reasoning=%q trace=%+v", reasoning, trace)
	}
	if trace[0].ID != "turn-1-reasoning" || trace[2].ID != "turn-2-reasoning" {
		t.Fatalf("reasoning turns were merged: %+v", trace)
	}
}
