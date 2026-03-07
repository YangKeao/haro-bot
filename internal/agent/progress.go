package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
)

type LLMStartInfo struct {
	Model   string
	Attempt int
}

type ProgressObserver interface {
	OnLLMStart(ctx context.Context, info LLMStartInfo)
	OnLLMStreamDelta(ctx context.Context, delta string)
	OnToolCalls(ctx context.Context, calls []llm.ToolCall)
}
