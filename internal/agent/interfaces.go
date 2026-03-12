package agent

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

// ConversationStore is the persistence API used by the agent and tool runner.
// It is aliased to memory.StoreAPI to keep method semantics centralized.
type ConversationStore = memory.StoreAPI

type PromptBuilder interface {
	System(ctx context.Context, memories []memory.MemoryItem, skillsList []skills.Metadata, format string) string
	Interrupt(ctx context.Context, memories []memory.MemoryItem, format string) string
	Skill(skill skills.Skill) string
}

type ToolRunner interface {
	Run(ctx context.Context, sessionID, userID int64, baseDir string, activeSkill *skills.Skill, calls []llm.ToolCall) ([]StoredMessage, *skills.Skill, error)
}
