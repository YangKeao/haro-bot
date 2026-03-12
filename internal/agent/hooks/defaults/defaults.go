package defaults

import (
	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/agent/hooks/compact"
	"github.com/YangKeao/haro-bot/internal/agent/hooks/memory"
	"github.com/YangKeao/haro-bot/internal/agent/hooks/status"
	"github.com/YangKeao/haro-bot/internal/llm"
	agentmemory "github.com/YangKeao/haro-bot/internal/memory"
)

func New(store agent.ConversationStore, memoryEngine *agentmemory.Engine, chatModel llm.ChatModel, contextConfig llm.ContextConfig, statusWriter agent.SessionStatusWriter) agent.HookSet {
	hooks := status.New(statusWriter)
	hooks.RunHooks = append(hooks.RunHooks,
		memory.New(memoryEngine),
	)
	hooks.TurnHooks = append(hooks.TurnHooks,
		compact.New(store, chatModel, contextConfig),
	)
	return hooks
}
