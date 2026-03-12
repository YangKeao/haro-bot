package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestNewOpenAI(t *testing.T) {
	t.Run("creates client with base URL and API key", func(t *testing.T) {
		client := New("https://api.example.com/v1", "test-key")
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("applies options", func(t *testing.T) {
		client := New("https://api.example.com/v1", "test-key", WithHTTPDebug(true))
		if client == nil {
			t.Fatal("expected non-nil client")
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

	client := New(srv.URL+"/v1", "test-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Chat(ctx, llm.ChatRequest{
		Model: "gpt-4o",
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
		Purpose: llm.PurposeChat,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("expected no outbound request with canceled context, got %d", requestCount.Load())
	}
}
