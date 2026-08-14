package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/memory"
)

type contextSnapshot struct {
	stored    []StoredMessage
	transient TransientContext
}

func loadContextSnapshot(ctx context.Context, store memory.StoreAPI, sessionID int64, systemPrompt string) (*contextSnapshot, error) {
	recent, summary, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	stored, err := toStoredMessages(recent)
	if err != nil {
		return nil, err
	}
	return &contextSnapshot{
		stored:    stored,
		transient: buildTransientContext(systemPrompt, summary, recent),
	}, nil
}

func (s *contextSnapshot) apply(run *RunState) {
	run.Stored = s.stored
	run.Transient = s.transient
}
