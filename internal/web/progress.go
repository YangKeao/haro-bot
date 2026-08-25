package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/llm"
)

type streamEvent struct {
	Name string
	Data any
}

type webProgress struct {
	events chan streamEvent
	mu     sync.Mutex

	content       string
	reasoning     string
	sequence      int64
	trace         []llm.TraceStep
	traceIndexes  map[string]int
	openReasoning map[int]string
	seenTools     map[string]bool
}

func newWebProgress() *webProgress {
	return &webProgress{
		events:        make(chan streamEvent, 64),
		traceIndexes:  make(map[string]int),
		openReasoning: make(map[int]string),
		seenTools:     make(map[string]bool),
	}
}

func (p *webProgress) Name() string  { return "web_sse" }
func (p *webProgress) Priority() int { return 300 }

func (p *webProgress) emit(ctx context.Context, name string, data any) {
	select {
	case p.events <- streamEvent{Name: name, Data: data}:
	case <-ctx.Done():
	}
}

func (p *webProgress) emitTrace(ctx context.Context, name string, turn int, providerSequence int64, data map[string]any) {
	p.mu.Lock()
	p.sequence++
	data["sequence"] = p.sequence
	p.mu.Unlock()
	data["turn_index"] = turn
	if providerSequence > 0 {
		data["provider_sequence"] = providerSequence
	}
	p.emit(ctx, name, data)
}

func (p *webProgress) OnLLMDelta(ctx context.Context, turn *agent.TurnState, event llm.StreamEvent) {
	turnIndex := 0
	if turn != nil {
		turnIndex = turn.Index
	}
	p.mu.Lock()
	p.content += event.Delta
	p.reasoning += event.ReasoningDelta
	p.mu.Unlock()
	if event.Delta != "" {
		p.emit(ctx, "assistant.delta", map[string]any{"delta": event.Delta, "turn_index": turnIndex})
	}
	if event.Trace != nil {
		p.onTraceEvent(ctx, turnIndex, *event.Trace)
		return
	}
	if event.ReasoningDelta != "" {
		p.onLegacyReasoningDelta(ctx, turnIndex, event.ReasoningDelta)
	}
}

func (p *webProgress) onLegacyReasoningDelta(ctx context.Context, turn int, delta string) {
	id := fmt.Sprintf("turn-%d-reasoning", turn)
	step := llm.TraceStep{ID: id, Kind: "reasoning", Status: "running"}
	p.mu.Lock()
	_, exists := p.traceIndexes[id]
	p.mergeStepLocked(step, delta, "")
	p.openReasoning[turn] = id
	p.mu.Unlock()
	if !exists {
		p.emitTrace(ctx, "reasoning.started", turn, 0, traceStepData(step))
	}
	p.emitTrace(ctx, "reasoning.delta", turn, 0, map[string]any{"id": id, "kind": "reasoning", "status": "running", "delta": delta})
}

func (p *webProgress) onTraceEvent(ctx context.Context, turn int, event llm.TraceEvent) {
	step := event.Step
	if step.ID == "" {
		step.ID = fmt.Sprintf("turn-%d-%s-%d", turn, step.Kind, step.Order)
	}
	switch step.Kind {
	case "reasoning":
		p.mu.Lock()
		_, exists := p.traceIndexes[step.ID]
		contentDelta := ""
		if event.Phase == "delta" {
			contentDelta = event.Delta
		}
		p.mergeStepLocked(step, contentDelta, "")
		if event.Phase == "delta" || event.Phase == "started" {
			p.openReasoning[turn] = step.ID
		} else if event.Phase == "completed" {
			delete(p.openReasoning, turn)
		}
		p.mu.Unlock()
		if !exists {
			p.emitTrace(ctx, "reasoning.started", turn, event.Sequence, traceStepData(step))
		}
		if event.Phase == "delta" {
			p.emitTrace(ctx, "reasoning.delta", turn, event.Sequence, map[string]any{"id": step.ID, "kind": "reasoning", "status": "running", "delta": event.Delta})
		} else if event.Phase == "completed" {
			p.emitTrace(ctx, "reasoning.completed", turn, event.Sequence, traceStepData(step))
		}
	case "tool":
		p.mu.Lock()
		seen := p.seenTools[step.ID]
		argumentsDelta := ""
		if event.Phase == "arguments.delta" {
			argumentsDelta = event.Delta
		}
		p.mergeStepLocked(step, "", argumentsDelta)
		p.seenTools[step.ID] = true
		p.mu.Unlock()
		data := traceStepData(step)
		if argumentsDelta != "" {
			data["arguments_delta"] = argumentsDelta
		}
		name := "tool.updated"
		if event.Phase == "started" && !seen {
			name = "tool.started"
		} else if event.Phase == "completed" {
			name = "tool.completed"
		}
		p.emitTrace(ctx, name, turn, event.Sequence, data)
	}
}

