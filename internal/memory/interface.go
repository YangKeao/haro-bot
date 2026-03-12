package memory

import "context"

// StoreAPI defines the persistence contract for session memory, summaries, and long-term memories.
// It is implemented by the memory store and is used by higher-level components (agent/tools)
// to avoid depending on concrete storage details.
type StoreAPI interface {
	// GetOrCreateUserByExternalID returns the internal user ID for an external IM user.
	// The provider identifies the IM platform (for example "telegram").
	GetOrCreateUserByExternalID(ctx context.Context, provider, externalID string) (int64, error)

	// GetOrCreateSession returns the active session ID for a user/channel pair.
	// If no session exists, a new one is created.
	GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error)

	// AddMessage appends a message to a session. Metadata captures tool calls/outputs and status.
	AddMessage(ctx context.Context, sessionID int64, role, content string, metadata *MessageMetadata) error

	// AddMessageAndGetID appends a message to a session and returns the inserted message ID.
	AddMessageAndGetID(ctx context.Context, sessionID int64, role, content string, metadata *MessageMetadata) (int64, error)

	// AppendSummary stores a summary snapshot for a session. If EntryID is 0,
	// it summarizes the latest message in the session.
	AppendSummary(ctx context.Context, sessionID int64, summary Summary) (int64, error)

	// LoadLatestSummary returns the most recent summary for a session, or nil if none exists.
	LoadLatestSummary(ctx context.Context, sessionID int64) (*Summary, error)

	// LoadViewMessages returns messages after the latest summary (if any) and the summary itself.
	// This is the canonical "current view" for LLM context. If limit <= 0, all messages
	// after the summary are returned. Invalid tool call/output pairs may be soft-deleted.
	LoadViewMessages(ctx context.Context, sessionID int64, limit int) ([]Message, *Summary, error)

	// SearchMessages searches session messages by content substring.
	// Results are ordered by most recent first. If limit <= 0, a default limit is used.
	SearchMessages(ctx context.Context, sessionID int64, query string, limit int, includeTool bool) ([]Message, error)
}

// Ensure the store implementation satisfies StoreAPI.
var _ StoreAPI = (*store)(nil)
