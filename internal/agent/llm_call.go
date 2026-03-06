package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

const (
	contextRetryScale = 0.7
	maxContextRetries = 3
)

func (a *Agent) callLLMWithTrim(ctx context.Context, log *zap.Logger, sessionID int64, model string, messages []llm.Message, tools []llm.Tool) (llm.ChatResponse, []llm.Message, error) {
	var out llm.ChatResponse
	estimator := a.estimatorForModel(model)
	budgeter := NewContextBudgeter(estimator, a.contextConfig)
	if estimator == nil && log != nil {
		log.Warn("no token estimator available for model, trimming will use message count", zap.String("model", model))
	}
	scale := 1.0

	for attempt := 0; attempt <= maxContextRetries; attempt++ {
		trimmed, info := budgeter.Trim(messages, scale)
		if log != nil && len(trimmed) != len(messages) {
			log.Debug("context trimmed",
				zap.Int64("session_id", sessionID),
				zap.Int("attempt", attempt),
				zap.Float64("scale", scale),
				zap.String("mode", info.Mode),
				zap.Int("budget", info.Budget),
				zap.Int("tokens_used", info.TokensUsed),
				zap.Int("messages_before", len(messages)),
				zap.Int("messages_after", len(trimmed)),
			)
		}
		if attempt > 0 && log != nil {
			log.Warn("context retry",
				zap.Int64("session_id", sessionID),
				zap.Int("attempt", attempt),
				zap.String("mode", info.Mode),
				zap.Int("messages", info.Messages),
				zap.Int("budget", info.Budget),
				zap.Int("tokens_used", info.TokensUsed),
			)
		}
		resp, err := a.llm.Chat(ctx, llm.ChatRequest{
			Model:            model,
			Messages:         trimmed,
			Tools:            tools,
			ReasoningEnabled: a.reasoning.Enabled,
			ReasoningEffort:  a.reasoning.Effort,
		})
		if err != nil {
			if llm.IsContextWindowExceeded(err) && attempt < maxContextRetries {
				scale *= contextRetryScale
				continue
			}
			return resp, trimmed, err
		}
		return resp, trimmed, nil
	}
	return out, messages, llm.ErrContextWindowExceeded
}
