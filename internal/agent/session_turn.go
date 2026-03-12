package agent

import "github.com/YangKeao/haro-bot/internal/llm"

func newTurnState(run *RunState, index int, model string, estimator *llm.TokenEstimator, tools []llm.Tool) *TurnState {
	return &TurnState{
		Run:       run,
		Index:     index,
		Model:     model,
		Stored:    run.Stored,
		Transient: run.Transient,
		Tools:     tools,
		Estimator: estimator,
	}
}
