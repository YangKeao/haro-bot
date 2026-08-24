package mcpmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/sandbox"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

const builtinBrowserName = "agent_browser"

type Manager struct {
	db        *gorm.DB
	box       *sandbox.SecretBox
	sandboxes *sandbox.Service
	publicURL string
	artifacts ArtifactSink

	mu           sync.Mutex
	httpSessions map[string]*httpMCPSession
	active       map[int64]map[string]struct{}
}

type ArtifactSink interface {
	SaveMCPArtifact(ctx context.Context, sessionID int64, name, mimeType string, data []byte) (string, error)
}

type httpMCPSession struct {
	serverID  int64
	agentID   int64
	sessionID int64
	updatedAt time.Time
	session   *sdkmcp.ClientSession
}

type assignedServer struct {
	server  dbmodel.MCPServer
	binding dbmodel.AgentMCPServer
	builtin bool
}

func New(db *gorm.DB, box *sandbox.SecretBox, sandboxes *sandbox.Service, publicURL string) (*Manager, error) {
	if db == nil {
		return nil, errors.New("MCP database is required")
	}
	return &Manager{db: db, box: box, sandboxes: sandboxes, publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"), httpSessions: make(map[string]*httpMCPSession), active: make(map[int64]map[string]struct{})}, nil
}

func (m *Manager) SetArtifactSink(sink ArtifactSink) { m.artifacts = sink }

func (m *Manager) ResetSession(sessionID int64) {
	m.mu.Lock()
	delete(m.active, sessionID)
	m.mu.Unlock()
}

func (m *Manager) Activate(sessionID int64, names ...string) {
	m.mu.Lock()
	if m.active[sessionID] == nil {
		m.active[sessionID] = make(map[string]struct{})
	}
	for _, name := range names {
		m.active[sessionID][name] = struct{}{}
	}
	m.mu.Unlock()
}

func (m *Manager) IsActive(sessionID int64, name string) bool {
	m.mu.Lock()
	_, ok := m.active[sessionID][name]
	m.mu.Unlock()
	return ok
}

func (m *Manager) CloseChat(ctx context.Context, agentID, sessionID int64) {
	prefix := fmt.Sprintf("%d:%d:", agentID, sessionID)
	m.mu.Lock()
	for key, client := range m.httpSessions {
		if strings.HasPrefix(key, prefix) {
			delete(m.httpSessions, key)
			_ = client.session.Close()
		}
	}
	delete(m.active, sessionID)
	m.mu.Unlock()
	if m.sandboxes != nil && m.sandboxes.Enabled() {
		_ = m.sandboxes.CloseMCPSession(ctx, agentID, browserSessionKey(agentID, sessionID, 0))
		var bindings []dbmodel.AgentMCPServer
		if err := m.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&bindings).Error; err == nil {
			for _, binding := range bindings {
				_ = m.sandboxes.CloseMCPSession(ctx, agentID, browserSessionKey(agentID, sessionID, binding.ServerID))
			}
		}
	}
}

func (m *Manager) CloseAgent(ctx context.Context, agentID int64) {
	var sessions []dbmodel.Session
	if err := m.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&sessions).Error; err != nil {
		return
	}
	for _, session := range sessions {
		m.CloseChat(ctx, agentID, session.ID)
	}
}

func (m *Manager) SourceDescription(ctx context.Context, agentID int64, sandboxEnabled bool) string {
	names := make([]string, 0)
	if sandboxEnabled {
		names = append(names, builtinBrowserName+" (browser automation)")
	}
	servers, _ := m.assignedServers(ctx, agentID, sandboxEnabled)
	for _, item := range servers {
		if !item.builtin {
			names = append(names, item.server.Name)
		}
	}
	if len(names) == 0 {
		return "Search deferred MCP tools assigned to this agent. No MCP sources are currently available."
	}
	return "Search deferred MCP tools assigned to this agent. Sources: " + strings.Join(names, ", ") + ". Exact tool names rank first; results activate tools for this user turn."
}

