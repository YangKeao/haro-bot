package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type toolRunnerStore struct {
	added []memory.Message
}

func (s *toolRunnerStore) GetOrCreateUserByTelegramID(context.Context, int64) (int64, error) {
	return 0, nil
}

func (s *toolRunnerStore) GetOrCreateSession(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (s *toolRunnerStore) AddMessage(context.Context, int64, string, string, *memory.MessageMetadata) error {
	return nil
}

func (s *toolRunnerStore) AddMessageAndGetID(_ context.Context, _ int64, role, content string, metadata *memory.MessageMetadata) (int64, error) {
	s.added = append(s.added, memory.Message{ID: int64(len(s.added) + 1), Role: role, Content: content, Metadata: metadata})
	return int64(len(s.added)), nil
}

func (s *toolRunnerStore) AppendSummary(context.Context, int64, memory.Summary) (int64, error) {
	return 0, nil
}

func (s *toolRunnerStore) LoadLatestSummary(context.Context, int64) (*memory.Summary, error) {
	return nil, nil
}

func (s *toolRunnerStore) LoadViewMessages(context.Context, int64, int) ([]memory.Message, *memory.Summary, error) {
	return nil, nil, nil
}

func (s *toolRunnerStore) SearchMessages(context.Context, int64, string, int, bool) ([]memory.Message, error) {
	return nil, nil
}

type staticTool struct {
	name   string
	output string
}

func (t *staticTool) Name() string { return t.name }

func (t *staticTool) Description() string { return "" }

func (t *staticTool) Parameters() map[string]any { return map[string]any{} }

func (t *staticTool) Execute(context.Context, tools.ToolContext, json.RawMessage) (string, error) {
	return t.output, nil
}

func TestToolRunnerTruncatesLargeToolOutput(t *testing.T) {
	estimator, err := llm.NewTokenEstimator("gpt-4o")
	if err != nil {
		t.Fatalf("new token estimator: %v", err)
	}
	var builder strings.Builder
	for i := 0; i < 5000; i++ {
		builder.WriteString("token ")
	}
	builder.WriteString("tail-marker")
	output := "head-marker " + builder.String()

	store := &toolRunnerStore{}
	registry := tools.NewRegistry(&staticTool{name: "big_tool", output: output})
	runner := NewToolRunner(registry, store, nil, nil, estimator)

	msgs, _, err := runner.Run(context.Background(), 1, 2, "", nil, []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "big_tool",
			Arguments: `{}`,
		},
	}})
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	got := msgs[0].ToLLM().Content
	if !strings.Contains(got, "tokens truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.Contains(got, "head-marker") {
		t.Fatalf("expected prefix to remain, got %q", got)
	}
	if !strings.Contains(got, "tail-marker") {
		t.Fatalf("expected suffix to remain, got %q", got)
	}
	if tokens := estimator.CountTokens(got); tokens > maxToolOutputTokens {
		t.Fatalf("expected output <= %d tokens, got %d", maxToolOutputTokens, tokens)
	}
	if len(store.added) != 1 || store.added[0].Content != got {
		t.Fatalf("expected persisted content to match truncated output, got %+v", store.added)
	}
}

func TestToolRunnerKeepsSmallToolOutput(t *testing.T) {
	estimator, err := llm.NewTokenEstimator("gpt-4o")
	if err != nil {
		t.Fatalf("new token estimator: %v", err)
	}
	store := &toolRunnerStore{}
	registry := tools.NewRegistry(&staticTool{name: "small_tool", output: "short output"})
	runner := NewToolRunner(registry, store, nil, nil, estimator)

	msgs, _, err := runner.Run(context.Background(), 1, 2, "", nil, []llm.ToolCall{{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFn{
			Name:      "small_tool",
			Arguments: `{}`,
		},
	}})
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if got := msgs[0].ToLLM().Content; got != "short output" {
		t.Fatalf("expected output unchanged, got %q", got)
	}
}
