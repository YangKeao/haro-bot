package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type ConversationStore interface {
	GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error)
	AddMessage(ctx context.Context, sessionID int64, role, content string, metadata *memory.MessageMetadata) error
	LoadViewMessages(ctx context.Context, sessionID int64, limit int) ([]memory.Message, *memory.Anchor, error)
	LoadRecentMessages(ctx context.Context, sessionID int64, limit int) ([]memory.Message, error)
	LoadLongMemories(ctx context.Context, userID int64, limit int) ([]memory.Memory, error)
}

type PromptBuilder interface {
	System(memories []memory.Memory, skillsList []skills.Metadata, format string) string
	Interrupt(memories []memory.Memory, format string) string
	Skill(skill skills.Skill) string
}

type ToolRunner interface {
	Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]llm.Message, *skills.Skill, error)
}
