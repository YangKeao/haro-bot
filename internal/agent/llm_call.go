package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

func (s *Session) callLLM(ctx context.Context, _ *zap.Logger, turn *TurnState, hooks MiddlewareSet, tools []llm.Tool) (llm.ChatResponse, error) {
	if tools == nil {
		tools = turn.Tools
	}
	call := &LLMCall{
		Model: turn.Model,
		Tools: tools,
	}
	return executeLLMMiddleware(ctx, hooks.LLMMiddleware, turn, call, func(ctx context.Context, turn *TurnState, call *LLMCall) (llm.ChatResponse, error) {
		handler := func(event llm.StreamEvent) {
			executeLLMDeltaListeners(ctx, hooks.LLMDeltaListeners, turn, event)
		}
		return s.deps.llm.Chat(ctx, llm.ChatRequest{
			Model:            turn.Model,
			Messages:         turn.LLMMessages(),
			Tools:            call.Tools,
			ReasoningEnabled: s.deps.reasoning.Enabled,
			ReasoningEffort:  s.deps.reasoning.Effort,
			StreamHandler:    handler,
			Purpose:          llm.PurposeChat,
		})
	})
}
