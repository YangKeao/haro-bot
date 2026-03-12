package llm

import "context"

// ChatModel is the provider boundary used by higher-level components.
// Implementations may call OpenAI-compatible APIs or any other SDK.
type ChatModel interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

