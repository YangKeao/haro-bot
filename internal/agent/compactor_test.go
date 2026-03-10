package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestCompactorShouldCompact(t *testing.T) {
	estimator, _ := llm.NewTokenEstimator("gpt-4o")

	tests := []struct {
		name        string
		messages    []llm.Message
		budget      int
		wantCompact bool
	}{
		{
			name:        "empty messages should not compact",
			messages:    nil,
			budget:      1000,
			wantCompact: false,
		},
		{
			name: "few messages should not compact",
			messages: []llm.Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			},
			budget:      1000,
			wantCompact: false,
		},
		{
			name: "many small messages under threshold should not compact",
			messages: func() []llm.Message {
				var msgs []llm.Message
				for i := 0; i < 10; i++ {
					msgs = append(msgs, llm.Message{Role: "user", Content: "short"})
					msgs = append(msgs, llm.Message{Role: "assistant", Content: "ok"})
				}
				return msgs
			}(),
			budget:      10000,
			wantCompact: false,
		},
		{
			name: "messages over threshold should compact",
			messages: func() []llm.Message {
				var msgs []llm.Message
				for i := 0; i < 8; i++ {
					msgs = append(msgs, llm.Message{Role: "user", Content: "This is a longer message to increase token count"})
					msgs = append(msgs, llm.Message{Role: "assistant", Content: "This is a longer response to increase token count"})
				}
				return msgs
			}(),
			budget:      200,
			wantCompact: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compactor{estimator: estimator}
			got := c.ShouldCompact(tt.messages, tt.budget)
			if got != tt.wantCompact {
				t.Errorf("ShouldCompact() = %v, want %v", got, tt.wantCompact)
			}
		})
	}
}

func TestCompactionTailStart(t *testing.T) {
	tests := []struct {
		name string
		msgs []memory.Message
		want int
	}{
		{
			name: "empty",
			msgs: nil,
			want: 0,
		},
		{
			name: "pending user after assistant",
			msgs: []memory.Message{
				{Role: "user", Content: "a"},
				{Role: "assistant", Content: "b"},
				{Role: "user", Content: "c"},
			},
			want: 2,
		},
		{
			name: "keep latest full turn",
			msgs: []memory.Message{
				{Role: "user", Content: "u1"},
				{Role: "assistant", Content: "a1"},
				{Role: "user", Content: "u2"},
				{Role: "assistant", Content: "a2"},
				{Role: "tool", Content: "t2"},
			},
			want: 2,
		},
		{
			name: "no user fallback to assistant",
			msgs: []memory.Message{
				{Role: "assistant", Content: "a1"},
				{Role: "tool", Content: "t1"},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactionTailStart(tt.msgs)
			if got != tt.want {
				t.Fatalf("tail start = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAppendTransientTail(t *testing.T) {
	stored := []llm.Message{{Role: "user", Content: "stored"}}
	current := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "stored"},
		{Role: "assistant", Content: "runtime"},
	}
	out := appendTransientTail(stored, current, 1)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[1].Content != "runtime" {
		t.Fatalf("unexpected transient message: %q", out[1].Content)
	}
}

func TestSelectCompactionPrefixAndTailBoundary(t *testing.T) {
	view := []memory.Message{
		{ID: 11, Role: "user", Content: "u1"},
		{ID: 12, Role: "assistant", Content: "a1"},
		{ID: 13, Role: "user", Content: "u2"},
		{ID: 14, Role: "assistant", Content: "a2"},
		{ID: 15, Role: "tool", Content: "t2"},
	}

	prefix, tail := selectCompactionPrefixAndTail(view)
	if len(prefix) != 2 || len(tail) != 3 {
		t.Fatalf("unexpected split: prefix=%d tail=%d", len(prefix), len(tail))
	}
	if prefix[len(prefix)-1].ID != 12 {
		t.Fatalf("unexpected prefix boundary id: %d", prefix[len(prefix)-1].ID)
	}
	if tail[0].ID != 13 {
		t.Fatalf("unexpected tail start id: %d", tail[0].ID)
	}
}

func TestSplitSystemMessages(t *testing.T) {
	input := []llm.Message{
		{Role: "system", Content: "main system"},
		{Role: "system", Content: "Session summary:\nold summary"},
		{Role: "system", Content: "another system"},
		{Role: "system", Content: "Session summary:\nnew summary"},
		{Role: "user", Content: "hello"},
	}

	base, latestSummary := splitSystemMessages(input)
	if len(base) != 2 {
		t.Fatalf("expected 2 base system messages, got %d", len(base))
	}
	if latestSummary == "" || !strings.Contains(latestSummary, "new summary") {
		t.Fatalf("expected latest summary to be kept, got %q", latestSummary)
	}
}

func TestBuildCompactPromptWithToolCalls(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "Search for Go"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{ID: "1", Function: llm.ToolCallFn{Name: "brave_search"}},
		}},
		{Role: "tool", ToolCallID: "1", Content: "results"},
	}

	prompt := buildCompactPrompt(messages)

	if !strings.Contains(prompt, "[brave_search]") {
		t.Error("prompt should contain tool call name")
	}
	if !strings.Contains(prompt, "Tool: results") {
		t.Error("prompt should contain tool response")
	}
	if !strings.Contains(prompt, "Summary:") {
		t.Error("prompt should end with Summary:")
	}
}

func TestBuildCompactPromptUsesRealNewlines(t *testing.T) {
	prompt := buildCompactPrompt([]llm.Message{{Role: "user", Content: "hello"}})
	if !strings.Contains(prompt, "Conversation:\n---\n") {
		t.Fatalf("prompt should contain real newline separators: %q", prompt)
	}
	if strings.Contains(prompt, "Conversation:\\n---\\n") {
		t.Fatalf("prompt should not contain escaped newline literals: %q", prompt)
	}
}
