package im

import (
	"context"
)

// Runtime is the IM integration boundary used by the application.
// Telegram is one implementation; future IM providers can implement the same contract.
type Runtime interface {
	Start(ctx context.Context)
}
