package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func (s *Session) callLLM(ctx context.Context, turn *TurnState, hooks MiddlewareSet) (llm.ChatResponse, error) {
	call := &LLMCall{
		Model: turn.Model,
		Tools: turn.Tools,
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
