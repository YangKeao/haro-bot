package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/YangKeao/haro-bot/internal/memory"
)

type SessionAnchorTool struct {
	store memory.StoreAPI
}

type sessionAnchorArgs struct {
	Phase          string         `json:"phase"`
	Summary        string         `json:"summary"`
	State          map[string]any `json:"state"`
	SourceEntryIDs []int64        `json:"source_entry_ids"`
	EntryID        int64          `json:"entry_id"`
}

func NewSessionAnchorTool(store memory.StoreAPI) *SessionAnchorTool {
	return &SessionAnchorTool{store: store}
}

func (t *SessionAnchorTool) Name() string { return "session_anchor" }

func (t *SessionAnchorTool) Description() string {
	return "Create a session anchor (handoff) to summarize state and reset the view window."
}

func (t *SessionAnchorTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"phase": map[string]any{
				"type":        "string",
				"description": "Optional phase name for the new anchor.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Concise summary of the current session state.",
			},
			"state": map[string]any{
				"type":        "object",
				"description": "Structured state payload to persist.",
			},
			"source_entry_ids": map[string]any{
				"type":        "array",
				"description": "Optional message IDs used to build this anchor.",
				"items": map[string]any{
					"type": "integer",
				},
			},
			"entry_id": map[string]any{
				"type":        "integer",
				"description": "Optional message ID to anchor from (defaults to latest message).",
			},
		},
	}
}

func (t *SessionAnchorTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.store == nil {
		return "", errors.New("anchor store not configured")
	}
	if tc.SessionID == 0 {
		return "", errors.New("session_id required")
	}
	var a sessionAnchorArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.Summary == "" && len(a.State) == 0 && a.Phase == "" {
		return "", errors.New("summary, state, or phase required")
	}
	anchor := memory.Anchor{
		SessionID:      tc.SessionID,
		EntryID:        a.EntryID,
		Phase:          a.Phase,
		Summary:        a.Summary,
		State:          a.State,
		SourceEntryIDs: a.SourceEntryIDs,
	}
	id, err := t.store.AppendAnchor(ctx, tc.SessionID, anchor)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("anchor_id=%d", id), nil
}
