package agent

import "strings"

func selectCompactionPrefixAndTail(view []StoredMessage) (prefix []StoredMessage, tail []StoredMessage) {
	if len(view) == 0 {
		return nil, nil
	}
	start := compactionTailStart(view)
	prefix = cloneStoredMessages(view[:start])
	tail = cloneStoredMessages(view[start:])
	return prefix, tail
}

func compactionTailStart(view []StoredMessage) int {
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
		return lastUser
	}

	triggerUser := lastIndexByRole(view, "user", lastAssistant-1)
	if triggerUser != -1 {
		return triggerUser
	}
	return lastAssistant
}

func lastIndexByRole(messages []StoredMessage, role string, end int) int {
	if len(messages) == 0 || end < 0 {
		return -1
	}
	if end >= len(messages) {
		end = len(messages) - 1
	}
	for i := end; i >= 0; i-- {
		if messages[i].Message.Role == role {
			return i
		}
	}
	return -1
}

func isSessionSummarySystemMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "Session summary")
}

func cloneStoredMessages(in []StoredMessage) []StoredMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]StoredMessage, len(in))
	copy(out, in)
	return out
}
