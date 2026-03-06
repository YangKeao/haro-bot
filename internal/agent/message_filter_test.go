package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestToLLMMessagesForContextDropsUnpairedToolCalls(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read",
			Arguments: `{"path":"a"}`,
		},
	}}
	msgs := []memory.Message{
		{Role: "assistant", Content: "", Metadata: &memory.MessageMetadata{ToolCalls: toolCalls}},
		{Role: "assistant", Content: "final", Metadata: nil},
	}
	out := toLLMMessagesForContext(msgs)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if out[0].Content != "final" {
		t.Fatalf("unexpected content: %q", out[0].Content)
	}
}

func TestToLLMMessagesForContextKeepsPairedToolCalls(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read",
			Arguments: `{"path":"a"}`,
		},
	}}
	msgs := []memory.Message{
		{Role: "assistant", Content: "", Metadata: &memory.MessageMetadata{ToolCalls: toolCalls}},
		{Role: "tool", Content: "ok", Metadata: &memory.MessageMetadata{ToolCallID: "call-1", Status: "ok"}},
		{Role: "assistant", Content: "final", Metadata: nil},
	}
	out := toLLMMessagesForContext(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if len(out[0].ToolCalls) != 1 || out[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("expected tool call kept, got %+v", out[0].ToolCalls)
	}
	if out[1].ToolCallID != "call-1" {
		t.Fatalf("expected tool output kept, got %q", out[1].ToolCallID)
	}
}
