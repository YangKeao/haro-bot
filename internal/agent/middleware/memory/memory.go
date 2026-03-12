package memory

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentmemory "github.com/YangKeao/haro-bot/internal/memory"
)

type middleware struct {
	engine *agentmemory.Engine
}

func New(engine *agentmemory.Engine) agent.RunMiddleware {
	return &middleware{engine: engine}
}

func (m *middleware) Name() string {
	return "memory"
}

func (m *middleware) Priority() int {
	return 100
}

func (m *middleware) HandleRun(ctx context.Context, run *agent.RunState, next agent.RunHandler) (string, error) {
	if m.engine != nil && m.engine.Enabled() {
		items, err := m.engine.Retrieve(ctx, run.UserID, run.SessionID, run.Input, 0)
		if err != nil {
			return "", err
		}
		run.Memories = items
	}
	output, err := next(ctx, run)
	if err != nil {
		return "", err
	}
	if m.engine != nil && m.engine.Enabled() && run.ShouldIngest {
		m.engine.IngestAsync(run.UserID, run.SessionID)
	}
	return output, nil
}
