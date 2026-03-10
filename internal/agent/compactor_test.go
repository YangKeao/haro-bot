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

func TestExtractLastTurn(t *testing.T) {
	tests := []struct {
		name              string
		messages          []llm.Message
		wantLastTurnCount int
		wantRemaining     int
		wantUserContent   string // Content of the first message in lastTurn (should be user if exists)
	}{
		{
			name: "empty messages",
			messages: []llm.Message{},
			wantLastTurnCount: 0,
			wantRemaining: 0,
		},
		{
			name: "only user message",
			messages: []llm.Message{
				{Role: "user", Content: "hello"},
			},
			wantLastTurnCount: 1,
			wantRemaining: 0,
			wantUserContent: "hello",
		},
		{
			name: "user then assistant - preserve user",
			messages: []llm.Message{
				{Role: "user", Content: "search for Go"},
				{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
					{ID: "1", Function: llm.ToolCallFn{Name: "brave_search"}},
				}},
				{Role: "tool", ToolCallID: "1", Content: "results"},
			},
			wantLastTurnCount: 3,
			wantRemaining: 0,
			wantUserContent: "search for Go",
		},
		{
			name: "multiple turns - preserve last user",
			messages: []llm.Message{
				{Role: "user", Content: "first request"},
				{Role: "assistant", Content: "first response"},
				{Role: "user", Content: "second request"},
				{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
					{ID: "1", Function: llm.ToolCallFn{Name: "brave_search"}},
				}},
				{Role: "tool", ToolCallID: "1", Content: "results"},
			},
			wantLastTurnCount: 3,
			wantRemaining: 2,
			wantUserContent: "second request",
		},
		{
			name: "assistant without tool calls",
			messages: []llm.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			wantLastTurnCount: 2,
			wantRemaining: 0,
			wantUserContent: "hello",
		},
		{
			name: "filter unrelated tool responses",
			messages: []llm.Message{
				{Role: "user", Content: "search"},
				{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
					{ID: "1", Function: llm.ToolCallFn{Name: "search"}},
				}},
				{Role: "tool", ToolCallID: "2", Content: "old results"}, // Unrelated
				{Role: "tool", ToolCallID: "1", Content: "new results"}, // Related
			},
			wantLastTurnCount: 3, // user + assistant + related tool
			wantRemaining: 0,
			wantUserContent: "search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lastTurn, remaining := extractLastTurn(tt.messages)
			
			if len(lastTurn) != tt.wantLastTurnCount {
				t.Errorf("lastTurn count = %d, want %d", len(lastTurn), tt.wantLastTurnCount)
			}
			
			if len(remaining) != tt.wantRemaining {
				t.Errorf("remaining count = %d, want %d", len(remaining), tt.wantRemaining)
			}
			
			if tt.wantUserContent != "" && len(lastTurn) > 0 {
				if lastTurn[0].Role != "user" {
					t.Errorf("first message in lastTurn should be user, got %s", lastTurn[0].Role)
				}
				if lastTurn[0].Content != tt.wantUserContent {
					t.Errorf("user content = %q, want %q", lastTurn[0].Content, tt.wantUserContent)
				}
			}
		})
	}
}
