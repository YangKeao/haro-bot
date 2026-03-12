package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

const (
	compactThreshold     = 0.85
	compactMinMessages   = 6
	summaryReserveTokens = 2000
	compactRetryScale    = 0.7
)

type compactor struct {
	store     memory.StoreAPI
	llm       llm.ChatModel
	estimator *llm.TokenEstimator
	model     string
}

func newCompactor(store memory.StoreAPI, llmClient llm.ChatModel, estimator *llm.TokenEstimator, model string) *compactor {
	return &compactor{
		store:     store,
		llm:       llmClient,
		estimator: estimator,
		model:     model,
	}
}

func (c *compactor) shouldCompact(messages []llm.Message, budget int) bool {
	if len(messages) < compactMinMessages || budget <= 0 || c.estimator == nil {
		return false
	}
	tokens := c.estimator.CountMessages(messages)
	ratio := float64(tokens) / float64(budget)
	return ratio >= compactThreshold
}

func (c *compactor) compact(ctx context.Context, sessionID int64, messages []llm.Message, budget int, cutoffEntryID int64) (*memory.Summary, error) {
	log := logging.L().Named("compactor")
	if c.llm == nil || c.store == nil {
		return nil, fmt.Errorf("compactor not configured")
	}
	if cutoffEntryID <= 0 {
		return nil, fmt.Errorf("cutoff entry id required")
	}

	var conversation []llm.Message
	for _, msg := range messages {
		if msg.Role != "system" {
			conversation = append(conversation, msg)
		}
	}

	availableBudget := budget - summaryReserveTokens
	if availableBudget < 1000 {
		availableBudget = 1000
	}

	scale := 1.0
	attempt := 0

	for {
		var toSummarize []llm.Message
		if c.estimator != nil {
			scaledBudget := int(float64(availableBudget) * scale)
			if scaledBudget < 500 {
				scaledBudget = 500
			}
			templateOverhead := 200
			targetTokens := scaledBudget - templateOverhead
			if targetTokens < 300 {
				targetTokens = 300
			}
			toSummarize = selectLLMMessagesByTokens(conversation, c.estimator, targetTokens)
		} else {
			toSummarize = conversation
		}

		if len(toSummarize) == 0 {
			log.Info("no messages left to summarize, creating empty summary",
				zap.Int64("session_id", sessionID),
				zap.Int("attempt", attempt),
			)
			summary := &memory.Summary{
				SessionID: sessionID,
				Summary:   "Context cleared due to token limit. Starting fresh conversation.",
				Phase:     "auto-compact",
				EntryID:   cutoffEntryID,
			}
			if _, err := c.store.AppendSummary(ctx, sessionID, *summary); err != nil {
				log.Error("failed to store summary", zap.Error(err))
				return nil, err
			}
			return summary, nil
		}

		summaryPrompt := buildCompactPrompt(toSummarize)
		summaryReq := []llm.Message{{Role: "user", Content: summaryPrompt}}

		log.Debug("generating context summary",
			zap.Int64("session_id", sessionID),
			zap.Int("attempt", attempt),
			zap.Float64("scale", scale),
			zap.Int("messages", len(toSummarize)),
		)

		resp, err := c.llm.Chat(ctx, llm.ChatRequest{
			Model:    c.model,
			Messages: summaryReq,
			Purpose:  llm.PurposeSummary,
		})
		if err != nil {
			if llm.IsContextWindowExceeded(err) {
				log.Warn("summary request exceeded context, retrying with smaller scale",
					zap.Int64("session_id", sessionID),
					zap.Int("attempt", attempt),
					zap.Float64("scale", scale),
					zap.Int("messages", len(toSummarize)),
					zap.Error(err),
				)
				scale *= compactRetryScale
				attempt++
				continue
			}
			log.Error("summary generation failed", zap.Error(err))
			return nil, err
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty summary response")
		}

		summary := &memory.Summary{
			SessionID: sessionID,
			Summary:   resp.Choices[0].Message.Content,
			Phase:     "auto-compact",
			EntryID:   cutoffEntryID,
		}
		if _, err := c.store.AppendSummary(ctx, sessionID, *summary); err != nil {
			log.Error("failed to store summary", zap.Error(err))
			return nil, err
		}

		log.Info("context compacted",
			zap.Int64("session_id", sessionID),
			zap.Int("original_messages", len(conversation)),
			zap.Int("summarized_messages", len(toSummarize)),
			zap.Int("attempts", attempt+1),
		)
		return summary, nil
	}
}

