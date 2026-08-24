package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type MCPServerRequest struct {
	Key         string            `json:"key"`
	AgentID     int64             `json:"agent_id"`
	SessionID   int64             `json:"session_id"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Workdir     string            `json:"workdir,omitempty"`
}

type MCPTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type MCPCallRequest struct {
	Server    MCPServerRequest `json:"server"`
	Name      string           `json:"name"`
	Arguments json.RawMessage  `json:"arguments"`
}

type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     []byte `json:"data,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

type MCPCallResult struct {
	Content           []MCPContent `json:"content"`
	StructuredContent any          `json:"structured_content,omitempty"`
	IsError           bool         `json:"is_error"`
}

type MCPRuntime interface {
	ListMCPTools(context.Context, RuntimeTarget, MCPServerRequest) ([]MCPTool, error)
	CallMCPTool(context.Context, RuntimeTarget, MCPCallRequest) (MCPCallResult, error)
	CloseMCPSession(context.Context, RuntimeTarget, string) error
}

func (r *HTTPRuntime) ListMCPTools(ctx context.Context, target RuntimeTarget, input MCPServerRequest) ([]MCPTool, error) {
	var output struct {
		Tools []MCPTool `json:"tools"`
	}
	err := r.request(ctx, target, http.MethodPost, "/v1/mcp/tools", input, &output, r.timeout())
	return output.Tools, err
}

func (r *HTTPRuntime) CallMCPTool(ctx context.Context, target RuntimeTarget, input MCPCallRequest) (MCPCallResult, error) {
	var output MCPCallResult
	err := r.request(ctx, target, http.MethodPost, "/v1/mcp/call", input, &output, r.timeout())
	return output, err
}

func (r *HTTPRuntime) CloseMCPSession(ctx context.Context, target RuntimeTarget, key string) error {
	return r.request(ctx, target, http.MethodDelete, "/v1/mcp/sessions/"+url.PathEscape(strings.TrimSpace(key)), nil, nil, r.timeout())
}
