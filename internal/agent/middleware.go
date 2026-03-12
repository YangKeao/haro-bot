package agent

import (
	"context"
	"sort"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type MiddlewareSet struct {
	RunMiddleware     []RunMiddleware
	TurnMiddleware    []TurnMiddleware
	LLMMiddleware     []LLMMiddleware
	LLMDeltaListeners []LLMDeltaListener
	ToolCallListeners []ToolCallListener
	OutputListeners   []OutputListener
}

type PromptMode string

const (
	PromptModeHandle PromptMode = "handle"
)

type RunState struct {
	SessionID int64
	UserID    int64
	Channel   string
	Model     string
	Input     string

	PromptMode   PromptMode
	PromptFormat string
	Prompt       string
	PendingInput string

	Stored    []StoredMessage
	Transient TransientContext
	Summary   *memory.Summary

	Memories        []memory.MemoryItem
	AvailableSkills []skills.Metadata
	ShouldIngest    bool
	Output          string
}

type TurnState struct {
	Run         *RunState
	Index       int
	Model       string
	Stored      []StoredMessage
	Transient   TransientContext
	Tools       []llm.Tool
	Estimator   *llm.TokenEstimator
	ActiveSkill *skills.Skill
	Output      string
	Done        bool
}

type LLMCall struct {
	Model   string
	Attempt int
	Tools   []llm.Tool
}

type RunHandler func(ctx context.Context, run *RunState) (string, error)

type RunMiddleware interface {
	Name() string
	HandleRun(ctx context.Context, run *RunState, next RunHandler) (string, error)
}

type TurnHandler func(ctx context.Context, turn *TurnState) error

type TurnMiddleware interface {
	Name() string
	HandleTurn(ctx context.Context, turn *TurnState, next TurnHandler) error
}

type LLMHandler func(ctx context.Context, turn *TurnState, call *LLMCall) (llm.ChatResponse, error)

type LLMMiddleware interface {
	Name() string
	HandleLLM(ctx context.Context, turn *TurnState, call *LLMCall, next LLMHandler) (llm.ChatResponse, error)
}

type LLMDeltaListener interface {
	Name() string
	OnLLMDelta(ctx context.Context, turn *TurnState, event llm.StreamEvent)
}

type ToolCallListener interface {
	Name() string
	OnToolCalls(ctx context.Context, turn *TurnState, msg llm.Message) error
}

type OutputListener interface {
	Name() string
	OnFinalOutput(ctx context.Context, turn *TurnState, output string) error
}

type prioritizedHook interface {
	Priority() int
}

func mergeMiddlewareSets(base, extra MiddlewareSet) MiddlewareSet {
	runMiddleware := append(append([]RunMiddleware(nil), base.RunMiddleware...), extra.RunMiddleware...)
	turnMiddleware := append(append([]TurnMiddleware(nil), base.TurnMiddleware...), extra.TurnMiddleware...)
	llmMiddleware := append(append([]LLMMiddleware(nil), base.LLMMiddleware...), extra.LLMMiddleware...)
	llmDeltaListeners := append(append([]LLMDeltaListener(nil), base.LLMDeltaListeners...), extra.LLMDeltaListeners...)
	toolCallListeners := append(append([]ToolCallListener(nil), base.ToolCallListeners...), extra.ToolCallListeners...)
	outputListeners := append(append([]OutputListener(nil), base.OutputListeners...), extra.OutputListeners...)

	sort.SliceStable(runMiddleware, func(i, j int) bool { return hookPriority(runMiddleware[i]) < hookPriority(runMiddleware[j]) })
	sort.SliceStable(turnMiddleware, func(i, j int) bool { return hookPriority(turnMiddleware[i]) < hookPriority(turnMiddleware[j]) })
	sort.SliceStable(llmMiddleware, func(i, j int) bool { return hookPriority(llmMiddleware[i]) < hookPriority(llmMiddleware[j]) })
	sort.SliceStable(llmDeltaListeners, func(i, j int) bool { return hookPriority(llmDeltaListeners[i]) < hookPriority(llmDeltaListeners[j]) })
	sort.SliceStable(toolCallListeners, func(i, j int) bool { return hookPriority(toolCallListeners[i]) < hookPriority(toolCallListeners[j]) })
	sort.SliceStable(outputListeners, func(i, j int) bool { return hookPriority(outputListeners[i]) < hookPriority(outputListeners[j]) })

	return MiddlewareSet{
		RunMiddleware:     runMiddleware,
		TurnMiddleware:    turnMiddleware,
		LLMMiddleware:     llmMiddleware,
		LLMDeltaListeners: llmDeltaListeners,
		ToolCallListeners: toolCallListeners,
		OutputListeners:   outputListeners,
	}
}

func hookPriority(h any) int {
	if prioritized, ok := h.(prioritizedHook); ok {
		return prioritized.Priority()
	}
	return 0
}

func executeRunMiddleware(ctx context.Context, middleware []RunMiddleware, run *RunState, final RunHandler) (string, error) {
	handler := final
	for i := len(middleware) - 1; i >= 0; i-- {
		current := middleware[i]
		next := handler
		handler = func(ctx context.Context, run *RunState) (string, error) {
			return current.HandleRun(ctx, run, next)
		}
	}
	return handler(ctx, run)
}

func executeTurnMiddleware(ctx context.Context, middleware []TurnMiddleware, turn *TurnState, final TurnHandler) error {
	handler := final
	for i := len(middleware) - 1; i >= 0; i-- {
		current := middleware[i]
		next := handler
		handler = func(ctx context.Context, turn *TurnState) error {
			return current.HandleTurn(ctx, turn, next)
		}
	}
	return handler(ctx, turn)
}

func executeLLMMiddleware(ctx context.Context, middleware []LLMMiddleware, turn *TurnState, call *LLMCall, final LLMHandler) (llm.ChatResponse, error) {
	handler := final
	for i := len(middleware) - 1; i >= 0; i-- {
		current := middleware[i]
		next := handler
		handler = func(ctx context.Context, turn *TurnState, call *LLMCall) (llm.ChatResponse, error) {
			return current.HandleLLM(ctx, turn, call, next)
		}
	}
	return handler(ctx, turn, call)
}

func executeLLMDeltaListeners(ctx context.Context, listeners []LLMDeltaListener, turn *TurnState, event llm.StreamEvent) {
	for _, listener := range listeners {
		listener.OnLLMDelta(ctx, turn, event)
	}
}

func executeToolCallListeners(ctx context.Context, listeners []ToolCallListener, turn *TurnState, msg llm.Message) error {
	for _, listener := range listeners {
		if err := listener.OnToolCalls(ctx, turn, msg); err != nil {
			return err
		}
	}
	return nil
}

func executeOutputListeners(ctx context.Context, listeners []OutputListener, turn *TurnState, output string) error {
	for _, listener := range listeners {
		if err := listener.OnFinalOutput(ctx, turn, output); err != nil {
			return err
		}
	}
	return nil
}
