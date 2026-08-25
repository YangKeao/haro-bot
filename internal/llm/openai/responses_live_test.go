package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestResponsesHostedWebSearchLive(t *testing.T) {
	baseURL, model := liveResponsesConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client := New(baseURL, "test", WithAPIMode("responses"))
	result, err := client.Chat(ctx, llm.ChatRequest{
		Model:           model,
		Messages:        []llm.Message{{Role: "user", Content: "Search the web for the current HTML title of openai.com. Answer in one sentence with a Markdown citation."}},
		HostedWebSearch: true,
	})
	if err != nil {
		t.Fatalf("hosted web search failed: %v", err)
	}
	if len(result.Choices) != 1 || !strings.Contains(result.Choices[0].Message.Content, "http") {
		t.Fatalf("expected a cited answer, got %+v", result)
	}
}

func TestResponsesFunctionReplayLive(t *testing.T) {
	baseURL, model := liveResponsesConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := New(baseURL, "test", WithAPIMode("responses"))
	tool := llm.Tool{Type: "function", Function: llm.FunctionSpec{
		Name: "echo_value", Description: "Echo a value supplied by the user",
		Parameters: map[string]any{
			"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"},
		},
	}}
	messages := []llm.Message{{Role: "user", Content: "Call echo_value with value integration-ok, then answer with the returned value."}}
	first, err := client.Chat(ctx, llm.ChatRequest{Model: model, Messages: messages, Tools: []llm.Tool{tool}})
	if err != nil {
		t.Fatalf("function call failed: %v", err)
	}
	if len(first.Choices) != 1 || len(first.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one function call, got %+v", first)
	}
	call := first.Choices[0].Message.ToolCalls[0]
	messages = append(messages,
		first.Choices[0].Message,
		llm.Message{Role: "tool", ToolCallID: call.ID, Content: `{"value":"integration-ok"}`},
	)
	second, err := client.Chat(ctx, llm.ChatRequest{Model: model, Messages: messages, Tools: []llm.Tool{tool}})
	if err != nil {
		t.Fatalf("function replay failed: %v", err)
	}
	if len(second.Choices) != 1 || !strings.Contains(second.Choices[0].Message.Content, "integration-ok") {
		t.Fatalf("expected replayed function result, got %+v", second)
	}
}

func liveResponsesConfig(t *testing.T) (string, string) {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_OAUTH_BASE_URL"))
	if baseURL == "" {
		t.Skip("OPENAI_OAUTH_BASE_URL is not set")
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_OAUTH_MODEL"))
	if model == "" {
		model = "gpt-5.6-luna"
	}
	return baseURL, model
}
