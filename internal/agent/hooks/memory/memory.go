package memory

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentmemory "github.com/YangKeao/haro-bot/internal/memory"
)

type hook struct {
	engine *agentmemory.Engine
}

func New(engine *agentmemory.Engine) agent.RunHook {
	return &hook{engine: engine}
}

func (h *hook) Name() string {
	return "memory"
}

func (h *hook) Priority() int {
	return 100
}

func (h *hook) BeforePrompt(ctx context.Context, run *agent.RunState) error {
	if h == nil || h.engine == nil || !h.engine.Enabled() || run == nil {
		return nil
	}
	items, err := h.engine.Retrieve(ctx, run.UserID, run.SessionID, run.Input, 0)
	if err != nil {
		return err
	}
	run.Memories = items
	return nil
}

func (h *hook) AfterRun(_ context.Context, run *agent.RunState) error {
	if h == nil || h.engine == nil || !h.engine.Enabled() || run == nil || !run.ShouldIngest {
		return nil
	}
	h.engine.IngestAsync(run.UserID, run.SessionID)
	return nil
}
