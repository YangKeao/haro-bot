package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

const (
	contextRetryScale = 0.7
	maxContextRetries = 10
)

func (a *Agent) callLLMWithTrim(ctx context.Context, log *zap.Logger, sessionID int64, model string, messages []llm.Message, tools []llm.Tool) (llm.ChatResponse, []llm.Message, error) {
	var out llm.ChatResponse
	estimator := a.estimatorForModel(model)
	budget := computeTokenBudget(a.contextConfig).InputBudget
	totalTokens := 0
	if estimator != nil {
		totalTokens = estimator.CountMessages(messages)
		log.Debug("initial token count for messages", zap.Int("tokens", totalTokens), zap.Int("budget", budget))
	} else {
		log.Warn("no token estimator available for model, skipping token-based trimming", zap.String("model", model))
	}
	if budget <= 0 && totalTokens > 0 {
		budget = totalTokens
	}
	scale := 1.0

	for attempt := 0; attempt <= maxContextRetries; attempt++ {
		var trimmed []llm.Message
		scaledBudget := 0
		maxMessages := 0
		if estimator != nil && budget > 0 {
			scaledBudget = scaleBudget(budget, scale)
			trimmed = trimMessagesForBudget(messages, estimator, scaledBudget)
		} else {
			maxMessages = scaleCount(len(messages), scale)
			trimmed = trimMessagesForCount(messages, maxMessages)
		}
		if len(trimmed) == 0 && len(messages) > 0 {
			trimmed = lastNonToolMessage(messages)
			if len(trimmed) == 0 {
				trimmed = messages[len(messages)-1:]
			}
		}
		if attempt > 0 && log != nil {
			log.Warn("context retry",
				zap.Int64("session_id", sessionID),
				zap.Int("attempt", attempt),
				zap.Int("messages", len(trimmed)),
				zap.Int("budget", scaledBudget),
				zap.Int("max_messages", maxMessages),
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

func scaleBudget(budget int, scale float64) int {
	if budget <= 0 {
		return budget
	}
	if scale <= 0 {
		return 1
	}
	if scale >= 1 {
		return budget
	}
	scaled := int(float64(budget) * scale)
	if scaled <= 0 {
		return 1
	}
	return scaled
}

func scaleCount(count int, scale float64) int {
	if count <= 0 {
		return count
	}
	if scale <= 0 {
		return 1
	}
	if scale >= 1 {
		return count
	}
	scaled := int(float64(count) * scale)
	if scaled <= 0 {
		return 1
	}
	return scaled
}

func lastNonToolMessage(messages []llm.Message) []llm.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "tool" {
			return messages[i:]
		}
	}
	return nil
}
