package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestAnchorHintToolPhase(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "write",
			Arguments: `{"path":"a","content":"b"}`,
		},
	}}
	msgs := []memory.Message{
		msg(1, "assistant", "tooling", &memory.MessageMetadata{ToolCalls: toolCalls}),
		msg(2, "tool", "ok", &memory.MessageMetadata{ToolCallID: "call-1", Status: "ok"}),
		msg(3, "assistant", "done", nil),
		msg(4, "user", "new task", nil),
	}
	hint := anchorHint(msgs, anchorUsage{})
	if hint == "" || !strings.Contains(hint, "tool-driven phase") {
		t.Fatalf("expected tool phase hint, got %q", hint)
	}
}

func TestAnchorHintToolErrors(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read",
			Arguments: `{"path":"a"}`,
		},
	}, {
		ID:   "call-2",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "exec",
			Arguments: `{"path":"./run.sh"}`,
		},
	}}
	msgs := []memory.Message{
		msg(1, "assistant", "tooling", &memory.MessageMetadata{ToolCalls: toolCalls}),
		msg(2, "tool", "err1", &memory.MessageMetadata{ToolCallID: "call-1", Status: "error"}),
		msg(3, "tool", "err2", &memory.MessageMetadata{ToolCallID: "call-2", Status: "error"}),
		msg(4, "assistant", "failed", nil),
		msg(5, "user", "retry", nil),
	}
	hint := anchorHint(msgs, anchorUsage{})
	if hint == "" || !strings.Contains(hint, "multiple tool errors occurred") {
		t.Fatalf("expected tool error hint, got %q", hint)
	}
}

func TestAnchorHintNearLimitCritical(t *testing.T) {
	msgs := []memory.Message{
		msg(1, "assistant", "a", nil),
		msg(2, "user", "b", nil),
		msg(3, "assistant", "c", nil),
		msg(4, "user", "d", nil),
		msg(5, "assistant", "e", nil),
		msg(6, "user", "f", nil),
		msg(7, "assistant", "g", nil),
		msg(8, "user", "h", nil),
		msg(9, "assistant", "i", nil),
		msg(10, "user", "j", nil),
	}
	hint := anchorHint(msgs, anchorUsage{TokensUsed: 95, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "critical") {
		t.Fatalf("expected critical limit hint, got %q", hint)
	}
}

func TestAnchorHintNearLimitHigh(t *testing.T) {
	var msgs []memory.Message
	for i := 1; i <= 17; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		msgs = append(msgs, msg(int64(i), role, "x", nil))
	}
	hint := anchorHint(msgs, anchorUsage{TokensUsed: 86, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "tight") {
		t.Fatalf("expected high limit hint, got %q", hint)
	}
}

func TestAnchorHintNearLimitMediumWithTooling(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read",
			Arguments: `{"path":"a"}`,
		},
	}}
	msgs := []memory.Message{
		msg(1, "assistant", "tooling", &memory.MessageMetadata{ToolCalls: toolCalls}),
		msg(2, "tool", "ok", &memory.MessageMetadata{ToolCallID: "call-1", Status: "ok"}),
		msg(3, "assistant", "done", nil),
		msg(4, "user", "new task", nil),
		msg(5, "assistant", "follow", nil),
		msg(6, "user", "more", nil),
		msg(7, "assistant", "more", nil),
		msg(8, "user", "more", nil),
		msg(9, "assistant", "more", nil),
		msg(10, "user", "more", nil),
	}
	hint := anchorHint(msgs, anchorUsage{TokensUsed: 75, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "getting long") {
		t.Fatalf("expected medium limit hint, got %q", hint)
	}
}

func msg(id int64, role, content string, meta *memory.MessageMetadata) memory.Message {
	return memory.Message{ID: id, Role: role, Content: content, Metadata: meta}
}
