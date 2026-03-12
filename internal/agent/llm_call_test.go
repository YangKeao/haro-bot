package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
)

func TestCompactCutoffEntryIDUsesLastStoredMessage(t *testing.T) {
	user, err := newStoredMessage(101, llm.Message{Role: "user", Content: "u1"})
	if err != nil {
		t.Fatalf("create user message: %v", err)
	}
	assistant, err := newStoredMessage(202, llm.Message{Role: "assistant", Content: "a1"})
	if err != nil {
		t.Fatalf("create assistant message: %v", err)
	}

	cutoff, err := compactCutoffEntryID([]StoredMessage{user, assistant})
	if err != nil {
		t.Fatalf("compactCutoffEntryID returned error: %v", err)
	}
	if cutoff != 202 {
		t.Fatalf("cutoff = %d, want %d", cutoff, 202)
	}
}

func TestCompactCutoffEntryIDFailsWithoutStoredMessages(t *testing.T) {
	_, err := compactCutoffEntryID(nil)
	if err == nil {
		t.Fatal("expected error when no stored message exists")
	}
}

func TestCompactCutoffEntryIDFailsOnInvalidStoredEntryID(t *testing.T) {
	_, err := compactCutoffEntryID([]StoredMessage{
		{
			EntryID: 0,
			Message: llm.Message{Role: "user", Content: "bad"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid stored entry id")
	}
}
