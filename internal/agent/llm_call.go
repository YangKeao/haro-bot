package agent

import (
	"context"
	"errors"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

const (
	contextRetryScale = 0.7
	maxContextRetries = 3
)

func (s *Session) callLLMWithTrim(ctx context.Context, log *zap.Logger, model string, messages []llm.Message, tools []llm.Tool, observer ProgressObserver) (llm.ChatResponse, []llm.Message, error) {
	var out llm.ChatResponse
	if s == nil || s.deps == nil {
		return out, messages, errors.New("session not configured")
	}
	estimator := s.estimatorForModel(model)
	budgeter := NewContextBudgeter(estimator, s.deps.contextConfig)
	if estimator == nil && log != nil {
		log.Warn("no token estimator available for model, trimming will use message count", zap.String("model", model))
	}

	budget := computeTokenBudget(s.deps.contextConfig)

	// Check if we should auto-compact before trimming
	compactor := NewCompactor(s.deps.store, s.deps.llm, estimator, model)
	if compactor.ShouldCompact(messages, budget.InputBudget) {
		log.Info("context approaching limit, attempting auto-compact",
			zap.Int64("session_id", s.id),
			zap.Int("messages", len(messages)),
		)
		summary, err := compactor.Compact(ctx, s.id, messages, budget.InputBudget)
		if err != nil {
			log.Warn("auto-compact failed, falling back to trim", zap.Error(err))
		} else if summary != nil {
			// Reload messages from the new view
			recent, _, err := s.deps.store.LoadViewMessages(ctx, s.id, 0)
			if err != nil {
				log.Warn("failed to reload after compact", zap.Error(err))
			} else {
				// Rebuild messages with the new compacted view
				// Preserve system messages
				var systemMsgs []llm.Message
				for _, msg := range messages {
					if msg.Role == "system" {
						systemMsgs = append(systemMsgs, msg)
					}
				}
				summaryMsg := formatSummaryMessage(summary)
				if summaryMsg != "" {
					systemMsgs = append(systemMsgs, llm.Message{Role: "system", Content: summaryMsg})
				}
				messages = append(systemMsgs, toLLMMessages(recent)...)
				log.Info("context compacted successfully",
					zap.Int64("session_id", s.id),
					zap.Int("new_message_count", len(messages)),
				)
			}
		}
	}

	scale := 1.0

	for attempt := 0; attempt <= maxContextRetries; attempt++ {
		trimmed, info := budgeter.Trim(messages, scale)
		if log != nil {
			log.Debug("context estimate",
				zap.Int64("session_id", s.id),
				zap.Int("attempt", attempt),
				zap.Float64("scale", scale),
				zap.String("mode", info.Mode),
				zap.Int("budget", info.Budget),
				zap.Int("tokens_used", info.TokensUsed),
				zap.Int("messages_before", len(messages)),
				zap.Int("messages_after", len(trimmed)),
			)
		}
		if log != nil && len(trimmed) != len(messages) {
			log.Debug("context trimmed",
				zap.Int64("session_id", s.id),
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
				zap.Int64("session_id", s.id),
				zap.Int("attempt", attempt),
				zap.String("mode", info.Mode),
				zap.Int("messages", info.Messages),
				zap.Int("budget", info.Budget),
				zap.Int("tokens_used", info.TokensUsed),
			)
		}
		if observer != nil {
			observer.OnLLMStart(ctx, LLMStartInfo{Model: model, Attempt: attempt})
		}
		var handler llm.StreamHandler
		if observer != nil {
			handler = func(event llm.StreamEvent) {
				if event.Delta != "" {
					observer.OnLLMStreamDelta(ctx, event.Delta)
				}
			}
		}
		resp, err := s.deps.llm.Chat(ctx, llm.ChatRequest{
			Model:            model,
			Messages:         trimmed,
			Tools:            tools,
			ReasoningEnabled: s.deps.reasoning.Enabled,
			ReasoningEffort:  s.deps.reasoning.Effort,
			StreamHandler:    handler,
			Purpose:          llm.PurposeChat,
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