func (m *Manager) Catalog(ctx context.Context, agentID, sessionID int64, sandboxEnabled bool) ([]CatalogTool, []string) {
	servers, err := m.assignedServers(ctx, agentID, sandboxEnabled)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var catalog []CatalogTool
	var warnings []string
	for _, item := range servers {
		tools, err := m.listTools(ctx, agentID, sessionID, item)
		if err != nil {
			warnings = append(warnings, item.server.Name+": "+err.Error())
			m.recordRefresh(item.server.ID, err)
			continue
		}
		m.recordRefresh(item.server.ID, nil)
		for _, tool := range tools {
			if !toolAllowed(tool.Name, item.binding) {
				continue
			}
			callable := callableName(item.server.Name, tool.Name)
			if item.builtin {
				callable = tool.Name
				tool.InputSchema = sanitizeBrowserSchema(tool.InputSchema)
			}
			catalog = append(catalog, CatalogTool{ServerID: item.server.ID, ServerName: item.server.Name, RawName: tool.Name, CallableName: callable, Title: tool.Title, Description: tool.Description, InputSchema: tool.InputSchema, Builtin: item.builtin})
		}
	}
	return catalog, warnings
}

func (m *Manager) assignedServers(ctx context.Context, agentID int64, sandboxEnabled bool) ([]assignedServer, error) {
	var result []assignedServer
	if sandboxEnabled {
		result = append(result, assignedServer{builtin: true, server: dbmodel.MCPServer{ID: 0, Name: builtinBrowserName, Description: "Browser automation", Transport: TransportStdio, Command: "agent-browser", Enabled: true}})
	}
	var bindings []dbmodel.AgentMCPServer
	if err := m.db.WithContext(ctx).Where("agent_id = ? AND enabled = ?", agentID, true).Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		var server dbmodel.MCPServer
		if err := m.db.WithContext(ctx).Where("id = ? AND enabled = ?", binding.ServerID, true).First(&server).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		result = append(result, assignedServer{server: server, binding: binding})
	}
	return result, nil
}

func (m *Manager) listTools(ctx context.Context, agentID, sessionID int64, item assignedServer) ([]sandbox.MCPTool, error) {
	if item.server.Transport == TransportStdio {
		if m.sandboxes == nil || !m.sandboxes.Enabled() {
			return nil, errors.New("stdio MCP requires an active Sandbox")
		}
		request, err := m.sandboxRequest(ctx, agentID, sessionID, item)
		if err != nil {
			return nil, err
		}
		return m.sandboxes.ListMCPTools(ctx, agentID, request)
	}
	session, err := m.httpSession(ctx, agentID, sessionID, item.server)
	if err != nil {
		return nil, err
	}
	var output []sandbox.MCPTool
	params := &sdkmcp.ListToolsParams{}
	for {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		for _, tool := range result.Tools {
			schema := map[string]any{"type": "object"}
			encoded, _ := json.Marshal(tool.InputSchema)
			_ = json.Unmarshal(encoded, &schema)
			title := ""
			if tool.Annotations != nil {
				title = tool.Annotations.Title
			}
			output = append(output, sandbox.MCPTool{Name: tool.Name, Title: title, Description: tool.Description, InputSchema: schema})
		}
		if result.NextCursor == "" {
			break
		}
		params.Cursor = result.NextCursor
	}
	return output, nil
}

