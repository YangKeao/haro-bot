package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/tools"
)

type SearchTool struct {
	manager        *Manager
	agentID        int64
	sandboxEnabled bool
	registry       *tools.Registry
}

func (m *Manager) RegisterTools(ctx context.Context, agentID int64, sandboxEnabled bool, registry *tools.Registry) {
	registry.Register(&SearchTool{manager: m, agentID: agentID, sandboxEnabled: sandboxEnabled, registry: registry})
}

func (t *SearchTool) Name() string { return "tool_search" }
func (t *SearchTool) Description() string {
	return t.manager.SourceDescription(context.Background(), t.agentID, t.sandboxEnabled)
}
func (t *SearchTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query":       map[string]any{"type": "string", "description": "Tool name or capability to search for."},
		"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": 16, "default": 8},
	}, "required": []string{"query"}, "additionalProperties": false}
}

func (t *SearchTool) ResetSession(sessionID int64) { t.manager.ResetSession(sessionID) }

func (t *SearchTool) Execute(ctx context.Context, tc tools.ToolContext, raw json.RawMessage) (string, error) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Query) == "" {
		return "", errors.New("query is required")
	}
	catalog, warnings := t.manager.Catalog(ctx, t.agentID, tc.SessionID, t.sandboxEnabled)
	matches := rankCatalog(catalog, input.Query, input.MaxResults)
	for _, match := range matches {
		t.registry.Register(&ProxyTool{manager: t.manager, agentID: t.agentID, catalog: match})
		t.manager.Activate(tc.SessionID, match.CallableName)
	}
	response := struct {
		Tools    []CatalogTool `json:"tools"`
		Warnings []string      `json:"warnings,omitempty"`
	}{Tools: matches, Warnings: warnings}
	encoded, _ := json.Marshal(response)
	return string(encoded), nil
}

type ProxyTool struct {
	manager *Manager
	agentID int64
	catalog CatalogTool
}

func (t *ProxyTool) Name() string { return t.catalog.CallableName }
func (t *ProxyTool) Description() string {
	description := strings.TrimSpace(t.catalog.Description)
	if description == "" {
		description = t.catalog.RawName
	}
	return description + " (MCP server: " + t.catalog.ServerName + ")"
}
func (t *ProxyTool) Parameters() map[string]any {
	if t.catalog.InputSchema == nil {
		return map[string]any{"type": "object"}
	}
	return t.catalog.InputSchema
}
func (t *ProxyTool) VisibleForSession(sessionID int64) bool {
	return t.manager.IsActive(sessionID, t.Name())
}

func (t *ProxyTool) Execute(ctx context.Context, tc tools.ToolContext, raw json.RawMessage) (string, error) {
	result, err := t.ExecuteRich(ctx, tc, raw)
	return result.ModelText, err
}

func (t *ProxyTool) ExecuteRich(ctx context.Context, tc tools.ToolContext, raw json.RawMessage) (tools.ToolResult, error) {
	if !t.manager.IsActive(tc.SessionID, t.Name()) {
		return tools.ToolResult{}, errors.New("MCP tool is not active in this turn; call tool_search first")
	}
	result, err := t.manager.Call(ctx, t.agentID, tc.SessionID, t.catalog, sanitizeArguments(raw, t.catalog.Builtin))
	output := tools.ToolResult{ToolName: t.catalog.RawName, MCPServer: t.catalog.ServerName, StructuredContent: result.StructuredContent}
	var modelParts, displayParts []string
	for index, content := range result.Content {
		switch content.Type {
		case "text":
			modelParts = append(modelParts, content.Text)
			displayParts = append(displayParts, content.Text)
		case "image":
			if len(content.Data) > 4<<20 {
				modelParts = append(modelParts, "[Image omitted: exceeds 4 MiB artifact limit]")
				continue
			}
			if t.manager.artifacts == nil {
				modelParts = append(modelParts, "[Image available in tool output but artifact storage is unavailable]")
				continue
			}
			name := fmt.Sprintf("%s-%d%s", t.catalog.RawName, index+1, extensionForMIME(content.MIMEType))
			id, saveErr := t.manager.artifacts.SaveMCPArtifact(ctx, tc.SessionID, name, content.MIMEType, content.Data)
			if saveErr != nil {
				modelParts = append(modelParts, "[Image artifact could not be stored]")
				continue
			}
			output.ArtifactIDs = append(output.ArtifactIDs, id)
			modelParts = append(modelParts, "[Image saved as artifact "+id+"; inspect it in the chat UI if needed]")
			displayParts = append(displayParts, "[Image artifact: "+id+"]")
		default:
			if content.Text != "" {
				modelParts = append(modelParts, content.Text)
				displayParts = append(displayParts, content.Text)
			}
		}
	}
	if result.StructuredContent != nil {
		encoded, _ := json.Marshal(result.StructuredContent)
		if len(modelParts) == 0 {
			modelParts = append(modelParts, string(encoded))
		}
		if len(displayParts) == 0 {
			displayParts = append(displayParts, string(encoded))
		}
	}
	output.ModelText = strings.Join(modelParts, "\n")
	output.DisplayText = strings.Join(displayParts, "\n")
	if output.DisplayText == output.ModelText {
		output.DisplayText = ""
	}
	if t.catalog.Builtin && isRetirableBrowserObservation(t.catalog.RawName) {
		output.ObservationKey = "agent-browser:active-page"
	}
	if result.IsError && err == nil {
		err = errors.New("MCP server reported a tool error")
	}
	return output, err
}

func sanitizeArguments(raw json.RawMessage, builtin bool) json.RawMessage {
	if !builtin {
		return raw
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return raw
	}
	delete(args, "session")
	delete(args, "extraArgs")
	delete(args, "extra_args")
	delete(args, "allowedDomains")
	delete(args, "allowed_domains")
	delete(args, "namespace")
	delete(args, "restore")
	delete(args, "restoreCheckFn")
	delete(args, "restoreCheckText")
	delete(args, "restoreCheckUrl")
	delete(args, "restoreSave")
	delete(args, "idleTimeout")
	delete(args, "all")
	encoded, _ := json.Marshal(args)
	return encoded
}

func isRetirableBrowserObservation(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "snapshot") || strings.Contains(name, "page_content") || strings.Contains(name, "get_text")
}

func extensionForMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}
