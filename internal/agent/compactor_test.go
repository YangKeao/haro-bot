package agent

import (
	"context"
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

// mockStoreForPreserve implements memory.StoreAPI for testing summary preservation
type mockStoreForPreserve struct {
	summaries []*memory.Summary
}

func (m *mockStoreForPreserve) GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (int64, error) {
	return 1, nil
}

func (m *mockStoreForPreserve) GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error) {
	return 1, nil
}

func (m *mockStoreForPreserve) AddMessage(ctx context.Context, sessionID int64, role, content string, metadata *memory.MessageMetadata) error {
	return nil
}

func (m *mockStoreForPreserve) AppendSummary(ctx context.Context, sessionID int64, summary memory.Summary) (int64, error) {
	m.summaries = append(m.summaries, &summary)
	return int64(len(m.summaries)), nil
}

func (m *mockStoreForPreserve) LoadLatestSummary(ctx context.Context, sessionID int64) (*memory.Summary, error) {
	if len(m.summaries) == 0 {
		return nil, nil
	}
	return m.summaries[len(m.summaries)-1], nil
}

func (m *mockStoreForPreserve) LoadViewMessages(ctx context.Context, sessionID int64, limit int) ([]memory.Message, *memory.Summary, error) {
	return nil, nil, nil
}

func (m *mockStoreForPreserve) SearchMessages(ctx context.Context, sessionID int64, query string, limit int, includeTool bool) ([]memory.Message, error) {
	return nil, nil
}

// TestPreservePreviousSummary tests that empty toSummarize preserves previous summary
func TestPreservePreviousSummary(t *testing.T) {
	store := &mockStoreForPreserve{
		summaries: []*memory.Summary{
			{
				ID:        1,
				SessionID: 1,
				Summary:   "Previous context: User was working on fixing a bug in compactor.go",
				Phase:     "auto-compact",
			},
		},
	}

	estimator, _ := llm.NewTokenEstimator("gpt-4o")
	c := &Compactor{
		store:     store,
		llm:       nil, // LLM not needed when toSummarize is empty
		estimator: estimator,
	}

	// Provide messages with a very small budget to force toSummarize to be empty
	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "current message"},
	}

	// Call Compact with budget so small that toSummarize will be empty
	summary, err := c.Compact(context.Background(), 1, messages, 10)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Verify that we got the previous summary, not a new empty one
	if summary == nil {
		t.Fatal("Expected summary, got nil")
	}

	if summary.Summary == "Context cleared due to token limit. Starting fresh conversation." {
		t.Error("Should have preserved previous summary, but got empty summary")
	}

	if !strings.Contains(summary.Summary, "Previous context") {
		t.Errorf("Expected previous summary content, got: %s", summary.Summary)
	}

	t.Logf("Successfully preserved previous summary: %s", summary.Summary)
}

// TestNoPreviousSummaryCreatesEmpty tests that empty summary is created when no previous exists
func TestNoPreviousSummaryCreatesEmpty(t *testing.T) {
	store := &mockStoreForPreserve{
		summaries: nil, // No previous summaries
	}

	estimator, _ := llm.NewTokenEstimator("gpt-4o")
	c := &Compactor{
		store:     store,
		llm:       nil,
		estimator: estimator,
	}

	messages := []llm.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "current message"},
	}

	summary, err := c.Compact(context.Background(), 1, messages, 10)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	if summary == nil {
		t.Fatal("Expected summary, got nil")
	}

	if summary.Summary != "Context cleared due to token limit. Starting fresh conversation." {
		t.Errorf("Expected empty summary message, got: %s", summary.Summary)
	}

	t.Logf("Created empty summary as expected: %s", summary.Summary)
}
