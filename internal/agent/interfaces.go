package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
)

type ToolRunner interface {
	Run(ctx context.Context, sessionID int64, baseDir string, calls []llm.ToolCall) ([]StoredMessage, error)
}
