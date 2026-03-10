package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/YangKeao/haro-bot/internal/llm"
	"go.uber.org/zap"
)

func (s *Session) callLLMWithTrim(ctx context.Context, log *zap.Logger, model string, messages []llm.Message, tools []llm.Tool, observer ProgressObserver) (llm.ChatResponse, []llm.Message, error) {
	var out llm.ChatResponse
	if s == nil || s.deps == nil {
		return out, messages, errors.New("session not configured")
	}

	estimator := s.estimatorForModel(model)
	budget := computeTokenBudget(s.deps.contextConfig)
	compactor := NewCompactor(s.deps.store, s.deps.llm, estimator, model)

	if compactor.ShouldCompact(messages, budget.InputBudget) {
		log.Info("context approaching limit, triggering preemptive compact", zap.Int64("session_id", s.id))
		newMessages, compactErr := s.compactAndReload(ctx, log, compactor, messages, budget.InputBudget)
		if compactErr != nil {
			return out, messages, fmt.Errorf("failed to compact context: %w", compactErr)
		}
		messages = newMessages
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
		Messages:         messages,
		Tools:            tools,
		ReasoningEnabled: s.deps.reasoning.Enabled,
		ReasoningEffort:  s.deps.reasoning.Effort,
		StreamHandler:    handler,
		Purpose:          llm.PurposeChat,
	})
	if err == nil {
		return resp, messages, nil
	}

	if !llm.IsContextWindowExceeded(err) {
		return resp, messages, err
	}

	log.Warn("context window exceeded, triggering compact",
		zap.Int64("session_id", s.id),
		zap.Error(err),
	)
	newMessages, compactErr := s.compactAndReload(ctx, log, compactor, messages, budget.InputBudget)
	if compactErr != nil {
		return out, messages, fmt.Errorf("failed to compact context after overflow: %w", compactErr)
	}
	messages = newMessages

	if observer != nil {
		observer.OnLLMStart(ctx, LLMStartInfo{Model: model, Attempt: 1})
	}
	resp, err = s.deps.llm.Chat(ctx, llm.ChatRequest{
		Model:            model,
		Messages:         messages,
		Tools:            tools,
		ReasoningEnabled: s.deps.reasoning.Enabled,
		ReasoningEffort:  s.deps.reasoning.Effort,
		StreamHandler:    handler,
		Purpose:          llm.PurposeChat,
	})
	return resp, messages, err
}

// compactAndReload summarizes an older prefix from persisted history and keeps the newest tail.
func (s *Session) compactAndReload(ctx context.Context, log *zap.Logger, compactor *Compactor, messages []llm.Message, budget int) ([]llm.Message, error) {
	view, _, err := s.deps.store.LoadViewMessages(ctx, s.id, 0)
	if err != nil {
		return nil, err
	}

	prefix, tail := selectCompactionPrefixAndTail(view)
	baseSystem, existingSummary := splitSystemMessages(messages)
	summaryMsg := existingSummary
	if len(prefix) > 0 {
		summary, compactErr := compactor.Compact(ctx, s.id, prefix, budget)
		if compactErr != nil {
			return nil, compactErr
		}
		if nextSummary := formatSummaryMessage(summary); nextSummary != "" {
			summaryMsg = nextSummary
		}
	}

	systemMsgs := append([]llm.Message(nil), baseSystem...)
	if summaryMsg != "" {
		systemMsgs = append(systemMsgs, llm.Message{Role: "system", Content: summaryMsg})
	}

	// Re-attach runtime-only messages that are not persisted in store yet
	// (for example Interrupt with storeInSession=false).
	conversation := appendTransientTail(toLLMMessages(tail), messages, len(view))
	conversation, trimmed := fitConversationToBudget(systemMsgs, conversation, compactor.estimator, budget)
	if trimmed {
		log.Warn("compacted context still exceeded budget; trimmed preserved tail",
			zap.Int64("session_id", s.id),
			zap.Int("preserved_before", len(toLLMMessages(tail))),
			zap.Int("preserved_after", len(conversation)),
		)
	}

	newMessages := append(systemMsgs, conversation...)
	log.Info("context compacted",
		zap.Int64("session_id", s.id),
		zap.Int("old_message_count", len(messages)),
		zap.Int("new_message_count", len(newMessages)),
		zap.Int("summarized_messages", len(prefix)),
		zap.Int("preserved_messages", len(tail)),
	)
	return newMessages, nil
}
