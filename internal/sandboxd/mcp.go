package sandboxd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type managedMCPClient struct {
	request sandbox.MCPServerRequest
	session *sdkmcp.ClientSession
}

func (s *Server) handleListMCPTools(w http.ResponseWriter, r *http.Request) {
	var input sandbox.MCPServerRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	client, err := s.mcpClient(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var tools []sandbox.MCPTool
	params := &sdkmcp.ListToolsParams{}
	for {
		result, err := client.session.ListTools(r.Context(), params)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		for _, tool := range result.Tools {
			schema, _ := tool.InputSchema.(map[string]any)
			if schema == nil {
				encoded, _ := json.Marshal(tool.InputSchema)
				_ = json.Unmarshal(encoded, &schema)
			}
			title := ""
			if tool.Annotations != nil {
				title = tool.Annotations.Title
			}
			tools = append(tools, sandbox.MCPTool{Name: tool.Name, Title: title, Description: tool.Description, InputSchema: schema})
		}
		if result.NextCursor == "" {
			break
		}
		params.Cursor = result.NextCursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *Server) handleCallMCPTool(w http.ResponseWriter, r *http.Request) {
	var input sandbox.MCPCallRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	client, err := s.mcpClient(r.Context(), input.Server)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var args any = map[string]any{}
	if len(input.Arguments) > 0 {
		if err := json.Unmarshal(input.Arguments, &args); err != nil {
			writeError(w, http.StatusBadRequest, "invalid MCP tool arguments")
			return
		}
	}
	result, err := client.session.CallTool(r.Context(), &sdkmcp.CallToolParams{Name: input.Name, Arguments: args})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, convertMCPResult(result))
}

func (s *Server) handleCloseMCPSession(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	s.mu.Lock()
	client := s.mcpClients[key]
	delete(s.mcpClients, key)
	s.mu.Unlock()
	if client != nil {
		_ = client.session.Close()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mcpClient(ctx context.Context, input sandbox.MCPServerRequest) (*managedMCPClient, error) {
	if err := validateMCPRequest(input); err != nil {
		return nil, err
	}
	s.mu.RLock()
	client := s.mcpClients[input.Key]
	clientCount := len(s.mcpClients)
	s.mu.RUnlock()
	if client != nil {
		if !sameMCPServer(client.request, input) {
			return nil, errors.New("MCP session key is already used by another server configuration")
		}
		return client, nil
	}
	if clientCount >= maxMCPClients {
		return nil, errors.New("sandbox MCP client limit reached")
	}
	workdir, err := s.resolveWorkdir(input.Workdir)
	if err != nil {
		return nil, err
	}
	command := exec.Command(input.Command, input.Args...)
	command.Dir = workdir
	command.Env = buildEnvironment(input.Environment, s.workspace, false)
	command.Stderr = os.Stderr
	transport := &sdkmcp.CommandTransport{Command: command, TerminateDuration: 5 * time.Second}
	session, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "haro-sandboxd", Version: "1"}, &sdkmcp.ClientOptions{Capabilities: &sdkmcp.ClientCapabilities{}}).Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("start MCP server: %w", err)
	}
	created := &managedMCPClient{request: input, session: session}
	s.mu.Lock()
	if existing := s.mcpClients[input.Key]; existing != nil {
		s.mu.Unlock()
		_ = session.Close()
		return existing, nil
	}
	s.mcpClients[input.Key] = created
	s.mu.Unlock()
	return created, nil
}

func validateMCPRequest(input sandbox.MCPServerRequest) error {
	if strings.TrimSpace(input.Key) == "" || len(input.Key) > 256 || strings.ContainsAny(input.Key, "/\\") {
		return errors.New("valid MCP session key is required")
	}
	if input.AgentID <= 0 || input.SessionID <= 0 || strings.TrimSpace(input.Command) == "" {
		return errors.New("agent_id, session_id and command are required")
	}
	if len(input.Args) > 100 || len(input.Environment) > 100 {
		return errors.New("MCP arguments or environment are too large")
	}
	return nil
}

func sameMCPServer(left, right sandbox.MCPServerRequest) bool {
	return left.AgentID == right.AgentID && left.SessionID == right.SessionID && left.Command == right.Command && fmt.Sprint(left.Args) == fmt.Sprint(right.Args)
}

func convertMCPResult(result *sdkmcp.CallToolResult) sandbox.MCPCallResult {
	output := sandbox.MCPCallResult{StructuredContent: result.StructuredContent, IsError: result.IsError}
	for _, content := range result.Content {
		switch value := content.(type) {
		case *sdkmcp.TextContent:
			output.Content = append(output.Content, sandbox.MCPContent{Type: "text", Text: value.Text})
		case *sdkmcp.ImageContent:
			output.Content = append(output.Content, sandbox.MCPContent{Type: "image", Data: value.Data, MIMEType: value.MIMEType})
		case *sdkmcp.AudioContent:
			output.Content = append(output.Content, sandbox.MCPContent{Type: "audio", Data: value.Data, MIMEType: value.MIMEType})
		default:
			encoded, _ := content.MarshalJSON()
			output.Content = append(output.Content, sandbox.MCPContent{Type: "resource", Text: string(encoded)})
		}
	}
	return output
}
