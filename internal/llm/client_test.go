package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatUsesStreaming(t *testing.T) {
	var gotStream any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotStream = payload["stream"]
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"sequence_number\":0,\"output_index\":0,\"content_index\":0,\"item_id\":\"item_1\",\"delta\":\"ok\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.done\",\"sequence_number\":1,\"output_index\":0,\"content_index\":0,\"item_id\":\"item_1\",\"text\":\"ok\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "")
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if gotStream != true {
		t.Fatalf("expected stream=true, got %v", gotStream)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
