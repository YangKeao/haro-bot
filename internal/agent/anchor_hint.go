package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

type anchorSignal struct {
	toolCallCount  int
	fileEditCount  int
	execCount      int
	toolErrorCount int
	phaseWithTools bool
}

type anchorUsage struct {
	TokensUsed  int
	TokenBudget int
}

func anchorHint(messages []memory.Message, usage anchorUsage) string {
	if len(messages) == 0 {
		return ""
	}
	signal := analyzeAnchorSignal(messages)
	hints := make([]string, 0, 3)
	if level := nearLimitLevel(usage.TokensUsed, usage.TokenBudget); level != "" {
		hints = append(hints, nearLimitHint(level))
	}
	if signal.phaseWithTools && (signal.fileEditCount > 0 || signal.execCount > 0 || signal.toolCallCount >= 6) {
		hints = append(hints, "Optional anchor: a tool-driven phase just completed. Anchor if it helps preserve state before switching tasks.")
	}
	if signal.phaseWithTools && signal.toolErrorCount >= 2 {
		hints = append(hints, "Recommended anchor: multiple tool errors occurred. Anchor to capture failure context before retrying or changing approach.")
	}
	if len(hints) == 0 {
		return ""
	}
	return fmt.Sprintf("Host notice: %s Use session_anchor with a concise summary plus phase (if this is a transition) and state (key decisions, modified files/tests, open TODOs), then continue with the user request.", strings.Join(hints, " "))
}

func analyzeAnchorSignal(messages []memory.Message) anchorSignal {
	signal := anchorSignal{}
	ordered := make([]memory.Message, len(messages))
	copy(ordered, messages)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	lastUserIdx := -1
	for i := len(ordered) - 1; i >= 0; i-- {
		if ordered[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx <= 0 {
		return signal
	}
	prior := ordered[:lastUserIdx]
	for _, msg := range prior {
		if msg.Role == "assistant" && msg.Metadata != nil && len(msg.Metadata.ToolCalls) > 0 {
			toolCalls, fileEdits, execs := countToolCalls(msg.Metadata.ToolCalls)
			signal.toolCallCount += toolCalls
			signal.fileEditCount += fileEdits
			signal.execCount += execs
		}
		if msg.Role == "tool" && msg.Metadata != nil && msg.Metadata.Status == "error" {
			signal.toolErrorCount++
		}
	}

	lastAssistantFinalIdx := -1
	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].Role == "assistant" && (prior[i].Metadata == nil || len(prior[i].Metadata.ToolCalls) == 0) {
			lastAssistantFinalIdx = i
			break
		}
	}
	if lastAssistantFinalIdx != -1 {
		for i := 0; i < lastAssistantFinalIdx; i++ {
			if prior[i].Role == "assistant" && prior[i].Metadata != nil && len(prior[i].Metadata.ToolCalls) > 0 {
				signal.phaseWithTools = true
				break
			}
		}
	}
	return signal
}

func countToolCalls(calls []llm.ToolCall) (toolCalls, fileEdits, execs int) {
	for _, call := range calls {
		toolCalls++
		switch strings.ToLower(call.Function.Name) {
		case "write", "edit":
			fileEdits++
		case "exec":
			execs++
		case "session_anchor":
			// ignore
		}
	}
	return toolCalls, fileEdits, execs
}

func nearLimitLevel(tokensUsed, tokenBudget int) string {
	if tokenBudget <= 0 || tokensUsed <= 0 {
		return ""
	}
	ratio := float64(tokensUsed) / float64(tokenBudget)
	switch {
	case ratio >= 0.95:
		return "critical"
	case ratio >= 0.85:
		return "high"
	case ratio >= 0.75:
		return "medium"
	default:
		return ""
	}
}

func nearLimitHint(level string) string {
	switch level {
	case "critical":
		return "Context window is critical. Anchor now unless you will finish in the next reply."
	case "high":
		return "Context window is tight. Prefer anchoring unless you expect to finish within 1-2 replies."
	case "medium":
		return "Context window is getting long. Consider anchoring if more steps remain."
	default:
		return ""
	}
}
