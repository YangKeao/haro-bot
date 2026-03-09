package memory

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestFilterInvalidToolOutputs(t *testing.T) {
	tests := []struct {
		name            string
		msgs            []Message
		wantCount       int
		wantDeleteCount int
	}{
		{
			name:            "empty messages",
			msgs:            []Message{},
			wantCount:       0,
			wantDeleteCount: 0,
		},
		{
			name: "no tool calls - all valid",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "hello"},
				{ID: 2, Role: "assistant", Content: "hi"},
			},
			wantCount:       2,
			wantDeleteCount: 0,
		},
		{
			name: "complete tool call pair - valid",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "run tool"},
				{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
					ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test"}}},
				}},
				{ID: 3, Role: "tool", Content: "result", Metadata: &MessageMetadata{ToolCallID: "call-1"}},
			},
			wantCount:       3,
			wantDeleteCount: 0,
		},
		{
			name: "orphaned tool call without response - should delete assistant",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "run tool"},
				{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
					ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test"}}},
				}},
			},
			wantCount:       1, // only user message remains
			wantDeleteCount: 1, // assistant message deleted
		},
		{
			name: "orphaned tool response without call - should delete tool",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "hello"},
				{ID: 2, Role: "tool", Content: "orphan", Metadata: &MessageMetadata{ToolCallID: "call-missing"}},
			},
			wantCount:       1, // only user message remains
			wantDeleteCount: 1, // tool message deleted
		},
		{
			name: "orphaned tool response with empty callID - should delete",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "hello"},
				{ID: 2, Role: "tool", Content: "orphan", Metadata: &MessageMetadata{ToolCallID: ""}},
			},
			wantCount:       1,
			wantDeleteCount: 1,
		},
		{
			name: "orphaned tool response without metadata - should delete",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "hello"},
				{ID: 2, Role: "tool", Content: "orphan", Metadata: nil},
			},
			wantCount:       1,
			wantDeleteCount: 1,
		},
		{
			name: "mixed valid and orphaned calls in same assistant - delete all related",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "run tools"},
				{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
					ToolCalls: []llm.ToolCall{
						{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test1"}},
						{ID: "call-2", Type: "function", Function: llm.ToolCallFn{Name: "test2"}},
					},
				}},
				{ID: 3, Role: "tool", Content: "result1", Metadata: &MessageMetadata{ToolCallID: "call-1"}},
				// call-2 has no response - orphaned
			},
			wantCount:       1, // only user remains (assistant deleted, and its tool responses too)
			wantDeleteCount: 2, // assistant (with orphaned call) + its valid tool response
		},
		{
			name: "multiple tool calls all orphaned - delete all",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "run tools"},
				{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
					ToolCalls: []llm.ToolCall{
						{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test1"}},
						{ID: "call-2", Type: "function", Function: llm.ToolCallFn{Name: "test2"}},
					},
				}},
			},
			wantCount:       1, // only user
			wantDeleteCount: 1, // assistant
		},
		{
			name: "two complete tool call pairs - all valid",
			msgs: []Message{
				{ID: 1, Role: "user", Content: "run tools"},
				{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
					ToolCalls: []llm.ToolCall{
						{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test1"}},
						{ID: "call-2", Type: "function", Function: llm.ToolCallFn{Name: "test2"}},
					},
				}},
				{ID: 3, Role: "tool", Content: "result1", Metadata: &MessageMetadata{ToolCallID: "call-1"}},
				{ID: 4, Role: "tool", Content: "result2", Metadata: &MessageMetadata{ToolCallID: "call-2"}},
			},
			wantCount:       4,
			wantDeleteCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, deleteIDs := filterInvalidToolOutputs(tt.msgs)
			if len(got) != tt.wantCount {
				t.Errorf("filterInvalidToolOutputs() returned %d messages, want %d", len(got), tt.wantCount)
			}
			if len(deleteIDs) != tt.wantDeleteCount {
				t.Errorf("filterInvalidToolOutputs() returned %d delete IDs, want %d", len(deleteIDs), tt.wantDeleteCount)
			}
		})
	}
}

func TestFilterInvalidToolOutputs_DeletesCorrectIDs(t *testing.T) {
	msgs := []Message{
		{ID: 100, Role: "user", Content: "hello"},
		{ID: 200, Role: "assistant", Content: "", Metadata: &MessageMetadata{
			ToolCalls: []llm.ToolCall{{ID: "call-orphan", Type: "function"}},
		}},
		{ID: 300, Role: "tool", Content: "orphan-response", Metadata: &MessageMetadata{ToolCallID: "call-missing"}},
	}

	_, deleteIDs := filterInvalidToolOutputs(msgs)

	// Should delete both the orphaned call (200) and orphaned response (300)
	expectedDeletes := map[int64]bool{200: true, 300: true}
	if len(deleteIDs) != 2 {
		t.Errorf("expected 2 deletes, got %d: %v", len(deleteIDs), deleteIDs)
	}
	for _, id := range deleteIDs {
		if !expectedDeletes[id] {
			t.Errorf("unexpected delete ID %d", id)
		}
	}
}

func TestFilterInvalidToolOutputs_CascadingDelete(t *testing.T) {
	// When an assistant message is deleted due to orphaned calls,
	// ALL its tool responses should also be deleted
	msgs := []Message{
		{ID: 1, Role: "user", Content: "run tools"},
		{ID: 2, Role: "assistant", Content: "", Metadata: &MessageMetadata{
			ToolCalls: []llm.ToolCall{
				{ID: "call-1", Type: "function", Function: llm.ToolCallFn{Name: "test1"}}, // has response
				{ID: "call-2", Type: "function", Function: llm.ToolCallFn{Name: "test2"}}, // orphaned
			},
		}},
		{ID: 3, Role: "tool", Content: "result1", Metadata: &MessageMetadata{ToolCallID: "call-1"}},
	}

	_, deleteIDs := filterInvalidToolOutputs(msgs)

	// Should delete assistant (2) and its tool response (3) even though call-1 had a response
	expectedDeletes := map[int64]bool{2: true, 3: true}
	if len(deleteIDs) != 2 {
		t.Errorf("expected 2 deletes, got %d: %v", len(deleteIDs), deleteIDs)
	}
	for _, id := range deleteIDs {
		if !expectedDeletes[id] {
			t.Errorf("unexpected delete ID %d", id)
		}
	}
}
