package openai

import (
	"context"
	"errors"
	"fmt"
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
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("expected no outbound request with canceled context, got %d", requestCount.Load())
	}
}

func TestChatRetriesEmptyResponses(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := requestCount.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			defer f.Flush()
		}
		if attempt == 1 {
			fmt.Fprint(w, "data: {\"id\":\"first\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"thinking\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"first\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"second\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"second\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := New(srv.URL, "test-key")
	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-4o",
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("expected 2 requests, got %d", got)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestChatStreamsReasoningContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"Checking the calculation\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"3973\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var reasoningDeltas []string
	client := New(srv.URL, "")
	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Model: "gpt-5.4",
		Messages: []llm.Message{
			{Role: "user", Content: "calculate"},
		},
		ReasoningEnabled: true,
		StreamHandler: func(event llm.StreamEvent) {
			if event.ReasoningDelta != "" {
				reasoningDeltas = append(reasoningDeltas, event.ReasoningDelta)
			}
		},
	})
	if err != nil {
		t.Fatalf("expected chat to succeed, got %v", err)
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "Checking the calculation" {
		t.Fatalf("unexpected persisted reasoning content: %q", got)
	}
	if len(reasoningDeltas) != 1 || reasoningDeltas[0] != "Checking the calculation" {
		t.Fatalf("unexpected streamed reasoning deltas: %#v", reasoningDeltas)
	}
}
