package agent

import (
	"context"
	"sort"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
)

type HookSet struct {
	RunHooks  []RunHook
	TurnHooks []TurnHook
}

type RunHook interface {
	Name() string
}

type TurnHook interface {
	Name() string
}

type RunState struct {
	SessionID int64
	UserID    int64
	Channel   string
	Model     string
	Input     string

	Stored    []StoredMessage
	Transient TransientContext
	Summary   *memory.Summary

	Memories        []memory.MemoryItem
	AvailableSkills []skills.Metadata
	ShouldIngest    bool
	Output          string
}

type TurnState struct {
	Run       *RunState
	Index     int
	Model     string
	Stored    []StoredMessage
	Transient TransientContext
	Tools     []llm.Tool
	Estimator *llm.TokenEstimator
}

type LLMCall struct {
	Model   string
	Attempt int
	Tools   []llm.Tool
}

type RunPrepareHook interface {
	BeforePrompt(ctx context.Context, run *RunState) error
}

type RunFinishHook interface {
	AfterRun(ctx context.Context, run *RunState) error
}

type RunFinalizeHook interface {
	OnRunFinish(ctx context.Context, run *RunState, runErr error) error
}

type TurnBeforeLLMHook interface {
	BeforeLLM(ctx context.Context, turn *TurnState, call *LLMCall) error
}

type TurnLLMStartHook interface {
	OnLLMStart(ctx context.Context, turn *TurnState, info LLMStartInfo) error
}

type TurnLLMDeltaHook interface {
	OnLLMDelta(ctx context.Context, turn *TurnState, event llm.StreamEvent) error
}

type TurnLLMErrorHook interface {
	OnLLMError(ctx context.Context, turn *TurnState, call *LLMCall, err error) (bool, error)
}

type TurnToolCallHook interface {
	OnToolCalls(ctx context.Context, turn *TurnState, msg llm.Message) error
}

type TurnOutputHook interface {
	OnFinalOutput(ctx context.Context, turn *TurnState, output string) error
}

type prioritizedHook interface {
	Priority() int
}

func mergeHookSets(base, extra HookSet) HookSet {
	runHooks := append(append([]RunHook(nil), base.RunHooks...), extra.RunHooks...)
	turnHooks := append(append([]TurnHook(nil), base.TurnHooks...), extra.TurnHooks...)
	sort.SliceStable(runHooks, func(i, j int) bool {
		return hookPriority(runHooks[i]) < hookPriority(runHooks[j])
	})
	sort.SliceStable(turnHooks, func(i, j int) bool {
		return hookPriority(turnHooks[i]) < hookPriority(turnHooks[j])
	})
	return HookSet{
		RunHooks:  runHooks,
		TurnHooks: turnHooks,
	}
}

func hookPriority(h any) int {
	if h == nil {
		return 0
	}
	if prioritized, ok := h.(prioritizedHook); ok {
		return prioritized.Priority()
	}
	return 0
}

func executeRunBeforePromptHooks(ctx context.Context, hooks []RunHook, run *RunState) error {
	for _, hook := range hooks {
		typed, ok := hook.(RunPrepareHook)
		if !ok {
			continue
		}
		if err := typed.BeforePrompt(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func executeRunAfterHooks(ctx context.Context, hooks []RunHook, run *RunState) error {
	for _, hook := range hooks {
		typed, ok := hook.(RunFinishHook)
		if !ok {
			continue
		}
		if err := typed.AfterRun(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func executeRunFinalizeHooks(ctx context.Context, hooks []RunHook, run *RunState, runErr error) error {
	for _, hook := range hooks {
		typed, ok := hook.(RunFinalizeHook)
		if !ok {
			continue
		}
		if err := typed.OnRunFinish(ctx, run, runErr); err != nil {
			return err
		}
	}
	return nil
}

func executeTurnBeforeLLMHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, call *LLMCall) error {
	for _, hook := range hooks {
		typed, ok := hook.(TurnBeforeLLMHook)
		if !ok {
			continue
		}
		if err := typed.BeforeLLM(ctx, turn, call); err != nil {
			return err
		}
	}
	return nil
}

func executeTurnLLMStartHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, info LLMStartInfo) error {
	for _, hook := range hooks {
		typed, ok := hook.(TurnLLMStartHook)
		if !ok {
			continue
		}
		if err := typed.OnLLMStart(ctx, turn, info); err != nil {
			return err
		}
	}
	return nil
}

func executeTurnLLMDeltaHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, event llm.StreamEvent) {
	for _, hook := range hooks {
		typed, ok := hook.(TurnLLMDeltaHook)
		if !ok {
			continue
		}
		_ = typed.OnLLMDelta(ctx, turn, event)
	}
}

func executeTurnLLMErrorHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, call *LLMCall, err error) (bool, error) {
	retry := false
	for _, hook := range hooks {
		typed, ok := hook.(TurnLLMErrorHook)
		if !ok {
			continue
		}
		hookRetry, hookErr := typed.OnLLMError(ctx, turn, call, err)
		if hookErr != nil {
			return false, hookErr
		}
		if hookRetry {
			retry = true
		}
	}
	return retry, nil
}

func executeTurnToolCallHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, msg llm.Message) error {
	for _, hook := range hooks {
		typed, ok := hook.(TurnToolCallHook)
		if !ok {
			continue
		}
		if err := typed.OnToolCalls(ctx, turn, msg); err != nil {
			return err
		}
	}
	return nil
}

func executeTurnOutputHooks(ctx context.Context, hooks []TurnHook, turn *TurnState, output string) error {
	for _, hook := range hooks {
		typed, ok := hook.(TurnOutputHook)
		if !ok {
			continue
		}
		if err := typed.OnFinalOutput(ctx, turn, output); err != nil {
			return err
		}
	}
	return nil
}
