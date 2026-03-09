package agent

import (
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
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
			name:         "empty messages should not compact",
			messages:     nil,
			budget:       1000,
			wantCompact:  false,
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
				// Need at least compactMinMessages (6) messages
				for i := 0; i < 8; i++ {
					msgs = append(msgs, llm.Message{Role: "user", Content: "This is a longer message to increase token count"})
					msgs = append(msgs, llm.Message{Role: "assistant", Content: "This is a longer response to increase token count"})
				}
				return msgs
			}(),
			budget:      200,
			wantCompact: true,
		},
		{
			name: "zero budget should not compact",
			messages: func() []llm.Message {
				return []llm.Message{
					{Role: "user", Content: "test"},
					{Role: "assistant", Content: "response"},
					{Role: "user", Content: "test"},
					{Role: "assistant", Content: "response"},
					{Role: "user", Content: "test"},
					{Role: "assistant", Content: "response"},
				}
			}(),
			budget:      0,
			wantCompact: false,
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

func TestCompactorNilEstimator(t *testing.T) {
	c := &Compactor{estimator: nil}
	messages := []llm.Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
	}
	if c.ShouldCompact(messages, 100) {
		t.Error("ShouldCompact should return false with nil estimator")
	}
}

func TestBuildCompactPrompt(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
		{Role: "assistant", Content: "I'm doing well, thanks!"},
	}

	prompt := buildCompactPrompt(messages)

	if !strings.Contains(prompt, "User: Hello") {
		t.Error("prompt should contain user message")
	}
	if !strings.Contains(prompt, "Assistant: Hi there!") {
		t.Error("prompt should contain assistant message")
	}
	if !strings.Contains(prompt, "Summary:") {
		t.Error("prompt should end with Summary:")
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
}
