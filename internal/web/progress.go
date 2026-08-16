package web

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
)

type streamEvent struct {
	Name string
	Data any
}

type webProgress struct {
	events    chan streamEvent
	mu        sync.Mutex
	content   string
	reasoning string
}

func newWebProgress() *webProgress   { return &webProgress{events: make(chan streamEvent, 64)} }
func (p *webProgress) Name() string  { return "web_sse" }
func (p *webProgress) Priority() int { return 300 }

func (p *webProgress) emit(ctx context.Context, name string, data any) {
	select {
	case p.events <- streamEvent{Name: name, Data: data}:
	case <-ctx.Done():
	}
}

func (p *webProgress) OnLLMDelta(ctx context.Context, _ *agent.TurnState, event llm.StreamEvent) {
	p.mu.Lock()
	p.content += event.Delta
	p.reasoning += event.ReasoningDelta
	p.mu.Unlock()
	if event.Delta != "" {
		p.emit(ctx, "assistant.delta", map[string]string{"delta": event.Delta})
	}
	if event.ReasoningDelta != "" {
		p.emit(ctx, "reasoning.delta", map[string]string{"delta": event.ReasoningDelta})
	}
}

func (p *webProgress) OnToolCalls(ctx context.Context, _ *agent.TurnState, msg llm.Message) error {
	for _, call := range msg.ToolCalls {
		var args any = json.RawMessage(call.Function.Arguments)
		if !json.Valid([]byte(call.Function.Arguments)) {
			args = call.Function.Arguments
		}
		p.emit(ctx, "tool.started", map[string]any{"id": call.ID, "name": call.Function.Name, "arguments": args})
	}
	return nil
}

func (p *webProgress) OnToolResults(ctx context.Context, _ *agent.TurnState, messages []agent.StoredMessage) error {
	for _, message := range messages {
		content := message.Message.Content
		truncated := false
		if len(content) > 64<<10 {
			content = content[:64<<10]
			truncated = true
		}
		p.emit(ctx, "tool.completed", map[string]any{"id": message.Message.ToolCallID, "content": content, "truncated": truncated})
	}
	return nil
}

func (p *webProgress) OnFinalOutput(ctx context.Context, _ *agent.TurnState, output string) error {
	p.emit(ctx, "assistant.completed", map[string]string{"content": output})
	return nil
}

func (p *webProgress) partial() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.content, p.reasoning
}
