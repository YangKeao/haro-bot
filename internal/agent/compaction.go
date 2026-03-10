package agent

import (
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func selectCompactionPrefixAndTail(view []memory.Message) (prefix []memory.Message, tail []memory.Message) {
	if len(view) == 0 {
		return nil, nil
	}
	start := compactionTailStart(view)
	prefix = cloneMemoryMessages(view[:start])
	tail = cloneMemoryMessages(view[start:])
	return prefix, tail
}

func compactionTailStart(view []memory.Message) int {
	if len(view) == 0 {
		return 0
	}

	lastAssistant := lastIndexByRole(view, "assistant", len(view)-1)
	lastUser := lastIndexByRole(view, "user", len(view)-1)

	if lastAssistant == -1 {
		if lastUser == -1 {
			return len(view)
		}
		return lastUser
	}

	if lastUser > lastAssistant {
		// Pending user input after the latest assistant should not be compacted.
		return lastUser
	}

	triggerUser := lastIndexByRole(view, "user", lastAssistant-1)
	if triggerUser != -1 {
		return triggerUser
	}
	return lastAssistant
}

func lastIndexByRole(messages []memory.Message, role string, end int) int {
	if len(messages) == 0 || end < 0 {
		return -1
	}
	if end >= len(messages) {
		end = len(messages) - 1
	}
	for i := end; i >= 0; i-- {
		if messages[i].Role == role {
			return i
		}
	}
	return -1
}

func splitSystemMessages(messages []llm.Message) (base []llm.Message, latestSummary string) {
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if isSessionSummarySystemMessage(msg.Content) {
			latestSummary = msg.Content
			continue
		}
		base = append(base, msg)
	}
	return base, latestSummary
}

func isSessionSummarySystemMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "Session summary")
}

func nonSystemMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// appendTransientTail keeps runtime-only conversation messages that are not in store yet.
//
// Why this exists:
//   - compactAndReload rebuilds from persisted view (`LoadViewMessages`), which only contains
//     stored conversation messages after the latest summary.
//   - Some flows intentionally send non-persisted messages to the LLM (for example
//     `Interrupt(..., storeInSession=false)` adds an in-memory user message only).
//
// `persistedCount` must be the number of persisted non-system messages currently in view
// (usually `len(view)` from `LoadViewMessages` before compaction). Any extra messages in
// `current` are treated as transient and appended after `storedTail`.
func appendTransientTail(storedTail []llm.Message, current []llm.Message, persistedCount int) []llm.Message {
	conversation := nonSystemMessages(current)
	if len(conversation) <= persistedCount {
		return storedTail
	}
	transient := conversation[persistedCount:]
	out := make([]llm.Message, 0, len(storedTail)+len(transient))
	out = append(out, storedTail...)
	out = append(out, transient...)
	return out
}

func cloneMemoryMessages(in []memory.Message) []memory.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]memory.Message, len(in))
	copy(out, in)
	return out
}

func fitConversationToBudget(systemMsgs, conversation []llm.Message, estimator *llm.TokenEstimator, budget int) ([]llm.Message, bool) {
	if len(conversation) == 0 || estimator == nil || budget <= 0 {
		return conversation, false
	}
	baseTokens := estimator.CountMessages(systemMsgs)
	available := budget - baseTokens
	if available <= 0 {
		return nil, len(conversation) > 0
	}
	trimmed := selectLLMMessagesByTokens(conversation, estimator, available)
	return trimmed, len(trimmed) < len(conversation)
}
