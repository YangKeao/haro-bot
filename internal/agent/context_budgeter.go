package agent

import "github.com/YangKeao/haro-bot/internal/llm"

type BudgetInfo struct {
	Mode       string
	Budget     int
	TokensUsed int
	Messages   int
}

type ContextBudgeter struct {
	estimator *llm.TokenEstimator
	budget    int
}

func NewContextBudgeter(estimator *llm.TokenEstimator, cfg llm.ContextConfig) *ContextBudgeter {
	budget := computeTokenBudget(cfg).InputBudget
	return &ContextBudgeter{estimator: estimator, budget: budget}
}

func (b *ContextBudgeter) Trim(messages []llm.Message, scale float64) ([]llm.Message, BudgetInfo) {
	if len(messages) == 0 {
		return nil, BudgetInfo{}
	}
	info := BudgetInfo{}
	if b == nil {
		return messages, info
	}
	budget := b.budget
	if b.estimator != nil {
		totalTokens := b.estimator.CountMessages(messages)
		if budget <= 0 && totalTokens > 0 {
			budget = totalTokens
		}
	}
	if b.estimator != nil && budget > 0 {
		scaledBudget := scaleBudget(budget, scale)
		trimmed := trimMessagesForBudget(messages, b.estimator, scaledBudget)
		if len(trimmed) == 0 {
			trimmed = lastNonToolMessage(messages)
		}
		if len(trimmed) == 0 {
			trimmed = messages[len(messages)-1:]
		}
		info.Mode = "tokens"
		info.Budget = scaledBudget
		info.TokensUsed = b.estimator.CountMessages(trimmed)
		info.Messages = len(trimmed)
		return trimmed, info
	}
	maxMessages := scaleCount(len(messages), scale)
	trimmed := trimMessagesForCount(messages, maxMessages)
	if len(trimmed) == 0 {
		trimmed = lastNonToolMessage(messages)
	}
	if len(trimmed) == 0 {
		trimmed = messages[len(messages)-1:]
	}
	info.Mode = "count"
	info.Budget = maxMessages
	info.Messages = len(trimmed)
	return trimmed, info
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
