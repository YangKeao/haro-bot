package llm

import (
	"strings"

	"github.com/openai/openai-go/responses"
)

type responseMeta struct {
	id      string
	model   string
	created int64
}

type toolCallAccum struct {
	itemID    string
	callID    string
	name      string
	argsFinal string
	argsDelta strings.Builder
	seenDelta bool
}

type streamAccumulator struct {
	textDeltaSeen bool
	textDelta     strings.Builder
	textDone      string
	toolByItemID  map[string]*toolCallAccum
	toolByCallID  map[string]*toolCallAccum
	toolOrder     []*toolCallAccum
	meta          responseMeta
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolByItemID: make(map[string]*toolCallAccum),
		toolByCallID: make(map[string]*toolCallAccum),
	}
}

func (a *streamAccumulator) setMeta(resp responses.Response) {
	if resp.ID != "" {
		a.meta.id = resp.ID
	}
	if resp.Model != "" {
		a.meta.model = string(resp.Model)
	}
	if resp.CreatedAt != 0 {
		a.meta.created = int64(resp.CreatedAt)
	}
}

func (a *streamAccumulator) addTextDelta(delta string) {
	if delta == "" {
		return
	}
	a.textDeltaSeen = true
	a.textDelta.WriteString(delta)
}

func (a *streamAccumulator) setTextDone(text string) {
	if text == "" || a.textDeltaSeen {
		return
	}
	a.textDone += text
}

func (a *streamAccumulator) addOutputItem(item responses.ResponseOutputItemUnion) {
	switch item.Type {
	case "function_call":
		a.addToolCallItem(item)
	case "message":
		a.addMessageItem(item)
	}
}

func (a *streamAccumulator) addMessageItem(item responses.ResponseOutputItemUnion) {
	if a.textDeltaSeen {
		return
	}
	for _, part := range item.Content {
		if part.Type != "output_text" {
			continue
		}
		if part.Text == "" {
			continue
		}
		a.textDone += part.Text
	}
}

func (a *streamAccumulator) addToolCallItem(item responses.ResponseOutputItemUnion) {
	key := item.ID
	if key == "" {
		key = item.CallID
	}
	if key == "" {
		return
	}
	tool := a.ensureToolCall(key)
	if item.ID != "" {
		tool.itemID = item.ID
	}
	if item.CallID != "" {
		tool.callID = item.CallID
		a.toolByCallID[item.CallID] = tool
	}
	if item.Name != "" {
		tool.name = item.Name
	}
	if item.Arguments != "" {
		tool.argsFinal = item.Arguments
	}
}

func (a *streamAccumulator) addToolArgsDelta(itemID, delta string) {
	if itemID == "" || delta == "" {
		return
	}
	tool := a.lookupToolCall(itemID)
	if tool == nil {
		tool = a.ensureToolCall(itemID)
	}
	tool.seenDelta = true
	tool.argsDelta.WriteString(delta)
}

func (a *streamAccumulator) setToolArgsDone(itemID, args string) {
	if itemID == "" {
		return
	}
	tool := a.lookupToolCall(itemID)
	if tool == nil {
		tool = a.ensureToolCall(itemID)
	}
	if args != "" {
		tool.argsFinal = args
	}
}

func (a *streamAccumulator) ensureToolCall(key string) *toolCallAccum {
	if existing := a.toolByItemID[key]; existing != nil {
		return existing
	}
	tool := &toolCallAccum{itemID: key}
	a.toolByItemID[key] = tool
	a.toolOrder = append(a.toolOrder, tool)
	return tool
}

func (a *streamAccumulator) lookupToolCall(key string) *toolCallAccum {
	if key == "" {
		return nil
	}
	if tool := a.toolByItemID[key]; tool != nil {
		return tool
	}
	if tool := a.toolByCallID[key]; tool != nil {
		return tool
	}
	return nil
}

func (a *streamAccumulator) buildChatResponse(fallbackModel string) ChatResponse {
	content := ""
	if a.textDeltaSeen {
		content = a.textDelta.String()
	} else {
		content = a.textDone
	}
	toolCalls := make([]ToolCall, 0, len(a.toolOrder))
	for _, tool := range a.toolOrder {
		if tool == nil {
			continue
		}
		if tool.name == "" || tool.callID == "" {
			continue
		}
		args := tool.argsFinal
		if args == "" && tool.argsDelta.Len() > 0 {
			args = tool.argsDelta.String()
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:   tool.callID,
			Type: "function",
			Function: ToolCallFn{
				Name:      tool.name,
				Arguments: args,
			},
		})
	}
	model := a.meta.model
	if model == "" {
		model = fallbackModel
	}
	msg := Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
	return ChatResponse{
		ID:      a.meta.id,
		Created: a.meta.created,
		Model:   model,
		Choices: []ChatChoice{{Index: 0, Message: msg}},
	}
}
