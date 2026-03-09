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
	// compactThreshold is the ratio of context usage that triggers auto-compaction
	compactThreshold = 0.85
	// compactMinMessages is the minimum number of messages before compaction is considered
	compactMinMessages = 6
	// summaryReserveTokens is the token budget reserved for the summary output
	summaryReserveTokens = 2000
)

// Compactor handles automatic context compaction by generating LLM summaries.
type Compactor struct {
	store     memory.StoreAPI
	llm       *llm.Client
	estimator *llm.TokenEstimator
	model     string
}

// NewCompactor creates a compactor for automatic context summarization.
func NewCompactor(store memory.StoreAPI, llmClient *llm.Client, estimator *llm.TokenEstimator, model string) *Compactor {
	return &Compactor{
		store:     store,
		llm:       llmClient,
		estimator: estimator,
		model:     model,
	}
}

// ShouldCompact returns true if the context usage exceeds the threshold.
func (c *Compactor) ShouldCompact(messages []llm.Message, budget int) bool {
	if len(messages) < compactMinMessages || budget <= 0 || c.estimator == nil {
		return false
	}
	tokens := c.estimator.CountMessages(messages)
	ratio := float64(tokens) / float64(budget)
	return ratio >= compactThreshold
}

// Compact generates a summary of messages and stores it, returning the summary.
// It preserves system messages and recent user messages while summarizing the conversation.
func (c *Compactor) Compact(ctx context.Context, sessionID int64, messages []llm.Message, budget int) (*memory.Summary, error) {
	log := logging.L().Named("compactor")
	if c.llm == nil || c.store == nil {
		return nil, fmt.Errorf("compactor not configured")
	}

	// Separate system messages and conversation
	var conversation []llm.Message
	for _, msg := range messages {
		if msg.Role != "system" {
			conversation = append(conversation, msg)
		}
	}

	if len(conversation) < compactMinMessages {
		return nil, fmt.Errorf("not enough messages to compact")
	}

	// Build the summary prompt with conversation history
	summaryPrompt := buildCompactPrompt(conversation)
	summaryReq := []llm.Message{
		{Role: "user", Content: summaryPrompt},
	}

	// Reserve tokens for the summary output
	availableBudget := budget - summaryReserveTokens
	if availableBudget < 1000 {
		availableBudget = 1000
	}

	// Trim the conversation to fit in budget for the summary request
	if c.estimator != nil {
		tokens := c.estimator.CountMessages(summaryReq)
		if tokens > availableBudget {
			// Keep only recent messages for summarization
			trimmed := selectLLMMessagesByTokens(conversation, c.estimator, availableBudget-tokens)
			summaryReq = []llm.Message{{Role: "user", Content: buildCompactPrompt(trimmed)}}
		}
	}

	log.Debug("generating context summary",
		zap.Int64("session_id", sessionID),
		zap.Int("messages", len(conversation)),
	)

	// Call LLM to generate summary
	resp, err := c.llm.Chat(ctx, llm.ChatRequest{
		Model:    c.model,
		Messages: summaryReq,
		Purpose:  llm.PurposeSummary,
	})
	if err != nil {
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
	}

	// Get the latest message ID to mark what was summarized
	latest, err := c.store.LoadViewMessages(ctx, sessionID, 1)
	if err == nil && len(latest) > 0 {
		summary.EntryID = latest[len(latest)-1].ID
	}

	// Store the summary
	_, err = c.store.AppendSummary(ctx, sessionID, *summary)
	if err != nil {
		log.Error("failed to store summary", zap.Error(err))
		return nil, err
	}

	log.Info("context compacted",
		zap.Int64("session_id", sessionID),
		zap.Int("original_messages", len(conversation)),
	)

	return summary, nil
}

// buildCompactPrompt creates the summarization prompt for a general-purpose agent.
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
