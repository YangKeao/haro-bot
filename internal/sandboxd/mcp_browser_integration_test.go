//go:build integration

package sandboxd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAgentBrowserMCPBridge(t *testing.T) {
	if os.Getenv("HARO_AGENT_BROWSER_TEST") == "" {
		t.Skip("set HARO_AGENT_BROWSER_TEST=1 for the live agent-browser smoke test")
	}
	server, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request := sandbox.MCPServerRequest{
		Key: "a1-s1-m0", AgentID: 1, SessionID: 1, Command: "npx",
		Args:        []string{"-y", "agent-browser@0.34.0", "mcp", "--tools", "core"},
		Environment: map[string]string{"AGENT_BROWSER_SESSION": "haro-integration-browser", "AGENT_BROWSER_CONTENT_BOUNDARIES": "1", "AGENT_BROWSER_MAX_OUTPUT_CHARS": "12000", "AGENT_BROWSER_EXECUTABLE_PATH": os.Getenv("HARO_AGENT_BROWSER_EXECUTABLE_PATH")},
	}
	client, err := server.mcpClient(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	defer client.session.Close()
	listed, err := client.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"agent_browser_open": false, "agent_browser_snapshot": false, "agent_browser_screenshot": false}
	for _, tool := range listed.Tools {
		if _, ok := wanted[tool.Name]; ok {
			wanted[tool.Name] = true
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("core profile did not expose %s", name)
		}
	}
	if result, err := client.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "agent_browser_open", Arguments: map[string]any{"url": "data:text/html,<main><h1>Haro MCP smoke</h1><button>Continue</button></main>"}}); err != nil || result.IsError {
		t.Fatalf("open failed: result=%#v err=%v", result, err)
	}
	snapshot, err := client.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "agent_browser_snapshot", Arguments: map[string]any{"interactive": false}})
	if err != nil || snapshot.IsError || len(snapshot.Content) == 0 {
		t.Fatalf("snapshot failed: result=%#v err=%v", snapshot, err)
	}
	screenshot, err := client.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "agent_browser_screenshot", Arguments: map[string]any{}})
	if err != nil || screenshot.IsError || len(screenshot.Content) == 0 {
		t.Fatalf("screenshot failed: result=%#v err=%v", screenshot, err)
	}
}
