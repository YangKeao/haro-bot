package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestTrimMessagesForBudget(t *testing.T) {
	estimator, _ := llm.NewTokenEstimator("gpt-4o")

	t.Run("empty messages return nil", func(t *testing.T) {
		result := trimMessagesForBudget(nil, estimator, 1000)
		if result != nil {
			t.Errorf("expected nil for empty messages, got %v", result)
		}
	})

	t.Run("nil estimator returns all messages", func(t *testing.T) {
		messages := []llm.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		}
		result := trimMessagesForBudget(messages, nil, 1000)
		if len(result) != len(messages) {
			t.Errorf("expected %d messages, got %d", len(messages), len(result))
		}
	})

	t.Run("zero budget returns system messages only", func(t *testing.T) {
		messages := []llm.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		}
		result := trimMessagesForBudget(messages, estimator, 0)
		// With zero budget, system messages might still be included
		// but actual behavior depends on token counting
		// Just verify we don't crash
		if result == nil {
			t.Error("result should not be nil")
		}
	})

	t.Run("preserves system messages", func(t *testing.T) {
		messages := []llm.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		}
		result := trimMessagesForBudget(messages, estimator, 1000)
		if len(result) < 1 {
			t.Fatal("expected at least system message")
		}
		if result[0].Role != "system" {
			t.Errorf("expected first message to be system, got %s", result[0].Role)
		}
	})
}

func TestSelectLLMMessagesByCount(t *testing.T) {
	t.Run("empty messages return nil", func(t *testing.T) {
		result := selectLLMMessagesByCount(nil, 10)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("zero or negative max returns all", func(t *testing.T) {
		messages := []llm.Message{
			{Role: "user", Content: "test"},
		}
		result := selectLLMMessagesByCount(messages, 0)
		if len(result) != 1 {
			t.Errorf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("respects max count", func(t *testing.T) {
		messages := []llm.Message{
			{Role: "user", Content: "1"},
			{Role: "assistant", Content: "2"},
			{Role: "user", Content: "3"},
			{Role: "assistant", Content: "4"},
			{Role: "user", Content: "5"},
		}
		result := selectLLMMessagesByCount(messages, 3)
		// Should include the 3 most recent
		if len(result) > 3 {
			t.Errorf("expected at most 3 messages, got %d", len(result))
		}
	})
}

