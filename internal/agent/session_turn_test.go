package agent

import (
	"testing"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func TestProjectObservationsRetiresOnlyOlderCurrentTurnMessages(t *testing.T) {
	messages := []StoredMessage{
		{EntryID: 1, Message: llm.Message{Content: "previous turn"}, Metadata: &memory.MessageMetadata{ObservationKey: "page"}},
		{EntryID: 10, Message: llm.Message{Content: "old snapshot"}, Metadata: &memory.MessageMetadata{ObservationKey: "page"}},
		{EntryID: 11, Message: llm.Message{Content: "evidence read"}},
		{EntryID: 12, Message: llm.Message{Content: "new snapshot"}, Metadata: &memory.MessageMetadata{ObservationKey: "page"}},
	}
	projected := projectObservations(messages, 9)
	if projected[0].Message.Content != "previous turn" {
		t.Fatal("previous turn observation was retired")
	}
	if projected[1].Message.Content == "old snapshot" {
		t.Fatal("older current-turn observation was not retired")
	}
	if projected[2].Message.Content != "evidence read" {
		t.Fatal("evidence read was changed")
	}
	if projected[3].Message.Content != "new snapshot" {
		t.Fatal("latest observation was changed")
	}
	if messages[1].Message.Content != "old snapshot" {
		t.Fatal("stored history was mutated")
	}
}
