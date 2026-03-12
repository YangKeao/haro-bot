package agent

import (
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func (t *TurnState) LLMMessages() []llm.Message {
	return composeLLMMessages(t.Stored, t.Transient)
}

func (t *TurnState) ReloadContext(recent []memory.Message, summary *memory.Summary) error {
	reloadedStored, err := toStoredMessages(recent)
	if err != nil {
		return err
	}
	t.Stored = reloadedStored
	t.Transient = refreshTransientContext(t.Transient, summary, recent)
	t.Run.Stored = reloadedStored
	t.Run.Transient = t.Transient
	t.Run.Summary = summary
	return nil
}
