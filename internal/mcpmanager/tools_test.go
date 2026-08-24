package mcpmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallableNameIsStableSafeAndBounded(t *testing.T) {
	clean := callableName("github", "search_code")
	if clean != "mcp__github__search_code" {
		t.Fatalf("unexpected clean name: %s", clean)
	}
	left := callableName("a.b", "search/code")
	right := callableName("a-b", "search-code")
	if left == right {
		t.Fatalf("sanitized names collided: %s", left)
	}
	long := callableName(strings.Repeat("server", 20), strings.Repeat("tool", 30))
	if len(long) > 64 {
		t.Fatalf("name has %d bytes: %s", len(long), long)
	}
}

func TestRankCatalogPrefersExactNames(t *testing.T) {
	catalog := []CatalogTool{
		{RawName: "search_issues", CallableName: "mcp__github__search_issues", Description: "Search issue text"},
		{RawName: "get_issue", CallableName: "mcp__github__get_issue", Description: "Get one issue"},
	}
	got := rankCatalog(catalog, "get_issue", 8)
	if len(got) != 1 || got[0].RawName != "get_issue" {
		t.Fatalf("unexpected matches: %#v", got)
	}
}

func TestBrowserArgumentsAndSchemaDropHostControlledFields(t *testing.T) {
	raw := json.RawMessage(`{"url":"https://example.com","session":"other","extraArgs":["--foo"],"allowedDomains":["evil.test"],"all":true}`)
	var args map[string]any
	if err := json.Unmarshal(sanitizeArguments(raw, true), &args); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session", "extraArgs", "allowedDomains", "all"} {
		if _, ok := args[key]; ok {
			t.Fatalf("host-controlled argument %s survived", key)
		}
	}
	if args["url"] != "https://example.com" {
		t.Fatalf("url was changed: %#v", args)
	}
	schema := sanitizeBrowserSchema(map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}, "session": map[string]any{"type": "string"}, "extraArgs": map[string]any{"type": "array"}}})
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["session"]; ok {
		t.Fatal("session remained in projected schema")
	}
	if _, ok := properties["url"]; !ok {
		t.Fatal("ordinary field was removed")
	}
}

func TestDiscoverProtectedResourceMetadataFromChallenge(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+server.URL+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource/mcp":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": server.URL + "/mcp", "authorization_servers": []string{server.URL}, "scopes_supported": []string{"read"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	metadata, err := discoverProtectedResource(context.Background(), server.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Resource != server.URL+"/mcp" || len(metadata.AuthorizationServers) != 1 || len(metadata.ScopesSupported) != 1 || metadata.ScopesSupported[0] != "read" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}
