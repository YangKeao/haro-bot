//go:build integration

package llm_test

import (
	"context"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

func TestLLMChat(t *testing.T) {
	client, model := testutil.NewLLMClientFromEnv(t)
	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Model:  model,
		Stream: true,
		Messages: []llm.Message{
			{Role: "user", Content: "Say hello in one word."},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		t.Fatalf("empty response: %+v", resp)
	}
}
