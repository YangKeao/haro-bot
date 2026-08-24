package agent

import "github.com/YangKeao/haro-bot/internal/llm"

func newTurnState(run *RunState, index int, model string, estimator *llm.TokenEstimator, tools []llm.Tool) *TurnState {
	return &TurnState{
		Run:       run,
		Index:     index,
		Model:     model,
		Stored:    projectObservations(run.Stored, run.TurnStartEntryID),
		Transient: run.Transient,
		Tools:     tools,
		Estimator: estimator,
	}
}

func projectObservations(messages []StoredMessage, turnStartEntryID int64) []StoredMessage {
	projected := append([]StoredMessage(nil), messages...)
	latest := make(map[string]int)
	for i, message := range projected {
		if message.EntryID < turnStartEntryID || message.Metadata == nil || message.Metadata.ObservationKey == "" {
			continue
		}
		latest[message.Metadata.ObservationKey] = i
	}
	for i := range projected {
		metadata := projected[i].Metadata
		if metadata == nil || metadata.ObservationKey == "" || projected[i].EntryID < turnStartEntryID || latest[metadata.ObservationKey] == i {
			continue
		}
		projected[i].Message.Content = "[Superseded observation omitted; use the latest result for this page.]"
	}
	return projected
}
