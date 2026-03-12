package status

import (
	"context"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
)

type listener struct {
	writer agent.SessionStatusWriter
}

func New(writer agent.SessionStatusWriter) agent.ToolCallListener {
	return &listener{writer: writer}
}

func (l *listener) Name() string {
	return "status"
}

func (l *listener) Priority() int {
	return 10
}

func (l *listener) OnToolCalls(_ context.Context, turn *agent.TurnState, msg llm.Message) error {
	if len(msg.ToolCalls) == 0 {
		return nil
	}
	toolName := msg.ToolCalls[len(msg.ToolCalls)-1].Function.Name
	l.writer.SetToolRunning(turn.Run.SessionID, toolName)
	return nil
}
