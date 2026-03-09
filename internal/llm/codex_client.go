package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

// Codex API constants
const (
	CodexBaseURL       = "https://chatgpt.com/backend-api/codex"
	CodexResponsesPath = "/responses"
)

// CodexClient is an LLM client that uses ChatGPT Codex OAuth
type CodexClient struct {
	oauthManager *CodexOAuthManager
	httpClient   *http.Client
	model        string
}

// NewCodexClient creates a new Codex client
func NewCodexClient(oauthManager *CodexOAuthManager, model string) *CodexClient {
	if model == "" {
		model = "gpt-4o" // Default model
	}
	return &CodexClient{
		oauthManager: oauthManager,
		httpClient:   &http.Client{Timeout: 5 * time.Minute},
		model:        model,
	}
}

// CodexRequest represents a request to the Codex Responses API
type CodexRequest struct {
	Model       string              `json:"model"`
	Input       []CodexInputItem    `json:"input,omitempty"`
	Instructions string             `json:"instructions,omitempty"`
	Tools       []CodexTool         `json:"tools,omitempty"`
	ToolChoice  interface{}         `json:"tool_choice,omitempty"`
	Stream      bool                `json:"stream"`
	Reasoning   *CodexReasoning     `json:"reasoning,omitempty"`
	Text        *CodexText          `json:"text,omitempty"`
	Store       bool                `json:"store,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// CodexInputItem represents an input item in Codex API
type CodexInputItem struct {
	Type      string           `json:"type"`
	Role      string           `json:"role,omitempty"`
	Content   interface{}      `json:"content,omitempty"`
	CallID    string           `json:"call_id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Arguments string           `json:"arguments,omitempty"`
	Output    string           `json:"output,omitempty"`
	Status    string           `json:"status,omitempty"`
}

// CodexTextContent represents text content
type CodexTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CodexTool represents a tool definition
type CodexTool struct {
	Type     string          `json:"type"`
	Function CodexFunction   `json:"function"`
}

// CodexFunction represents a function definition
type CodexFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// CodexReasoning represents reasoning configuration
type CodexReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CodexText represents text configuration
type CodexText struct {
	Verbosity string `json:"verbosity,omitempty"`
}

// CodexResponse represents a response from Codex API
type CodexResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Output  []CodexOutputItem   `json:"output"`
	Usage   *CodexUsage         `json:"usage,omitempty"`
	Status  string              `json:"status"`
	Error   *CodexError         `json:"error,omitempty"`
}

// CodexOutputItem represents an output item
type CodexOutputItem struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	Role      string             `json:"role,omitempty"`
	Content   []CodexContentPart `json:"content,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
}

// CodexContentPart represents a content part
type CodexContentPart struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	Refusal    string      `json:"refusal,omitempty"`
	Reasoning  string      `json:"reasoning,omitempty"`
	Summary    interface{} `json:"summary,omitempty"`
}

// CodexUsage represents token usage
type CodexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	CachedTokens      int64 `json:"cached_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
}

// CodexError represents an error response
type CodexError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// Chat sends a chat request via Codex API
func (c *CodexClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	log := logging.L().Named("codex_llm")
	start := time.Now()
	var out ChatResponse
	
	accessToken, err := c.oauthManager.GetAccessToken(ctx)
	if err != nil {
		return out, fmt.Errorf("failed to get access token: %w", err)
	}
	
	// Convert to Codex format
	codexReq := c.convertRequest(req)
	codexReq.Stream = true // Always stream for better UX
	
	reqBody, err := json.Marshal(codexReq)
	if err != nil {
		return out, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, 
		CodexBaseURL+CodexResponsesPath, bytes.NewReader(reqBody))
	if err != nil {
		return out, fmt.Errorf("failed to create request: %w", err)
	}
	
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	
	log.Debug("codex request",
		zap.String("model", codexReq.Model),
		zap.Int("input_items", len(codexReq.Input)),
		zap.Int("tools", len(codexReq.Tools)),
	)
	
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()
	
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return out, fmt.Errorf("codex API error (status %d): %s", httpResp.StatusCode, string(body))
	}
	
	// Parse SSE stream
	resp, err := c.parseStream(httpResp.Body, req.StreamHandler)
	if err != nil {
		return out, fmt.Errorf("failed to parse stream: %w", err)
	}
	
	out = c.convertResponse(resp)
	
	log.Debug("codex response",
		zap.Duration("latency", time.Since(start)),
		zap.Int("output_items", len(resp.Output)),
		zap.String("model", out.Model),
		zap.Int64("prompt_tokens", out.Usage.PromptTokens),
		zap.Int64("completion_tokens", out.Usage.CompletionTokens),
	)
	
	if len(out.Choices) == 0 || (out.Choices[0].Message.Content == "" && len(out.Choices[0].Message.ToolCalls) == 0) {
		return out, errors.New("empty codex response")
	}
	
	return out, nil
}

