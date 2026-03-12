package defaults

import (
	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/agent/listener/status"
	compactmw "github.com/YangKeao/haro-bot/internal/agent/middleware/compact"
	memorymw "github.com/YangKeao/haro-bot/internal/agent/middleware/memory"
	promptmw "github.com/YangKeao/haro-bot/internal/agent/middleware/prompt"
	statusmw "github.com/YangKeao/haro-bot/internal/agent/middleware/status"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	agentmemory "github.com/YangKeao/haro-bot/internal/memory"
)

func New(guidelinesMgr *guidelines.Manager, store agentmemory.StoreAPI, memoryEngine *agentmemory.Engine, chatModel llm.ChatModel, contextConfig llm.ContextConfig, statusWriter agent.SessionStatusWriter) agent.MiddlewareSet {
	middleware := statusmw.New(statusWriter)
	middleware.RunMiddleware = append(middleware.RunMiddleware,
		memorymw.New(memoryEngine),
		promptmw.New(guidelinesMgr),
	)
	middleware.LLMMiddleware = append(middleware.LLMMiddleware,
		compactmw.New(store, chatModel, contextConfig),
	)
	if statusWriter != nil {
		middleware.ToolCallListeners = append(middleware.ToolCallListeners,
			status.New(statusWriter),
		)
	}
	return middleware
}
