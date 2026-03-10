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

// extractLastTurn extracts the last complete turn from messages.
// A "turn" is defined as the triggering user message plus the last assistant message
// and all related tool responses. This ensures that after compact, the LLM can
// continue with the task because it has both the original request and any pending tool calls.
func extractLastTurn(messages []llm.Message) (lastTurn, remaining []llm.Message) {
	if len(messages) == 0 {
		return nil, messages
	}

	// Find the last assistant message (which may contain tool calls)
	lastAssistantIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	// If no assistant message, check for last user message
	if lastAssistantIdx == -1 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				return messages[i:], messages[:i]
			}
		}
		return nil, messages
	}

	// Find the last user message that triggered this assistant response
	// This is the user message immediately before the assistant message
	lastUserIdx := -1
	for i := lastAssistantIdx - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	// Collect tool call IDs from the last assistant message
	toolCallIDs := make(map[string]bool)
	for _, call := range messages[lastAssistantIdx].ToolCalls {
		toolCallIDs[call.ID] = true
	}

	// Determine the start of the last turn
	turnStartIdx := lastAssistantIdx
	if lastUserIdx != -1 {
		turnStartIdx = lastUserIdx
	}

	// The last turn includes:
	// 1. The triggering user message (if exists)
	// 2. The last assistant message
	// 3. All subsequent tool responses (that respond to its tool calls)
	// 4. Any subsequent user message
	lastTurn = messages[turnStartIdx:]
	remaining = messages[:turnStartIdx]

	// If there are tool calls, we need to keep all related tool responses
	// They are already included in lastTurn since we take everything from turnStartIdx onwards
	// But we should verify the tool responses match the tool calls
	if len(toolCallIDs) > 0 {
		// Filter lastTurn to only include:
		// - The triggering user message (if exists)
		// - The assistant message
		// - Tool responses that match our tool calls
		// - Any user messages at the end
		filtered := make([]llm.Message, 0, len(lastTurn))
		
		// Find where the assistant message is in lastTurn
		assistantOffset := 0
		if lastUserIdx != -1 {
			assistantOffset = lastAssistantIdx - lastUserIdx
		}
		
		// Include messages before the assistant (the user message)
		for i := 0; i < assistantOffset; i++ {
			filtered = append(filtered, lastTurn[i])
		}
		
		// Always include the assistant message
		filtered = append(filtered, lastTurn[assistantOffset])

		for i := assistantOffset + 1; i < len(lastTurn); i++ {
			msg := lastTurn[i]
			if msg.Role == "tool" {
				// Only include tool responses that match our tool calls
				if toolCallIDs[msg.ToolCallID] {
					filtered = append(filtered, msg)
				}
			} else if msg.Role == "user" {
				// Include any user message that came after
				filtered = append(filtered, msg)
			}
		}
		lastTurn = filtered
	}

	return lastTurn, remaining
}

// compactAndReload performs compaction and preserves the last turn for task continuity.
// It returns the updated messages or an error.
func (s *Session) compactAndReload(ctx context.Context, log *zap.Logger, compactor *Compactor, messages []llm.Message, budget int) ([]llm.Message, error) {
	// 1. Extract the last turn before compacting
	lastTurn, toCompact := extractLastTurn(messages)

	log.Debug("extracting last turn before compact",
		zap.Int64("session_id", s.id),
		zap.Int("total_messages", len(messages)),
		zap.Int("last_turn_messages", len(lastTurn)),
		zap.Int("to_compact_messages", len(toCompact)),
	)

	// 2. Perform compaction on messages excluding the last turn
	summary, err := compactor.Compact(ctx, s.id, toCompact, budget)
	if err != nil {
		return nil, err
	}

	// 3. Rebuild messages: system messages + summary + last turn
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

	// 4. Append the preserved last turn
	newMessages := append(systemMsgs, lastTurn...)

	log.Info("context compacted",
		zap.Int64("session_id", s.id),
		zap.Int("old_message_count", len(messages)),
		zap.Int("new_message_count", len(newMessages)),
		zap.Int("preserved_last_turn", len(lastTurn)),
	)

	return newMessages, nil
}
