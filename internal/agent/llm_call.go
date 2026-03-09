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

	// Check if we should preemptively compact before calling LLM
	if compactor.ShouldCompact(messages, budget.InputBudget) {
		log.Info("context approaching limit, triggering preemptive compact",
			zap.Int64("session_id", s.id),
		)
		newMessages, compErr := s.compactAndReload(ctx, log, compactor, messages, budget.InputBudget)
		if compErr != nil {
			// Return error through normal path - this will be sent to user
			return out, messages, fmt.Errorf("failed to compact context: %w", compErr)
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

	if err != nil {
		if llm.IsContextWindowExceeded(err) {
			log.Warn("context window exceeded, triggering compact",
				zap.Int64("session_id", s.id),
				zap.Error(err),
			)
			newMessages, compErr := s.compactAndReload(ctx, log, compactor, messages, budget.InputBudget)
			if compErr != nil {
				// Return error through normal path
				return out, messages, fmt.Errorf("failed to compact context after overflow: %w", compErr)
			}
			messages = newMessages

			// Retry once - compactor guarantees success, so one retry is sufficient
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
		}
		return resp, messages, err
	}

	return resp, messages, nil
}

// compactAndReload performs compaction and reloads messages from the store.
// It returns the updated messages or an error.
func (s *Session) compactAndReload(ctx context.Context, log *zap.Logger, compactor *Compactor, messages []llm.Message, budget int) ([]llm.Message, error) {
	summary, err := compactor.Compact(ctx, s.id, messages, budget)
	if err != nil {
		return nil, err
	}

	// Reload messages from the new view
	recent, _, loadErr := s.deps.store.LoadViewMessages(ctx, s.id, 0)
	if loadErr != nil {
		log.Warn("failed to reload after compact", zap.Error(loadErr))
		return nil, loadErr
	}

	// Rebuild messages with the new compacted view
	// Preserve system messages
	var systemMsgs []llm.Message
	for _, msg := range messages {
		if msg.Role == "system" {
			systemMsgs = append(systemMsgs, msg)
		}
	}

	summaryMsg := formatSummaryMessage(summary)
	if summaryMsg != "" {
		systemMsgs = append(systemMsgs, llm.Message{Role: "system", Content: summaryMsg})
	}

	newMessages := append(systemMsgs, toLLMMessages(recent)...)

	log.Info("context compacted",
		zap.Int64("session_id", s.id),
		zap.Int("old_message_count", len(messages)),
		zap.Int("new_message_count", len(newMessages)),
	)

	return newMessages, nil
}