func (m *Manager) Call(ctx context.Context, agentID, sessionID int64, tool CatalogTool, args json.RawMessage) (sandbox.MCPCallResult, error) {
	servers, err := m.assignedServers(ctx, agentID, true)
	if err != nil {
		return sandbox.MCPCallResult{}, err
	}
	var selected *assignedServer
	for i := range servers {
		if servers[i].server.ID == tool.ServerID && servers[i].server.Name == tool.ServerName {
			selected = &servers[i]
			break
		}
	}
	if selected == nil || !toolAllowed(tool.RawName, selected.binding) {
		return sandbox.MCPCallResult{}, errors.New("MCP tool is no longer assigned or allowed")
	}
	if selected.server.Transport == TransportStdio {
		request, err := m.sandboxRequest(ctx, agentID, sessionID, *selected)
		if err != nil {
			return sandbox.MCPCallResult{}, err
		}
		return m.sandboxes.CallMCPTool(ctx, agentID, sandbox.MCPCallRequest{Server: request, Name: tool.RawName, Arguments: args})
	}
	session, err := m.httpSession(ctx, agentID, sessionID, selected.server)
	if err != nil {
		return sandbox.MCPCallResult{}, err
	}
	var arguments any = map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return sandbox.MCPCallResult{}, err
		}
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool.RawName, Arguments: arguments})
	if err != nil {
		return sandbox.MCPCallResult{}, err
	}
	return convertSDKResult(result), nil
}

func (m *Manager) sandboxRequest(ctx context.Context, agentID, sessionID int64, item assignedServer) (sandbox.MCPServerRequest, error) {
	request := sandbox.MCPServerRequest{Key: browserSessionKey(agentID, sessionID, item.server.ID), AgentID: agentID, SessionID: sessionID, Command: item.server.Command, Workdir: "/workspace"}
	if item.builtin {
		request.Args = []string{"mcp", "--tools", "core"}
		request.Environment = map[string]string{"AGENT_BROWSER_SESSION": fmt.Sprintf("haro-a%d-s%d", agentID, sessionID), "AGENT_BROWSER_CONTENT_BOUNDARIES": "1", "AGENT_BROWSER_MAX_OUTPUT_CHARS": "12000"}
		return request, nil
	}
	_ = json.Unmarshal(item.server.ArgsJSON, &request.Args)
	environment, _, _, err := m.credentials(ctx, agentID, item.server.ID)
	request.Environment = environment
	return request, err
}

func (m *Manager) httpSession(ctx context.Context, agentID, sessionID int64, server dbmodel.MCPServer) (*sdkmcp.ClientSession, error) {
	key := httpSessionKey(agentID, sessionID, server.ID)
	m.mu.Lock()
	if existing := m.httpSessions[key]; existing != nil && existing.updatedAt.Equal(server.UpdatedAt) {
		m.mu.Unlock()
		return existing.session, nil
	}
	m.mu.Unlock()
	_, headers, credential, err := m.credentials(ctx, agentID, server.ID)
	if err != nil {
		return nil, err
	}
	if credential != nil && credential.AccessTokenCiphertext != "" {
		token, err := m.accessToken(ctx, server, credential)
		if err != nil {
			return nil, err
		}
		typeName := strings.TrimSpace(credential.TokenType)
		if typeName == "" {
			typeName = "Bearer"
		}
		headers["Authorization"] = typeName + " " + token
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: &headerRoundTripper{base: http.DefaultTransport, headers: headers}}
	transport := &sdkmcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client, MaxRetries: 2, DisableStandaloneSSE: true}
	connected, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "haro-bot", Version: "1"}, &sdkmcp.ClientOptions{Capabilities: &sdkmcp.ClientCapabilities{}}).Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if old := m.httpSessions[key]; old != nil {
		_ = old.session.Close()
	}
	m.httpSessions[key] = &httpMCPSession{serverID: server.ID, agentID: agentID, sessionID: sessionID, updatedAt: server.UpdatedAt, session: connected}
	m.mu.Unlock()
	return connected, nil
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (r *headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, value := range r.headers {
		clone.Header.Set(name, value)
	}
	return r.base.RoundTrip(clone)
}

func (m *Manager) recordRefresh(serverID int64, refreshErr error) {
	if serverID == 0 {
		return
	}
	now := time.Now()
	updates := map[string]any{"last_refresh_at": now}
	if refreshErr == nil {
		updates["last_error"] = nil
	} else {
		message := refreshErr.Error()
		updates["last_error"] = message
	}
	_ = m.db.Model(&dbmodel.MCPServer{}).Where("id = ?", serverID).Updates(updates).Error
}

