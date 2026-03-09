package memory

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestMemoryItem(t *testing.T) {
	item := MemoryItem{
		Type:    "fact",
		Content: "User likes Go programming",
		Score:   0.85,
	}

	if item.Type != "fact" {
		t.Errorf("expected type 'fact', got %s", item.Type)
	}
	if item.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", item.Score)
	}
}

func TestMessageMetadata(t *testing.T) {
	t.Run("empty metadata", func(t *testing.T) {
		var meta MessageMetadata
		if meta.ToolCallID != "" {
			t.Error("expected empty ToolCallID")
		}
		if len(meta.ToolCalls) != 0 {
			t.Error("expected empty ToolCalls")
		}
	})

	t.Run("with tool calls", func(t *testing.T) {
		meta := &MessageMetadata{
			ToolCalls: []llm.ToolCall{
				{ID: "call1", Function: llm.ToolCallFn{Name: "test_tool"}},
			},
		}
		if len(meta.ToolCalls) != 1 {
			t.Errorf("expected 1 tool call, got %d", len(meta.ToolCalls))
		}
		if meta.ToolCalls[0].Function.Name != "test_tool" {
			t.Errorf("expected tool name 'test_tool', got %s", meta.ToolCalls[0].Function.Name)
		}
	})
}

func TestSummary(t *testing.T) {
	summary := &Summary{
		SessionID: 123,
		Phase:     "testing",
		Summary:   "Test summary content",
		State: map[string]any{
			"key": "value",
		},
	}

	if summary.SessionID != 123 {
		t.Errorf("expected SessionID 123, got %d", summary.SessionID)
	}
	if summary.Phase != "testing" {
		t.Errorf("expected Phase 'testing', got %s", summary.Phase)
	}
}
