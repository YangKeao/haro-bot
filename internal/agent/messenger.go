package agent

import "context"

// SessionMessenger delivers out-of-band messages for a session (e.g., Telegram notifications).
// Implementations should be non-blocking where possible and may ignore unknown session IDs.
type SessionMessenger interface {
	SendSessionMessage(ctx context.Context, sessionID int64, message string) error
}
