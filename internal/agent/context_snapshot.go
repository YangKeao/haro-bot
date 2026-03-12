package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/memory"
)

type contextSnapshot struct {
	summary   *memory.Summary
	stored    []StoredMessage
	transient TransientContext
}

func loadContextSnapshot(ctx context.Context, store memory.StoreAPI, sessionID int64, systemPrompt, pendingUserInput string) (*contextSnapshot, error) {
	recent, summary, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	stored, err := toStoredMessages(recent)
	if err != nil {
		return nil, err
	}
	return &contextSnapshot{
		summary:   summary,
		stored:    stored,
		transient: buildTransientContext(systemPrompt, summary, recent, pendingUserInput),
	}, nil
}

func (s *contextSnapshot) apply(run *RunState) {
	if s == nil || run == nil {
		return
	}
	run.Summary = s.summary
	run.Stored = s.stored
	run.Transient = s.transient
}
