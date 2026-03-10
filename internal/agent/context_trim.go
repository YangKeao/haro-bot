package agent

import (
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

func trimMessagesForBudget(messages []llm.Message, estimator *llm.TokenEstimator, budget int) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	if estimator == nil || budget <= 0 {
		return messages
	}
	system := make([]llm.Message, 0, len(messages))
	rest := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			system = append(system, msg)
		} else {
			rest = append(rest, msg)
		}
	}
	baseTokens := estimator.CountMessages(system)
	available := budget - baseTokens
	if available <= 0 {
		return system
	}
	trimmed := selectLLMMessagesByTokens(rest, estimator, available)
	if len(system) == 0 {
		return trimmed
	}
	return append(system, trimmed...)
}


func selectLLMMessagesByTokens(messages []llm.Message, estimator *llm.TokenEstimator, budget int) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	if estimator == nil || budget <= 0 {
		return messages
	}
	requiredToolCalls := map[string]struct{}{}
	selected := make([]llm.Message, 0, len(messages))
	used := 0
	includedAny := false
	seenTool := false
	log := logging.L().Named("context_trim")

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		mustInclude := false
		if msg.Role == "tool" && msg.ToolCallID == "" {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			paired := make([]llm.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if call.ID == "" {
					continue
				}
				if _, ok := requiredToolCalls[call.ID]; ok {
					paired = append(paired, call)
					delete(requiredToolCalls, call.ID)
				}
			}
			if len(paired) == 0 && msg.Content == "" {
				continue
			}
			if len(paired) == 0 {
				msg.ToolCalls = nil
			} else {
				msg.ToolCalls = paired
				mustInclude = true
			}
		}
		firstTool := msg.Role == "tool" && !seenTool
		if msg.Role == "tool" {
			seenTool = true
		}
		if !includedAny && msg.Role != "tool" {
			mustInclude = true
		}

		tokens := estimator.CountMessage(msg)
		if firstTool && tokens > budget {
			originalTokens := tokens
			msg = llm.Message{
				Role:       "tool",
				ToolCallID: msg.ToolCallID,
				Content:    "tool response omitted: output too large for context window",
			}
			tokens = estimator.CountMessage(msg)
			log.Debug("rewrote oversized tool response", zap.String("tool_call_id", msg.ToolCallID), zap.Int("original_tokens", originalTokens), zap.Int("rewritten_tokens", tokens), zap.Int("budget", budget))
		}
		if mustInclude || used+tokens <= budget {
			used += tokens
			selected = append(selected, msg)
			includedAny = true
			if msg.Role == "tool" && msg.ToolCallID != "" {
				requiredToolCalls[msg.ToolCallID] = struct{}{}
			}
			continue
		}
		if len(requiredToolCalls) == 0 {
			break
		}
	}

	// The selected slice was built in reverse order; restore original ordering.
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

func selectLLMMessagesByCount(messages []llm.Message, maxMessages int) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	if maxMessages <= 0 {
		return messages
	}
	requiredToolCalls := map[string]struct{}{}
	selected := make([]llm.Message, 0, len(messages))
	includedAny := false

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		mustInclude := false
		if msg.Role == "tool" && msg.ToolCallID == "" {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			paired := make([]llm.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if call.ID == "" {
					continue
				}
				if _, ok := requiredToolCalls[call.ID]; ok {
					paired = append(paired, call)
					delete(requiredToolCalls, call.ID)
				}
			}
			if len(paired) == 0 && msg.Content == "" {
				continue
			}
			if len(paired) == 0 {
				msg.ToolCalls = nil
			} else {
				msg.ToolCalls = paired
				mustInclude = true
			}
		}
		if !includedAny && msg.Role != "tool" {
			mustInclude = true
		}

		if mustInclude || len(selected) < maxMessages {
			selected = append(selected, msg)
			includedAny = true
			if msg.Role == "tool" && msg.ToolCallID != "" {
				requiredToolCalls[msg.ToolCallID] = struct{}{}
			}
			continue
		}
		if len(requiredToolCalls) == 0 {
			break
		}
	}

	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}
