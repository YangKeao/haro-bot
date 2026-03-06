package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBraveSearchToolMissingKey(t *testing.T) {
	tool := NewBraveSearchTool("")
	_, err := tool.Execute(context.Background(), ToolContext{}, mustJSON(t, map[string]any{
		"query": "golang",
	}))
	if err == nil {
		t.Fatalf("expected error for missing api key")
	}
}

func TestBraveSearchToolSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "key" {
			t.Errorf("unexpected api key header: %q", got)
			http.Error(w, "bad api key", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		if got := q.Get("q"); got != "golang" {
			t.Errorf("unexpected query: %q", got)
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		if got := q.Get("count"); got != "5" {
			t.Errorf("unexpected count: %q", got)
			http.Error(w, "bad count", http.StatusBadRequest)
			return
		}
		if got := q.Get("offset"); got != "1" {
			t.Errorf("unexpected offset: %q", got)
			http.Error(w, "bad offset", http.StatusBadRequest)
			return
		}
		if got := q.Get("country"); got != "US" {
			t.Errorf("unexpected country: %q", got)
			http.Error(w, "bad country", http.StatusBadRequest)
			return
		}
		if got := q.Get("search_lang"); got != "en" {
			t.Errorf("unexpected search_lang: %q", got)
			http.Error(w, "bad search_lang", http.StatusBadRequest)
			return
		}
		if got := q.Get("ui_lang"); got != "en-US" {
			t.Errorf("unexpected ui_lang: %q", got)
			http.Error(w, "bad ui_lang", http.StatusBadRequest)
			return
		}
		if got := q.Get("freshness"); got != "pw" {
			t.Errorf("unexpected freshness: %q", got)
			http.Error(w, "bad freshness", http.StatusBadRequest)
			return
		}
		if got := q.Get("summary"); got != "1" {
			t.Errorf("unexpected summary: %q", got)
			http.Error(w, "bad summary", http.StatusBadRequest)
			return
		}
		if got := q.Get("enable_rich_callback"); got != "1" {
			t.Errorf("unexpected enable_rich_callback: %q", got)
			http.Error(w, "bad enable_rich_callback", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"query":{"original":"golang","more_results_available":true},"web":{"results":[{"title":"Go","url":"https://go.dev","description":"The Go programming language"}]}}`)
	}))
	defer srv.Close()

	tool := NewBraveSearchTool("key",
		WithBraveSearchEndpoint(srv.URL),
		WithBraveSearchHTTPClient(srv.Client()),
	)

	out, err := tool.Execute(context.Background(), ToolContext{}, mustJSON(t, map[string]any{
		"query":                "golang",
		"count":                5,
		"offset":               1,
		"country":              "US",
		"search_lang":          "en",
		"ui_lang":              "en-US",
		"freshness":            "pw",
		"summary":              true,
		"enable_rich_callback": true,
	}))
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}

	var payload struct {
		Query struct {
			Original             string `json:"original"`
			MoreResultsAvailable bool   `json:"more_results_available"`
		} `json:"query"`
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if payload.Query.Original != "golang" {
		t.Fatalf("unexpected query in output: %q", payload.Query.Original)
	}
	if !payload.Query.MoreResultsAvailable {
		t.Fatalf("expected more_results_available to be true")
	}
	if len(payload.Results) != 1 || payload.Results[0].URL != "https://go.dev" {
		t.Fatalf("unexpected results: %+v", payload.Results)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return b
}
