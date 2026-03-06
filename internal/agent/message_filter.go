package agent

import (
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
)

// toLLMMessagesForContext filters tool call pairs so function calls always have outputs.
func toLLMMessagesForContext(msgs []memory.Message) []llm.Message {
	if len(msgs) == 0 {
		return nil
	}
	callIDs := map[string]struct{}{}
	outputIDs := map[string]struct{}{}
	for _, m := range msgs {
		if m.Role == "assistant" && m.Metadata != nil {
			for _, call := range m.Metadata.ToolCalls {
				if call.ID != "" {
					callIDs[call.ID] = struct{}{}
				}
			}
		}
		if m.Role == "tool" && m.Metadata != nil && m.Metadata.ToolCallID != "" {
			outputIDs[m.Metadata.ToolCallID] = struct{}{}
		}
	}
	valid := map[string]struct{}{}
	for id := range callIDs {
		if _, ok := outputIDs[id]; ok {
			valid[id] = struct{}{}
		}
	}

	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		llmMsg := toLLMMessage(m)
		switch llmMsg.Role {
		case "assistant":
			if len(llmMsg.ToolCalls) > 0 {
				filtered := make([]llm.ToolCall, 0, len(llmMsg.ToolCalls))
				for _, call := range llmMsg.ToolCalls {
					if call.ID == "" {
						continue
					}
					if _, ok := valid[call.ID]; ok {
						filtered = append(filtered, call)
					}
				}
				llmMsg.ToolCalls = filtered
			}
			if llmMsg.Content == "" && len(llmMsg.ToolCalls) == 0 {
				continue
			}
		case "tool":
			if llmMsg.ToolCallID == "" {
				continue
			}
			if _, ok := valid[llmMsg.ToolCallID]; !ok {
				continue
			}
		}
		out = append(out, llmMsg)
	}
	return out
}
