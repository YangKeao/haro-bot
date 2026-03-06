package agent

import "github.com/YangKeao/haro-bot/internal/llm"

type tokenBudget struct {
	Effective    int
	AutoCompact  int
	InputBudget  int
	AnchorBudget int
}

func computeTokenBudget(cfg llm.ContextConfig) tokenBudget {
	effective := cfg.EffectiveWindowTokens()
	autoCompact := cfg.AutoCompactLimit()
	inputBudget := effective
	if inputBudget == 0 && autoCompact > 0 {
		inputBudget = autoCompact
	}
	anchorBudget := autoCompact
	if anchorBudget == 0 {
		anchorBudget = inputBudget
	}
	return tokenBudget{
		Effective:    effective,
		AutoCompact:  autoCompact,
		InputBudget:  inputBudget,
		AnchorBudget: anchorBudget,
	}
}