// convertRequest converts ChatRequest to CodexRequest
func (c *CodexClient) convertRequest(req ChatRequest) CodexRequest {
	out := CodexRequest{
		Model:  c.model,
		Input:  make([]CodexInputItem, 0, len(req.Messages)),
		Stream: true,
		Store:  true,
	}
	
	// Convert messages
	for _, msg := range req.Messages {
		item := CodexInputItem{
			Role: msg.Role,
		}
		
		if len(msg.ToolCalls) > 0 {
			// Tool call from assistant
			item.Type = "function_call"
			if len(msg.ToolCalls) > 0 {
				tc := msg.ToolCalls[0]
				item.CallID = tc.ID
				item.Name = tc.Function.Name
				item.Arguments = tc.Function.Arguments
			}
		} else if msg.ToolCallID != "" {
			// Tool response
			item.Type = "function_call_output"
			item.CallID = msg.ToolCallID
			item.Output = msg.Content
			item.Status = "completed"
		} else {
			// Regular message
			item.Type = "message"
			item.Content = []CodexTextContent{
				{Type: "input_text", Text: msg.Content},
			}
		}
		
		out.Input = append(out.Input, item)
	}
	
	// Convert tools
	if len(req.Tools) > 0 {
		out.Tools = make([]CodexTool, len(req.Tools))
		for i, tool := range req.Tools {
			out.Tools[i] = CodexTool{
				Type: "function",
				Function: CodexFunction{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				},
			}
		}
	}
	
	// Reasoning config
	if req.ReasoningEnabled {
		effort := req.ReasoningEffort
		if effort == "" {
			effort = "medium"
		}
		out.Reasoning = &CodexReasoning{
			Effort:  effort,
			Summary: "auto",
		}
	}
	
	return out
}

// convertResponse converts CodexResponse to ChatResponse
func (c *CodexClient) convertResponse(resp *CodexResponse) ChatResponse {
	out := ChatResponse{
		ID:      resp.ID,
		Created: resp.Created,
		Model:   resp.Model,
	}
	
	if resp.Usage != nil {
		out.Usage = Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	
	// Convert output items to message
	if len(resp.Output) > 0 {
		msg := Message{Role: "assistant"}
		var toolCalls []ToolCall
		
		for _, item := range resp.Output {
			switch item.Type {
			case "message":
				for _, part := range item.Content {
					switch part.Type {
					case "output_text":
						msg.Content += part.Text
					case "reasoning":
						msg.ReasoningContent += part.Reasoning
					}
				}
			case "function_call":
				tc := ToolCall{
					ID:   item.CallID,
					Type: "function",
					Function: ToolCallFn{
						Name:      item.Name,
						Arguments: item.Arguments,
					},
				}
				toolCalls = append(toolCalls, tc)
			}
		}
		
		msg.ToolCalls = toolCalls
		out.Choices = []ChatChoice{{Index: 0, Message: msg}}
	}
	
	return out
}

// parseStream parses SSE stream from Codex API
func (c *CodexClient) parseStream(body io.Reader, handler StreamHandler) (*CodexResponse, error) {
	var finalResp CodexResponse
	scanner := bufio.NewScanner(body)
	
	for scanner.Scan() {
		line := scanner.Text()
		
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		
		var event struct {
			Type   string         `json:"type"`
			Delta  *CodexDelta    `json:"delta,omitempty"`
			Output *CodexOutputItem `json:"output,omitempty"`
			Response *CodexResponse `json:"response,omitempty"`
		}
		
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		
		// Handle different event types
		switch event.Type {
		case "response.output_item.added":
			if event.Output != nil {
				finalResp.Output = append(finalResp.Output, *event.Output)
			}
		case "response.output_item.delta":
			if handler != nil && event.Delta != nil {
				if event.Delta.Content != "" {
					handler(StreamEvent{Delta: event.Delta.Content})
				}
				if event.Delta.Reasoning != "" {
					handler(StreamEvent{ReasoningDelta: event.Delta.Reasoning})
				}
			}
		case "response.completed":
			if event.Response != nil {
				finalResp = *event.Response
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}
	
	return &finalResp, nil
}

// CodexDelta represents a streaming delta
type CodexDelta struct {
	Content   string `json:"content,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}
