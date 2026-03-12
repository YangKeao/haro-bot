package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type ToolRunner interface {
	Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]StoredMessage, *skills.Skill, error)
}
