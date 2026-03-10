package agent

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
	// compactThreshold is the ratio of context usage that triggers auto-compaction.
	compactThreshold = 0.85
	// compactMinMessages avoids compacting very short conversations.
	compactMinMessages = 6
	// summaryReserveTokens reserves room for summary model output.
	summaryReserveTokens = 2000
	// compactRetryScale shrinks summary input when context overflow occurs.
	compactRetryScale = 0.5
	// compactMaxAttempts prevents infinite retry loops.
	compactMaxAttempts = 4
)

// Compactor handles automatic context compaction by generating LLM summaries.
type Compactor struct {
	store     memory.StoreAPI
	llm       *llm.Client
	estimator *llm.TokenEstimator
	model     string
}

func NewCompactor(store memory.StoreAPI, llmClient *llm.Client, estimator *llm.TokenEstimator, model string) *Compactor {
	return &Compactor{store: store, llm: llmClient, estimator: estimator, model: model}
}

func (c *Compactor) ShouldCompact(messages []llm.Message, budget int) bool {
	if len(messages) < compactMinMessages || budget <= 0 || c.estimator == nil {
		return false
	}
	tokens := c.estimator.CountMessages(messages)
	return float64(tokens)/float64(budget) >= compactThreshold
}

// Compact summarizes the provided persisted prefix and stores it as the latest summary.
func (c *Compactor) Compact(ctx context.Context, sessionID int64, prefix []memory.Message, budget int) (*memory.Summary, error) {
	log := logging.L().Named("compactor")
	if c.llm == nil || c.store == nil {
		return nil, fmt.Errorf("compactor not configured")
	}
	if len(prefix) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}
	entryID := prefix[len(prefix)-1].ID
	if entryID == 0 {
		return nil, fmt.Errorf("invalid summary entry boundary")
	}

	conversation := toLLMMessages(prefix)
	available := budget - summaryReserveTokens
	if available < 800 {
		available = 800
	}

	scale := 1.0
	for attempt := 0; attempt < compactMaxAttempts; attempt++ {
		toSummarize := c.selectSummaryMessages(conversation, available, scale)
		prompt := buildCompactPrompt(toSummarize)

		resp, err := c.llm.Chat(ctx, llm.ChatRequest{
			Model:    c.model,
			Messages: []llm.Message{{Role: "user", Content: prompt}},
			Purpose:  llm.PurposeSummary,
		})
		if err != nil {
			if llm.IsContextWindowExceeded(err) {
				log.Warn("summary request exceeded context, shrinking summary input",
					zap.Int64("session_id", sessionID),
					zap.Int("attempt", attempt),
					zap.Float64("scale", scale),
					zap.Error(err),
				)
				scale *= compactRetryScale
				continue
			}
			return nil, err
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("empty summary response")
		}
		text := strings.TrimSpace(resp.Choices[0].Message.Content)
		if text == "" {
			text = "Conversation compacted. Continue from recent context."
		}
		return c.storeSummary(ctx, sessionID, entryID, text)
	}

	return nil, fmt.Errorf("summary generation exceeded retry limit")
}

func (c *Compactor) selectSummaryMessages(conversation []llm.Message, available int, scale float64) []llm.Message {
	if len(conversation) == 0 {
		return nil
	}
	if c.estimator == nil || available <= 0 {
		return conversation
	}
	target := int(float64(available) * scale)
	if target < 300 {
		target = 300
	}
	trimmed := selectLLMMessagesByTokens(conversation, c.estimator, target)
	if len(trimmed) == 0 {
		return conversation[len(conversation)-1:]
	}
	return trimmed
}

func (c *Compactor) storeSummary(ctx context.Context, sessionID, entryID int64, text string) (*memory.Summary, error) {
	summary := &memory.Summary{
		SessionID: sessionID,
		EntryID:   entryID,
		Phase:     "auto-compact",
		Summary:   text,
	}
	if _, err := c.store.AppendSummary(ctx, sessionID, *summary); err != nil {
		return nil, err
	}
	return summary, nil
}

func buildCompactPrompt(messages []llm.Message) string {
	var b strings.Builder
	b.WriteString("Create a concise handoff summary so another assistant can continue the task.\n\n")
	b.WriteString("Include key decisions, constraints, current status, and next steps.\n")
	b.WriteString("Do not include verbatim transcript unless critical.\n\n")
	b.WriteString("Conversation:\n---\n")

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("User: ")
		case "assistant":
			b.WriteString("Assistant: ")
		case "tool":
			b.WriteString("Tool: ")
		default:
			b.WriteString(msg.Role)
			b.WriteString(": ")
		}
		b.WriteString(msg.Content)
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
	b.WriteString("---\n\nSummary:")
	return b.String()
}
