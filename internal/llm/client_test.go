package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewOpenAIChatModel(t *testing.T) {
	t.Run("creates client with base URL and API key", func(t *testing.T) {
		client := NewOpenAIChatModel("https://api.example.com/v1", "test-key")
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("applies options", func(t *testing.T) {
		client := NewOpenAIChatModel("https://api.example.com/v1", "test-key", WithHTTPDebug(true))
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestTokenEstimator(t *testing.T) {
	t.Run("creates estimator for known model", func(t *testing.T) {
		est, err := NewTokenEstimator("gpt-4o")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if est == nil {
			t.Fatal("expected non-nil estimator")
		}
	})

	t.Run("counts message tokens", func(t *testing.T) {
		est, _ := NewTokenEstimator("gpt-4o")
		msg := Message{Role: "user", Content: "Hello, world!"}
		count := est.CountMessage(msg)
		if count <= 0 {
			t.Errorf("expected positive token count, got %d", count)
		}
	})

	t.Run("counts messages tokens", func(t *testing.T) {
		est, _ := NewTokenEstimator("gpt-4o")
		msgs := []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello!"},
		}
		count := est.CountMessages(msgs)
		if count <= 0 {
			t.Errorf("expected positive token count, got %d", count)
		}
	})
}

func TestContextConfig(t *testing.T) {
	t.Run("zero config returns zero effective", func(t *testing.T) {
		cfg := ContextConfig{}
		if cfg.EffectiveWindowTokens() != 0 {
			t.Errorf("expected 0, got %d", cfg.EffectiveWindowTokens())
		}
	})

	t.Run("calculates effective window", func(t *testing.T) {
		cfg := ContextConfig{
			WindowTokens:                  100000,
			EffectiveContextWindowPercent: 80,
		}
		effective := cfg.EffectiveWindowTokens()
		if effective != 80000 {
			t.Errorf("expected 80000, got %d", effective)
		}
	})

	t.Run("defaults to 95 percent", func(t *testing.T) {
		cfg := ContextConfig{
			WindowTokens: 100000,
		}
		effective := cfg.EffectiveWindowTokens()
		if effective != 95000 {
			t.Errorf("expected 95000, got %d", effective)
		}
	})

	t.Run("auto compact limit", func(t *testing.T) {
		cfg := ContextConfig{
			AutoCompactTokenLimit: 50000,
		}
		limit := cfg.AutoCompactLimit()
		if limit != 50000 {
			t.Errorf("expected 50000, got %d", limit)
		}
	})
}

func TestReasoningConfig(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		cfg := ReasoningConfig{}
		if cfg.Enabled {
			t.Error("expected reasoning to be disabled by default")
		}
	})
}

func TestIsContextWindowExceeded(t *testing.T) {
	t.Run("returns false for nil error", func(t *testing.T) {
		if IsContextWindowExceeded(nil) {
			t.Error("expected false for nil error")
		}
	})
}

func TestChatRespectsCanceledContext(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"unexpected request"}`))
	}))
	defer srv.Close()

	client := NewOpenAIChatModel(srv.URL+"/v1", "test-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Chat(ctx, ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Purpose: PurposeChat,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("expected no outbound request with canceled context, got %d", requestCount.Load())
	}
}
