package llm

type Message struct {
	Role             string         `json:"role"`
	Content          string         `json:"content,omitempty"`
	Images           []ImageContent `json:"images,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	TraceSteps       []TraceStep    `json:"trace_steps,omitempty"`
}

// TraceStep is a display-oriented record of one model activity. It is kept
// separate from the provider replay fields above so richer UI history never
// changes the conversation sent back to a model.
type TraceStep struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ToolKind  string `json:"tool_kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Status    string `json:"status,omitempty"`
	Content   string `json:"content,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Detail    any    `json:"detail,omitempty"`
	Order     int64  `json:"order,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ImageContent struct {
	URL      string `json:"url"`
	MIMEType string `json:"mime_type,omitempty"`
	Name     string `json:"name,omitempty"`
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

type ChatRequest struct {
	Model            string        `json:"model"`
	Messages         []Message     `json:"messages"`
	Tools            []Tool        `json:"tools,omitempty"`
	ToolChoice       any           `json:"tool_choice,omitempty"`
	Temperature      float64       `json:"temperature,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	ReasoningEnabled bool          `json:"reasoning_enabled,omitempty"`
	ReasoningEffort  string        `json:"reasoning_effort,omitempty"`
	HostedWebSearch  bool          `json:"hosted_web_search,omitempty"`
	StreamHandler    StreamHandler `json:"-"`
}

type StreamEvent struct {
	Delta          string
	ReasoningDelta string
	Trace          *TraceEvent
}

// TraceEvent carries lifecycle changes for an ordered trace step. Sequence is
// provider-local; web transports assign their own run-wide monotonic sequence.
type TraceEvent struct {
	Phase    string
	Sequence int64
	Step     TraceStep
	Delta    string
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
