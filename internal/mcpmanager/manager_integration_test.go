//go:build integration

package mcpmanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/testutil"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableHTTPServerCatalogAndCall(t *testing.T) {
	database, cleanup := testutil.NewTestDBWithMigrations(t)
	defer cleanup()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-mcp", Version: "1"}, nil)
	server.AddTool(&sdkmcp.Tool{Name: "echo", Description: "Echo text", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}, func(_ context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var input struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(request.Params.Arguments, &input); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echo:" + input.Text}}}, nil
	})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Token") != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	if err := database.Create(&dbmodel.Provider{ID: 1, Name: "test", BaseURL: "https://example.invalid", PromptFormat: "openai"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&dbmodel.Agent{ID: 1, ProviderID: 1, Name: "test", Model: "test", EffectiveContextWindowPercent: 95}).Error; err != nil {
		t.Fatal(err)
	}
	box, err := sandbox.NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(database, box, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	configured, err := manager.CreateServer(context.Background(), ServerWrite{Name: "echo", Transport: TransportHTTP, URL: httpServer.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&dbmodel.AgentMCPServer{AgentID: 1, ServerID: configured.ID, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetAgentCredential(context.Background(), 1, configured.ID, AgentCredentialWrite{Headers: map[string]string{"X-Test-Token": "secret"}}); err != nil {
		t.Fatal(err)
	}
	catalog, warnings := manager.Catalog(context.Background(), 1, 1, false)
	if len(warnings) != 0 || len(catalog) != 1 || catalog[0].RawName != "echo" {
		t.Fatalf("catalog=%#v warnings=%#v", catalog, warnings)
	}
	result, err := manager.Call(context.Background(), 1, 1, catalog[0], json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "echo:hello" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
