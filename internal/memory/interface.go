package memory

import "context"

// StoreAPI defines the persistence contract for session memory, anchors, and long-term memories.
// It is implemented by the memory store and is used by higher-level components (agent/tools)
// to avoid depending on concrete storage details.
type StoreAPI interface {
	// GetOrCreateUserByTelegramID returns the internal user ID for a Telegram user.
	// If the user does not exist, it is created.
	GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (int64, error)

	// GetOrCreateSession returns the active session ID for a user/channel pair.
	// If no session exists, a new one is created.
	GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error)

	// AddMessage appends a message to a session. Metadata captures tool calls/outputs and status.
	AddMessage(ctx context.Context, sessionID int64, role, content string, metadata *MessageMetadata) error

	// AppendAnchor stores a summary snapshot (anchor) for a session. If EntryID is 0,
	// it anchors the latest message in the session.
	AppendAnchor(ctx context.Context, sessionID int64, anchor Anchor) (int64, error)

	// LoadLatestAnchor returns the most recent anchor for a session, or nil if none exists.
	LoadLatestAnchor(ctx context.Context, sessionID int64) (*Anchor, error)

	// LoadViewMessages returns messages after the latest anchor (if any) and the anchor itself.
	// This is the canonical "current view" for LLM context. If limit <= 0, all messages
	// after the anchor are returned. Invalid tool call/output pairs may be soft-deleted.
	LoadViewMessages(ctx context.Context, sessionID int64, limit int) ([]Message, *Anchor, error)

	// LoadLongMemories returns the user's long-term memories ordered by importance.
	// If limit <= 0, a default limit is used.
	LoadLongMemories(ctx context.Context, userID int64, limit int) ([]Memory, error)
}

// Ensure the store implementation satisfies StoreAPI.
var _ StoreAPI = (*store)(nil)
