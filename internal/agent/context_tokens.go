package agent

import (
	"sort"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

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

func selectMessagesByTokens(messages []memory.Message, estimator *llm.TokenEstimator, budget int) ([]memory.Message, int) {
	if len(messages) == 0 {
		return nil, 0
	}
	if estimator == nil || budget <= 0 {
		return messages, 0
	}
	requiredToolCalls := map[string]struct{}{}
	selected := make([]memory.Message, 0, len(messages))
	used := 0
	includedAny := false

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		mustInclude := false
		if msg.Role == "tool" && msg.Metadata != nil && msg.Metadata.ToolCallID != "" {
			requiredToolCalls[msg.Metadata.ToolCallID] = struct{}{}
			mustInclude = true
		}
		if msg.Role == "assistant" && msg.Metadata != nil && len(msg.Metadata.ToolCalls) > 0 {
			for _, call := range msg.Metadata.ToolCalls {
				if call.ID == "" {
					continue
				}
				if _, ok := requiredToolCalls[call.ID]; ok {
					mustInclude = true
					delete(requiredToolCalls, call.ID)
				}
			}
		}
		if !includedAny {
			mustInclude = true
		}

		tokens := estimator.CountMessage(toLLMMessage(msg))
		if mustInclude || used+tokens <= budget {
			used += tokens
			selected = append(selected, msg)
			includedAny = true
			continue
		}
		if len(requiredToolCalls) == 0 {
			break
		}
	}

	sort.SliceStable(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	return selected, used
}
