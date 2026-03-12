package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

func (s *Session) callLLMWithTrim(ctx context.Context, log *zap.Logger, model string, stored []StoredMessage, transient TransientContext, tools []llm.Tool, observer ProgressObserver) (llm.ChatResponse, []StoredMessage, TransientContext, error) {
	var out llm.ChatResponse
	if s == nil || s.deps == nil {
		return out, stored, transient, errors.New("session not configured")
	}

	estimator := s.estimatorForModel(model)
	budget := computeTokenBudget(s.deps.contextConfig)
	compactor := NewCompactor(s.deps.store, s.deps.llm, estimator, model)

	if compactor.ShouldCompact(composeLLMMessages(stored, transient), budget.InputBudget) {
		log.Info("context approaching limit, triggering preemptive compact",
			zap.Int64("session_id", s.id),
		)
		reloadedStored, reloadedTransient, compErr := s.compactAndReload(ctx, log, compactor, stored, transient, budget.InputBudget)
		if compErr != nil {
			return out, stored, transient, fmt.Errorf("failed to compact context: %w", compErr)
		}
		stored = reloadedStored
		transient = reloadedTransient
	}

	if observer != nil {
		observer.OnLLMStart(ctx, LLMStartInfo{Model: model, Attempt: 0})
	}

	var handler llm.StreamHandler
	if observer != nil {
		handler = func(event llm.StreamEvent) {
			if event.ReasoningDelta != "" {
				observer.OnLLMReasoningDelta(ctx, event.ReasoningDelta)
			}
			if event.Delta != "" {
				observer.OnLLMStreamDelta(ctx, event.Delta)
			}
		}
	}

	resp, err := s.deps.llm.Chat(ctx, llm.ChatRequest{
		Model:            model,
		Messages:         composeLLMMessages(stored, transient),
		Tools:            tools,
		ReasoningEnabled: s.deps.reasoning.Enabled,
		ReasoningEffort:  s.deps.reasoning.Effort,
		StreamHandler:    handler,
		Purpose:          llm.PurposeChat,
	})

	if err != nil {
		if llm.IsContextWindowExceeded(err) {
			log.Warn("context window exceeded, triggering compact",
				zap.Int64("session_id", s.id),
				zap.Error(err),
			)
			reloadedStored, reloadedTransient, compErr := s.compactAndReload(ctx, log, compactor, stored, transient, budget.InputBudget)
			if compErr != nil {
				return out, stored, transient, fmt.Errorf("failed to compact context after overflow: %w", compErr)
			}
			stored = reloadedStored
			transient = reloadedTransient
			if observer != nil {
				observer.OnLLMStart(ctx, LLMStartInfo{Model: model, Attempt: 1})
			}
			resp, err = s.deps.llm.Chat(ctx, llm.ChatRequest{
				Model:            model,
				Messages:         composeLLMMessages(stored, transient),
				Tools:            tools,
				ReasoningEnabled: s.deps.reasoning.Enabled,
				ReasoningEffort:  s.deps.reasoning.Effort,
				StreamHandler:    handler,
				Purpose:          llm.PurposeChat,
			})
		}
		return resp, stored, transient, err
	}

	return resp, stored, transient, nil
}

func (s *Session) compactAndReload(ctx context.Context, log *zap.Logger, compactor *Compactor, stored []StoredMessage, transient TransientContext, budget int) ([]StoredMessage, TransientContext, error) {
	toCompact, tail := selectCompactionPrefixAndTail(stored)
	if len(toCompact) == 0 {
		return stored, transient, nil
	}
	cutoffEntryID, err := compactCutoffEntryID(toCompact)
	if err != nil {
		return nil, transient, err
	}

	log.Debug("compacting persisted context",
		zap.Int64("session_id", s.id),
		zap.Int("stored_messages", len(stored)),
		zap.Int("compact_prefix", len(toCompact)),
		zap.Int("preserved_tail", len(tail)),
	)

	if _, err := compactor.Compact(ctx, s.id, storedMessagesToLLM(toCompact), budget, cutoffEntryID); err != nil {
		return nil, transient, err
	}

	recent, summary, err := s.deps.store.LoadViewMessages(ctx, s.id, 0)
	if err != nil {
		return nil, transient, err
	}
	reloadedStored, err := toStoredMessages(recent)
	if err != nil {
		return nil, transient, err
	}
	reloadedTransient := refreshTransientContext(transient, summary, recent)

	log.Info("context compacted",
		zap.Int64("session_id", s.id),
		zap.Int("old_stored_count", len(stored)),
		zap.Int("new_stored_count", len(reloadedStored)),
	)
	return reloadedStored, reloadedTransient, nil
}

func compactCutoffEntryID(messages []StoredMessage) (int64, error) {
	if len(messages) == 0 {
		return 0, errors.New("no persisted message found in compaction prefix")
	}
	last := messages[len(messages)-1].EntryID
	if last <= 0 {
		return 0, fmt.Errorf("invalid compact cutoff entry id: %d", last)
	}
	return last, nil
}
