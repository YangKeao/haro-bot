package im

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
)

// Runtime is the IM integration boundary used by the application.
// Telegram is one implementation; future IM providers can implement the same contract.
type Runtime interface {
	Start(ctx context.Context)
	SessionMessenger() agent.SessionMessenger
}
