package agent

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

func (s *Session) callLLM(ctx context.Context, log *zap.Logger, turn *TurnState, hooks HookSet, tools []llm.Tool) (llm.ChatResponse, error) {
	var out llm.ChatResponse

	if tools == nil {
		tools = turn.Tools
	}

	for attempt := 0; attempt < 2; attempt++ {
		call := &LLMCall{
			Model:   turn.Model,
			Attempt: attempt,
			Tools:   tools,
		}
		if err := executeTurnBeforeLLMHooks(ctx, hooks.TurnHooks, turn, call); err != nil {
			return out, err
		}
		if err := executeTurnLLMStartHooks(ctx, hooks.TurnHooks, turn, LLMStartInfo{Model: turn.Model, Attempt: attempt}); err != nil {
			return out, err
		}

		handler := func(event llm.StreamEvent) {
			executeTurnLLMDeltaHooks(ctx, hooks.TurnHooks, turn, event)
		}

		resp, err := s.deps.llm.Chat(ctx, llm.ChatRequest{
			Model:            turn.Model,
			Messages:         turn.LLMMessages(),
			Tools:            tools,
			ReasoningEnabled: s.deps.reasoning.Enabled,
			ReasoningEffort:  s.deps.reasoning.Effort,
			StreamHandler:    handler,
			Purpose:          llm.PurposeChat,
		})
		if err == nil {
			return resp, nil
		}
		retry, hookErr := executeTurnLLMErrorHooks(ctx, hooks.TurnHooks, turn, call, err)
		if hookErr != nil {
			return out, hookErr
		}
		if !retry {
			log.Error("llm chat error", zap.Int64("session_id", turn.Run.SessionID), zap.Error(err))
			return resp, err
		}
	}

	return out, errors.New("llm retry limit exceeded")
}