func (m *Manager) closeServerSessions(serverID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.httpSessions {
		if client.serverID == serverID {
			delete(m.httpSessions, key)
			_ = client.session.Close()
		}
	}
}

func (m *Manager) closeAgentServerSessions(agentID, serverID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.httpSessions {
		if client.serverID == serverID && client.agentID == agentID {
			delete(m.httpSessions, key)
			_ = client.session.Close()
		}
	}
}

func toolAllowed(name string, binding dbmodel.AgentMCPServer) bool {
	var allowed, denied []string
	_ = json.Unmarshal(binding.AllowedTools, &allowed)
	_ = json.Unmarshal(binding.DeniedTools, &denied)
	for _, item := range denied {
		if item == name {
			return false
		}
	}
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if item == name {
			return true
		}
	}
	return false
}

var invalidToolName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func callableName(server, tool string) string {
	raw := "mcp__" + strings.ToLower(server) + "__" + tool
	base := invalidToolName.ReplaceAllString(raw, "_")
	hash := sha256.Sum256([]byte(server + "\x00" + tool))
	if base == raw && len(base) <= 64 {
		return base
	}
	hashBytes := 3
	if len(base) > 64 {
		hashBytes = 6
	}
	suffix := "_" + hex.EncodeToString(hash[:hashBytes])
	limit := 64 - len(suffix)
	if len(base) > limit {
		base = base[:limit]
	}
	return strings.TrimRight(base, "_") + suffix
}

func sanitizeBrowserSchema(schema map[string]any) map[string]any {
	encoded, _ := json.Marshal(schema)
	var result map[string]any
	_ = json.Unmarshal(encoded, &result)
	properties, _ := result["properties"].(map[string]any)
	for _, name := range []string{"session", "namespace", "allowedDomains", "extraArgs", "extra_args", "restore", "restoreCheckFn", "restoreCheckText", "restoreCheckUrl", "restoreSave", "idleTimeout", "all"} {
		delete(properties, name)
	}
	return result
}

func browserSessionKey(agentID, sessionID, serverID int64) string {
	return fmt.Sprintf("a%d-s%d-m%d", agentID, sessionID, serverID)
}
func httpSessionKey(agentID, sessionID, serverID int64) string {
	return fmt.Sprintf("%d:%d:%d", agentID, sessionID, serverID)
}

func convertSDKResult(result *sdkmcp.CallToolResult) sandbox.MCPCallResult {
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

func rankCatalog(catalog []CatalogTool, query string, limit int) []CatalogTool {
	if limit <= 0 {
		limit = 8
	}
	if limit > 16 {
		limit = 16
	}
	query = strings.ToLower(strings.TrimSpace(query))
	terms := strings.Fields(query)
	type scored struct {
		tool  CatalogTool
		score int
	}
	scores := make([]scored, 0, len(catalog))
	for _, tool := range catalog {
		name := strings.ToLower(tool.RawName)
		callable := strings.ToLower(tool.CallableName)
		haystack := strings.ToLower(tool.ServerName + " " + tool.Title + " " + tool.Description + " " + tool.RawName)
		score := 0
		if query == name || query == callable {
			score += 10000
		}
		if strings.Contains(name, query) || strings.Contains(callable, query) {
			score += 1000
		}
		for _, term := range terms {
			score += strings.Count(haystack, term) * 10
		}
		if query == "" {
			score = 1
		}
		if score > 0 {
			scores = append(scores, scored{tool: tool, score: score})
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].tool.CallableName < scores[j].tool.CallableName
		}
		return scores[i].score > scores[j].score
	})
	if len(scores) > limit {
		scores = scores[:limit]
	}
	out := make([]CatalogTool, 0, len(scores))
	for _, item := range scores {
		out = append(out, item.tool)
	}
	return out
}
