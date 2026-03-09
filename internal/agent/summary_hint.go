package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

type summarySignal struct {
	toolCallCount  int
	fileEditCount  int
	execCount      int
	toolErrorCount int
	phaseWithTools bool
}

// summaryHint generates hints for the LLM to consider creating a summary.
// Note: Auto-compaction handles context window limits automatically at 85%,
// so this function only provides hints for task-phase transitions and error recovery.
func summaryHint(messages []memory.Message) string {
	if len(messages) == 0 {
		return ""
	}
	signal := analyzeSummarySignal(messages)
	hints := make([]string, 0, 2)

	// Hint for task phase completion
	if signal.phaseWithTools && (signal.fileEditCount > 0 || signal.execCount > 0 || signal.toolCallCount >= 6) {
		hints = append(hints, "Optional summary: a tool-driven phase just completed. Summarize if it helps preserve state before switching tasks.")
	}
	// Hint for error recovery
	if signal.phaseWithTools && signal.toolErrorCount >= 2 {
		hints = append(hints, "Recommended summary: multiple tool errors occurred. Summarize to capture failure context before retrying or changing approach.")
	}
	if len(hints) == 0 {
		return ""
	}
	return fmt.Sprintf("Host notice: %s Use session_summary with a concise summary plus phase (if this is a transition) and state (key decisions, modified files/tests, open TODOs), then continue with the user request.", strings.Join(hints, " "))
}

func analyzeSummarySignal(messages []memory.Message) summarySignal {
	signal := summarySignal{}
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
		case "apply_patch":
			fileEdits++
		case "exec_command":
			execs++
		case "session_summary":
			// ignore
		}
	}
	return toolCalls, fileEdits, execs
}
