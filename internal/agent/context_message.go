package agent

import (
	"fmt"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

// ContextMessage is an in-memory context item used by the agent loop.
// Persisted messages carry EntryID; transient messages do not.
type ContextMessage interface {
	ToLLM() llm.Message
	EntryID() (int64, bool)
}

type transientContextMessage struct {
	msg llm.Message
}

func newTransientContextMessage(msg llm.Message) ContextMessage {
	return transientContextMessage{msg: msg}
}

func (m transientContextMessage) ToLLM() llm.Message {
	return m.msg
}

func (m transientContextMessage) EntryID() (int64, bool) {
	return 0, false
}

type persistedContextMessage struct {
	msg     llm.Message
	entryID int64
}

func newPersistedContextMessage(entryID int64, msg llm.Message) (ContextMessage, error) {
	if entryID <= 0 {
		return nil, fmt.Errorf("persisted context message requires positive entry id, got %d", entryID)
	}
	return persistedContextMessage{
		msg:     msg,
		entryID: entryID,
	}, nil
}

func (m persistedContextMessage) ToLLM() llm.Message {
	return m.msg
}

func (m persistedContextMessage) EntryID() (int64, bool) {
	return m.entryID, true
}

func contextMessagesToLLM(messages []ContextMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.ToLLM())
	}
	return out
}

func toContextMessages(msgs []memory.Message) ([]ContextMessage, error) {
	out := make([]ContextMessage, 0, len(msgs))
	for _, msg := range msgs {
		ctxMsg, err := newPersistedContextMessage(msg.ID, toLLMMessage(msg))
		if err != nil {
			return nil, err
		}
		out = append(out, ctxMsg)
	}
	return out, nil
}
