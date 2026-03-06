package agent

import "github.com/YangKeao/haro-bot/internal/llm"

type tokenBudget struct {
	Effective     int
	AutoCompact   int
	InputBudget   int
	SummaryBudget int
}

func computeTokenBudget(cfg llm.ContextConfig) tokenBudget {
	effective := cfg.EffectiveWindowTokens()
	autoCompact := cfg.AutoCompactLimit()
	inputBudget := effective
	if inputBudget == 0 && autoCompact > 0 {
		inputBudget = autoCompact
	}
	summaryBudget := autoCompact
	if summaryBudget == 0 {
		summaryBudget = inputBudget
	}
	return tokenBudget{
		Effective:     effective,
		AutoCompact:   autoCompact,
		InputBudget:   inputBudget,
		SummaryBudget: summaryBudget,
	}
}
