package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestSummaryHintToolPhase(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "exec_command",
			Arguments: `{"cmd":"echo ok","workdir":"/tmp"}`,
		},
	}}
	msgs := []memory.Message{
		msg(1, "assistant", "tooling", &memory.MessageMetadata{ToolCalls: toolCalls}),
		msg(2, "tool", "ok", &memory.MessageMetadata{ToolCallID: "call-1", Status: "ok"}),
		msg(3, "assistant", "done", nil),
		msg(4, "user", "new task", nil),
	}
	hint := summaryHint(msgs, summaryUsage{})
	if hint == "" || !strings.Contains(hint, "tool-driven phase") {
		t.Fatalf("expected tool phase hint, got %q", hint)
	}
}

func TestSummaryHintToolErrors(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read_file",
			Arguments: `{"file_path":"/tmp/a"}`,
		},
	}, {
		ID:   "call-2",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "exec_command",
			Arguments: `{"cmd":"./run.sh","workdir":"/tmp"}`,
		},
	}}
	msgs := []memory.Message{
		msg(1, "assistant", "tooling", &memory.MessageMetadata{ToolCalls: toolCalls}),
		msg(2, "tool", "err1", &memory.MessageMetadata{ToolCallID: "call-1", Status: "error"}),
		msg(3, "tool", "err2", &memory.MessageMetadata{ToolCallID: "call-2", Status: "error"}),
		msg(4, "assistant", "failed", nil),
		msg(5, "user", "retry", nil),
	}
	hint := summaryHint(msgs, summaryUsage{})
	if hint == "" || !strings.Contains(hint, "multiple tool errors occurred") {
		t.Fatalf("expected tool error hint, got %q", hint)
	}
}

func TestSummaryHintNearLimitCritical(t *testing.T) {
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
	hint := summaryHint(msgs, summaryUsage{TokensUsed: 95, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "critical") {
		t.Fatalf("expected critical limit hint, got %q", hint)
	}
}

func TestSummaryHintNearLimitHigh(t *testing.T) {
	var msgs []memory.Message
	for i := 1; i <= 17; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		msgs = append(msgs, msg(int64(i), role, "x", nil))
	}
	hint := summaryHint(msgs, summaryUsage{TokensUsed: 86, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "tight") {
		t.Fatalf("expected high limit hint, got %q", hint)
	}
}

func TestSummaryHintNearLimitMediumWithTooling(t *testing.T) {
	toolCalls := []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "read_file",
			Arguments: `{"file_path":"/tmp/a"}`,
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
	hint := summaryHint(msgs, summaryUsage{TokensUsed: 75, TokenBudget: 100})
	if hint == "" || !strings.Contains(hint, "getting long") {
		t.Fatalf("expected medium limit hint, got %q", hint)
	}
}

func msg(id int64, role, content string, meta *memory.MessageMetadata) memory.Message {
	return memory.Message{ID: id, Role: role, Content: content, Metadata: meta}
}