func buildCompactPrompt(messages []llm.Message) string {
	var b strings.Builder
	b.WriteString("You are performing a CONTEXT CHECKPOINT COMPACTION for a conversation assistant.\n\n")
	b.WriteString("Create a concise handoff summary for another LLM that will resume the task.\n\n")
	b.WriteString("Include:\n")
	b.WriteString("- Key decisions and conclusions reached\n")
	b.WriteString("- Important facts, preferences, or constraints mentioned by the user\n")
	b.WriteString("- Current state of any ongoing tasks or discussions\n")
	b.WriteString("- What remains to be done (if applicable)\n\n")
	b.WriteString("Guidelines:\n")
	b.WriteString("- Be concise and structured\n")
	b.WriteString("- Focus on information needed to continue seamlessly\n")
	b.WriteString("- Skip redundant or trivial details\n")
	b.WriteString("- Do NOT include the actual conversation, only the summary\n\n")
	b.WriteString("Conversation to summarize:\n")
	b.WriteString("---\n")

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		case "tool":
			b.WriteString("Tool: ")
		default:
			b.WriteString(fmt.Sprintf("%s: ", msg.Role))
		}
		if msg.Content != "" {
			b.WriteString(msg.Content)
		}
		if len(msg.ToolCalls) > 0 {
			b.WriteString(" [")
			for i, tc := range msg.ToolCalls {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(tc.Function.Name)
			}
			b.WriteString("]")
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("Summary:")

	return b.String()
}

func selectLLMMessagesByTokens(messages []llm.Message, estimator *llm.TokenEstimator, budget int) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	if estimator == nil || budget <= 0 {
		return messages
	}
	requiredToolCalls := map[string]struct{}{}
	selected := make([]llm.Message, 0, len(messages))
	used := 0
	includedAny := false
	seenTool := false
	log := logging.L().Named("context_trim")

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		mustInclude := false
		if msg.Role == "tool" && msg.ToolCallID == "" {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			paired := make([]llm.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if call.ID == "" {
					continue
				}
				if _, ok := requiredToolCalls[call.ID]; ok {
					paired = append(paired, call)
					delete(requiredToolCalls, call.ID)
				}
			}
			if len(paired) == 0 && msg.Content == "" {
				continue
			}
			if len(paired) == 0 {
				msg.ToolCalls = nil
			} else {
				msg.ToolCalls = paired
				mustInclude = true
			}
		}
		firstTool := msg.Role == "tool" && !seenTool
		if msg.Role == "tool" {
			seenTool = true
		}
		if !includedAny && msg.Role != "tool" {
			mustInclude = true
		}

		tokens := estimator.CountMessage(msg)
		if firstTool && tokens > budget {
			originalTokens := tokens
			msg = llm.Message{
				Role:       "tool",
				ToolCallID: msg.ToolCallID,
				Content:    "tool response omitted: output too large for context window",
			}
			tokens = estimator.CountMessage(msg)
			log.Debug("rewrote oversized tool response", zap.String("tool_call_id", msg.ToolCallID), zap.Int("original_tokens", originalTokens), zap.Int("rewritten_tokens", tokens), zap.Int("budget", budget))
		}
		if mustInclude || used+tokens <= budget {
			used += tokens
			selected = append(selected, msg)
			includedAny = true
			if msg.Role == "tool" && msg.ToolCallID != "" {
				requiredToolCalls[msg.ToolCallID] = struct{}{}
			}
			continue
		}
		if len(requiredToolCalls) == 0 {
			break
		}
	}

	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}
