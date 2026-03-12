package status

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
)

type hook struct {
	writer agent.SessionStatusWriter
}

func New(writer agent.SessionStatusWriter) agent.HookSet {
	if writer == nil {
		return agent.HookSet{}
	}
	h := &hook{writer: writer}
	return agent.HookSet{
		RunHooks:  []agent.RunHook{h},
		TurnHooks: []agent.TurnHook{h},
	}
}

func (h *hook) Name() string {
	return "status"
}

func (h *hook) Priority() int {
	return 10
}

func (h *hook) BeforePrompt(_ context.Context, run *agent.RunState) error {
	if h == nil || h.writer == nil || run == nil {
		return nil
	}
	h.writer.SetLLMModel(run.SessionID, run.Model)
	h.writer.SetState(run.SessionID, agent.StateWaitingForLLM)
	return nil
}

func (h *hook) OnRunFinish(_ context.Context, run *agent.RunState, _ error) error {
	if h == nil || h.writer == nil || run == nil {
		return nil
	}
	h.writer.SetState(run.SessionID, agent.StateIdle)
	return nil
}

func (h *hook) BeforeLLM(_ context.Context, turn *agent.TurnState, call *agent.LLMCall) error {
	if h == nil || h.writer == nil || turn == nil || turn.Run == nil {
		return nil
	}
	h.writer.SetLLMModel(turn.Run.SessionID, turn.Model)
	if call != nil && call.Model != "" {
		h.writer.SetLLMModel(turn.Run.SessionID, call.Model)
	}
	h.writer.SetState(turn.Run.SessionID, agent.StateWaitingForLLM)
	return nil
}

func (h *hook) OnToolCalls(_ context.Context, turn *agent.TurnState, msg llm.Message) error {
	if h == nil || h.writer == nil || turn == nil || turn.Run == nil || len(msg.ToolCalls) == 0 {
		return nil
	}
	toolName := msg.ToolCalls[len(msg.ToolCalls)-1].Function.Name
	h.writer.SetToolRunning(turn.Run.SessionID, toolName)
	return nil
}
