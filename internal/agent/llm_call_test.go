package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestCompactCutoffEntryIDUsesLastPersistedMessage(t *testing.T) {
	system := newTransientContextMessage(llm.Message{Role: "system", Content: "sys"})
	user, err := newPersistedContextMessage(101, llm.Message{Role: "user", Content: "u1"})
	if err != nil {
		t.Fatalf("create user context message: %v", err)
	}
	assistant, err := newPersistedContextMessage(202, llm.Message{Role: "assistant", Content: "a1"})
	if err != nil {
		t.Fatalf("create assistant context message: %v", err)
	}
	ephemeral := newTransientContextMessage(llm.Message{Role: "system", Content: "hint"})

	cutoff, err := compactCutoffEntryID([]ContextMessage{system, user, assistant, ephemeral})
	if err != nil {
		t.Fatalf("compactCutoffEntryID returned error: %v", err)
	}
	if cutoff != 202 {
		t.Fatalf("cutoff = %d, want %d", cutoff, 202)
	}
}

func TestCompactCutoffEntryIDFailsWithoutPersistedMessages(t *testing.T) {
	_, err := compactCutoffEntryID([]ContextMessage{
		newTransientContextMessage(llm.Message{Role: "system", Content: "sys"}),
		newTransientContextMessage(llm.Message{Role: "user", Content: "temp"}),
	})
	if err == nil {
		t.Fatal("expected error when no persisted message exists")
	}
}

func TestCompactCutoffEntryIDFailsOnInvalidPersistedEntryID(t *testing.T) {
	_, err := compactCutoffEntryID([]ContextMessage{
		testContextMessage{
			msg:       llm.Message{Role: "user", Content: "bad"},
			entryID:   0,
			persisted: true,
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid persisted entry id")
	}
}

type testContextMessage struct {
	msg       llm.Message
	entryID   int64
	persisted bool
}

func (m testContextMessage) ToLLM() llm.Message {
	return m.msg
}

func (m testContextMessage) EntryID() (int64, bool) {
	return m.entryID, m.persisted
}
