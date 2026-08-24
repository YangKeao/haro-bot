package agent

import (
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

// StoredMessage is a persisted conversation entry loaded from or written to the store.
type StoredMessage struct {
	EntryID  int64
	Message  llm.Message
	Metadata *memory.MessageMetadata
}

// TransientMessage is runtime-only context that is never persisted directly.
type TransientMessage struct {
	Message llm.Message
}

// TransientContext keeps runtime-only messages separate from persisted history.
// Prefix is prepended before stored history; Suffix is appended after stored history.
type TransientContext struct {
	Prefix []TransientMessage
}

func newTransientMessage(msg llm.Message) TransientMessage {
	return TransientMessage{Message: msg}
}

func newStoredMessage(entryID int64, msg llm.Message, metadata ...*memory.MessageMetadata) (StoredMessage, error) {
	if entryID <= 0 {
		return StoredMessage{}, fmt.Errorf("stored message requires positive entry id, got %d", entryID)
	}
	stored := StoredMessage{
		EntryID: entryID,
		Message: msg,
	}
	if len(metadata) > 0 {
		stored.Metadata = metadata[0]
	}
	return stored, nil
}

func storedMessagesToLLM(messages []StoredMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Message)
	}
	return out
}

func transientMessagesToLLM(messages []TransientMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Message)
	}
	return out
}

func composeLLMMessages(stored []StoredMessage, transient TransientContext) []llm.Message {
	out := make([]llm.Message, 0, len(transient.Prefix)+len(stored))
	out = append(out, transientMessagesToLLM(transient.Prefix)...)
	out = append(out, storedMessagesToLLM(stored)...)
	return out
}

func toStoredMessages(msgs []memory.Message) ([]StoredMessage, error) {
	out := make([]StoredMessage, 0, len(msgs))
	for _, msg := range msgs {
		stored, err := newStoredMessage(msg.ID, toLLMMessage(msg), msg.Metadata)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func buildTransientContext(systemPrompt string, summary *memory.Summary, recent []memory.Message) TransientContext {
	prefix := []TransientMessage{newTransientMessage(llm.Message{Role: "system", Content: systemPrompt})}
	if summaryMsg := formatSummaryMessage(summary); summaryMsg != "" {
		prefix = append(prefix, newTransientMessage(llm.Message{Role: "system", Content: summaryMsg}))
	}
	if hint := summaryHint(recent); hint != "" {
		prefix = append(prefix, newTransientMessage(llm.Message{Role: "system", Content: hint}))
	}
	return TransientContext{Prefix: prefix}
}

func refreshTransientContext(base TransientContext, summary *memory.Summary, recent []memory.Message) TransientContext {
	prefix := make([]TransientMessage, 0, len(base.Prefix)+2)
	for _, msg := range base.Prefix {
		content := strings.TrimSpace(msg.Message.Content)
		if isSessionSummarySystemMessage(content) || isSessionHintSystemMessage(content) {
			continue
		}
		prefix = append(prefix, msg)
	}
	if summaryMsg := formatSummaryMessage(summary); summaryMsg != "" {
		prefix = append(prefix, newTransientMessage(llm.Message{Role: "system", Content: summaryMsg}))
	}
	if hint := summaryHint(recent); hint != "" {
		prefix = append(prefix, newTransientMessage(llm.Message{Role: "system", Content: hint}))
	}
	return TransientContext{
		Prefix: prefix,
	}
}

func isSessionSummarySystemMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), internalCheckpointPrefix)
}

func isSessionHintSystemMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "Host notice:")
}
