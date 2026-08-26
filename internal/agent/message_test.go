package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestToLLMMessagesRestoresToolMetadata(t *testing.T) {
	toolCalls := []llm.ToolCall{
		{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test", Arguments: `{"x":1}`}},
	}
	meta := &memory.MessageMetadata{
		ToolCallID: "call-1",
		ToolCalls:  toolCalls,
	}
	msgs := []memory.Message{
		{Role: "assistant", Content: "tooling", Metadata: meta},
		{Role: "tool", Content: "ok", Metadata: meta},
	}
	llmMsgs := make([]llm.Message, 0, len(msgs))
	for _, msg := range msgs {
		llmMsgs = append(llmMsgs, toLLMMessage(msg))
	}
	if len(llmMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(llmMsgs))
	}
	if got := llmMsgs[0].ToolCalls; len(got) != 1 || got[0].ID != "call-1" {
		t.Fatalf("expected tool_calls to roundtrip, got %+v", got)
	}
	if llmMsgs[1].ToolCallID != "call-1" {
		t.Fatalf("expected tool_call_id to roundtrip, got %q", llmMsgs[1].ToolCallID)
	}
}

func TestToLLMMessageAddsOpaqueAttachmentManifest(t *testing.T) {
	msg := toLLMMessage(memory.Message{
		Role: "user", Content: "inspect this",
		Attachments: []memory.AttachmentContent{{ID: "abc123", Name: "food data.zip", MIMEType: "application/zip", SizeBytes: 42, SHA256: "deadbeef"}},
	})
	if !strings.Contains(msg.Content, "attachment://abc123/food%20data.zip") {
		t.Fatalf("expected opaque attachment URI, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "untrusted user data") {
		t.Fatalf("expected untrusted-data warning, got %q", msg.Content)
	}
	if len(msg.Images) != 0 {
		t.Fatalf("non-image attachment must not become an image: %+v", msg.Images)
	}
}
