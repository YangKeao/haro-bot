package status

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
)

type middleware struct {
	writer agent.SessionStatusWriter
}

func New(writer agent.SessionStatusWriter) agent.MiddlewareSet {
	if writer == nil {
		return agent.MiddlewareSet{}
	}
	m := &middleware{writer: writer}
	return agent.MiddlewareSet{
		RunMiddleware: []agent.RunMiddleware{m},
		LLMMiddleware: []agent.LLMMiddleware{m},
	}
}

func (m *middleware) Name() string {
	return "status"
}

func (m *middleware) Priority() int {
	return 10
}

func (m *middleware) HandleRun(ctx context.Context, run *agent.RunState, next agent.RunHandler) (string, error) {
	m.writer.SetLLMModel(run.SessionID, run.Model)
	m.writer.SetState(run.SessionID, agent.StateWaitingForLLM)
	defer m.writer.SetState(run.SessionID, agent.StateIdle)
	return next(ctx, run)
}

func (m *middleware) HandleLLM(ctx context.Context, turn *agent.TurnState, call *agent.LLMCall, next agent.LLMHandler) (llm.ChatResponse, error) {
	m.writer.SetLLMModel(turn.Run.SessionID, turn.Model)
	if call.Model != "" {
		m.writer.SetLLMModel(turn.Run.SessionID, call.Model)
	}
	m.writer.SetState(turn.Run.SessionID, agent.StateWaitingForLLM)
	return next(ctx, turn, call)
}
