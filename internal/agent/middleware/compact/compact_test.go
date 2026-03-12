package compact

import (
	"context"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

type stubChatModel struct{}

func (stubChatModel) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

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
		{
			name: "zero budget should not compact",
			messages: []llm.Message{
				{Role: "user", Content: "test"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "test"},
				{Role: "assistant", Content: "response"},
				{Role: "user", Content: "test"},
				{Role: "assistant", Content: "response"},
			},
			budget:      0,
			wantCompact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &compactor{estimator: estimator}
			got := c.shouldCompact(tt.messages, tt.budget)
			if got != tt.wantCompact {
				t.Errorf("shouldCompact() = %v, want %v", got, tt.wantCompact)
			}
		})
	}
}

func TestCompactorNilEstimator(t *testing.T) {
	c := &compactor{estimator: nil}
	messages := []llm.Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "response"},
	}
	if c.shouldCompact(messages, 100) {
		t.Error("shouldCompact should return false with nil estimator")
	}
}

func TestCompactorCompactRequiresCutoffEntryID(t *testing.T) {
	c := &compactor{
		store: noopStoreAPI{},
		llm:   stubChatModel{},
		model: "test-model",
	}
	_, err := c.compact(context.Background(), 1, []llm.Message{{Role: "user", Content: "hello"}}, 4096, 0)
	if err == nil {
		t.Fatal("expected error when cutoff entry id is missing")
	}
	if !strings.Contains(err.Error(), "cutoff entry id required") {
		t.Fatalf("unexpected error: %v", err)
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

func TestCompactCutoffEntryIDUsesLastStoredMessage(t *testing.T) {
	user, err := newStoredMessageForTest(101, llm.Message{Role: "user", Content: "u1"})
	if err != nil {
		t.Fatalf("create user message: %v", err)
	}
	assistantMsg, err := newStoredMessageForTest(202, llm.Message{Role: "assistant", Content: "a1"})
	if err != nil {
		t.Fatalf("create assistant message: %v", err)
	}

	cutoff, err := compactCutoffEntryID([]agent.StoredMessage{user, assistantMsg})
	if err != nil {
		t.Fatalf("compactCutoffEntryID returned error: %v", err)
	}
	if cutoff != 202 {
		t.Fatalf("cutoff = %d, want %d", cutoff, 202)
	}
}

func TestCompactCutoffEntryIDFailsWithoutStoredMessages(t *testing.T) {
	_, err := compactCutoffEntryID(nil)
	if err == nil {
		t.Fatal("expected error when no stored message exists")
	}
}

func TestCompactCutoffEntryIDFailsOnInvalidStoredEntryID(t *testing.T) {
	_, err := compactCutoffEntryID([]agent.StoredMessage{
		{
			EntryID: 0,
			Message: llm.Message{Role: "user", Content: "bad"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid stored entry id")
	}
}

func TestSelectCompactionPrefixAndTail(t *testing.T) {
	tests := []struct {
		name            string
		messages        []llm.Message
		wantPrefixCount int
		wantTailCount   int
		wantTailFirst   string
	}{
		{
			name: "tool exchange keeps triggering user in tail",
			messages: []llm.Message{
				{Role: "user", Content: "first request"},
				{Role: "assistant", Content: "first response"},
				{Role: "user", Content: "second request"},
				{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
					{ID: "1", Function: llm.ToolCallFn{Name: "brave_search"}},
				}},
				{Role: "tool", ToolCallID: "1", Content: "results"},
			},
			wantPrefixCount: 2,
			wantTailCount:   3,
			wantTailFirst:   "second request",
		},
		{
			name: "assistant without tool calls keeps latest exchange",
			messages: []llm.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			wantPrefixCount: 0,
			wantTailCount:   2,
			wantTailFirst:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := toStoredMessagesForTest(tt.messages)
			if err != nil {
				t.Fatalf("toStoredMessagesForTest: %v", err)
			}
			prefix, tail := selectCompactionPrefixAndTail(stored)

			if len(prefix) != tt.wantPrefixCount {
				t.Errorf("prefix count = %d, want %d", len(prefix), tt.wantPrefixCount)
			}
			if len(tail) != tt.wantTailCount {
				t.Errorf("tail count = %d, want %d", len(tail), tt.wantTailCount)
			}
			if tt.wantTailFirst != "" && len(tail) > 0 {
				first := tail[0].Message
				if first.Content != tt.wantTailFirst {
					t.Errorf("tail first content = %q, want %q", first.Content, tt.wantTailFirst)
				}
			}
		})
	}
}

func newStoredMessageForTest(entryID int64, msg llm.Message) (agent.StoredMessage, error) {
	if entryID <= 0 {
		return agent.StoredMessage{}, nil
	}
	return agent.StoredMessage{
		EntryID: entryID,
		Message: msg,
	}, nil
}

func toStoredMessagesForTest(messages []llm.Message) ([]agent.StoredMessage, error) {
	out := make([]agent.StoredMessage, 0, len(messages))
	for i, msg := range messages {
		stored, err := newStoredMessageForTest(int64(i+1), msg)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

type noopStoreAPI struct{}

func (noopStoreAPI) GetOrCreateUserByExternalID(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (noopStoreAPI) GetOrCreateSession(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (noopStoreAPI) AddMessage(context.Context, int64, string, string, *memory.MessageMetadata) error {
	return nil
}

func (noopStoreAPI) AddMessageAndGetID(context.Context, int64, string, string, *memory.MessageMetadata) (int64, error) {
	return 0, nil
}

func (noopStoreAPI) AppendSummary(context.Context, int64, memory.Summary) (int64, error) {
	return 0, nil
}

func (noopStoreAPI) LoadLatestSummary(context.Context, int64) (*memory.Summary, error) {
	return nil, nil
}

func (noopStoreAPI) LoadViewMessages(context.Context, int64, int) ([]memory.Message, *memory.Summary, error) {
	return nil, nil, nil
}

func (noopStoreAPI) SearchMessages(context.Context, int64, string, int, bool) ([]memory.Message, error) {
	return nil, nil
}
