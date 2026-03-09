package llm

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function FunctionSpec `json:"function"`
}

type FunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function ToolCallFn `json:"function"`
}

type ToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type RequestPurpose string

const (
	PurposeChat    RequestPurpose = "chat"
	PurposeMemory  RequestPurpose = "memory"
	PurposeSummary RequestPurpose = "summary"
	PurposeSecurity RequestPurpose = "security"
)

type ChatRequest struct {
	Model            string         `json:"model"`
	Messages         []Message      `json:"messages"`
	Tools            []Tool         `json:"tools,omitempty"`
	ToolChoice       any            `json:"tool_choice,omitempty"`
	Temperature      float64        `json:"temperature,omitempty"`
	Stream           bool           `json:"stream,omitempty"`
	ReasoningEnabled bool           `json:"reasoning_enabled,omitempty"`
	ReasoningEffort  string         `json:"reasoning_effort,omitempty"`
	StreamHandler    StreamHandler  `json:"-"`
	Purpose          RequestPurpose `json:"-"`
}

type StreamEvent struct {
	Delta string
}

type StreamHandler func(StreamEvent)

type ReasoningConfig struct {
	Enabled bool
	Effort  string
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Usage   Usage        `json:"usage,omitempty"`
	Choices []ChatChoice `json:"choices"`
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type ChatChoice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
}