func (p *webProgress) OnToolCalls(ctx context.Context, turn *agent.TurnState, msg llm.Message) error {
	turnIndex := 0
	if turn != nil {
		turnIndex = turn.Index
	}
	p.finalizeReasoning(ctx, turnIndex)
	for _, call := range msg.ToolCalls {
		var args any = json.RawMessage(call.Function.Arguments)
		if !json.Valid([]byte(call.Function.Arguments)) {
			args = call.Function.Arguments
		}
		step := llm.TraceStep{ID: call.ID, Kind: "tool", ToolKind: "function", Name: call.Function.Name, Status: "running", Arguments: call.Function.Arguments}
		p.mu.Lock()
		seen := p.seenTools[call.ID]
		p.mergeStepLocked(step, "", "")
		p.seenTools[call.ID] = true
		p.mu.Unlock()
		name := "tool.started"
		if seen {
			name = "tool.updated"
		}
		p.emitTrace(ctx, name, turnIndex, 0, map[string]any{
			"id": call.ID, "kind": "tool", "tool_kind": "function", "name": call.Function.Name,
			"status": "running", "arguments": args,
		})
	}
	return nil
}

func (p *webProgress) OnToolResults(ctx context.Context, turn *agent.TurnState, messages []agent.StoredMessage) error {
	turnIndex := 0
	if turn != nil {
		turnIndex = turn.Index
	}
	for _, message := range messages {
		content := message.Message.Content
		status := "completed"
		var detail any
		if message.Metadata != nil {
			if message.Metadata.DisplayContent != "" {
				content = message.Metadata.DisplayContent
			}
			if message.Metadata.Status == "error" {
				status = "error"
			}
			detail = message.Metadata.StructuredContent
		}
		fullContent := content
		truncated := false
		if len(content) > 64<<10 {
			content = content[:64<<10]
			truncated = true
		}
		step := llm.TraceStep{ID: message.Message.ToolCallID, Kind: "tool", ToolKind: "function", Status: status, Result: fullContent, Detail: detail, Truncated: truncated}
		p.mu.Lock()
		p.mergeStepLocked(step, "", "")
		p.seenTools[step.ID] = true
		p.mu.Unlock()
		p.emitTrace(ctx, "tool.completed", turnIndex, 0, map[string]any{
			"id": step.ID, "kind": "tool", "tool_kind": "function", "status": status,
			"content": content, "detail": detail, "truncated": truncated,
		})
	}
	return nil
}

func (p *webProgress) OnFinalOutput(ctx context.Context, turn *agent.TurnState, output string) error {
	turnIndex := 0
	if turn != nil {
		turnIndex = turn.Index
	}
	p.finalizeReasoning(ctx, turnIndex)
	p.emit(ctx, "assistant.completed", map[string]any{"content": output, "turn_index": turnIndex})
	return nil
}

func (p *webProgress) finalizeReasoning(ctx context.Context, turn int) {
	p.mu.Lock()
	id := p.openReasoning[turn]
	if id == "" {
		p.mu.Unlock()
		return
	}
	delete(p.openReasoning, turn)
	step := p.trace[p.traceIndexes[id]]
	step.Status = "completed"
	p.mergeStepLocked(step, "", "")
	p.mu.Unlock()
	p.emitTrace(ctx, "reasoning.completed", turn, 0, traceStepData(step))
}

func (p *webProgress) mergeStepLocked(update llm.TraceStep, contentDelta, argumentsDelta string) {
	index, ok := p.traceIndexes[update.ID]
	if !ok {
		index = len(p.trace)
		p.traceIndexes[update.ID] = index
		p.trace = append(p.trace, update)
	}
	step := &p.trace[index]
	if update.Kind != "" {
		step.Kind = update.Kind
	}
	if update.ToolKind != "" {
		step.ToolKind = update.ToolKind
	}
	if update.Name != "" {
		step.Name = update.Name
	}
	if update.Status != "" {
		step.Status = update.Status
	}
	if update.Content != "" {
		step.Content = update.Content
	}
	if contentDelta != "" {
		step.Content += contentDelta
	}
	if update.Arguments != "" {
		step.Arguments = update.Arguments
	}
	if argumentsDelta != "" {
		step.Arguments += argumentsDelta
	}
	if update.Result != "" {
		step.Result = update.Result
	}
	if update.Detail != nil {
		step.Detail = update.Detail
	}
	if update.Order != 0 {
		step.Order = update.Order
	}
	step.Truncated = step.Truncated || update.Truncated
}

func traceStepData(step llm.TraceStep) map[string]any {
	return map[string]any{
		"id": step.ID, "kind": step.Kind, "tool_kind": step.ToolKind, "name": step.Name,
		"status": step.Status, "content": step.Content, "arguments": step.Arguments,
		"result": step.Result, "detail": step.Detail, "order": step.Order, "truncated": step.Truncated,
	}
}

func (p *webProgress) partial() (string, string, []llm.TraceStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	trace := append([]llm.TraceStep(nil), p.trace...)
	return p.content, p.reasoning, trace
}
