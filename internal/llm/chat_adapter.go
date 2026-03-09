package llm

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"
)

func buildChatMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if msg.Content == "" {
				continue
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Role: constant.ValueOf[constant.System](),
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
				},
			})
		case "developer":
			if msg.Content == "" {
				continue
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{
				OfDeveloper: &openai.ChatCompletionDeveloperMessageParam{
					Role: constant.ValueOf[constant.Developer](),
					Content: openai.ChatCompletionDeveloperMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
				},
			})
		case "assistant":
			assistant := openai.ChatCompletionAssistantMessageParam{
				Role: constant.ValueOf[constant.Assistant](),
			}
			if msg.Content != "" {
				assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(msg.Content),
				}
			}
			if len(msg.ToolCalls) > 0 {
				assistant.ToolCalls = buildChatToolCalls(msg.ToolCalls)
			}
			// Add reasoning content if present (for models like o1, deepseek-reasoner)
			if msg.ReasoningContent != "" {
				assistant.ReasoningContent = openai.String(msg.ReasoningContent)
			}
			if msg.Content == "" && len(assistant.ToolCalls) == 0 && msg.ReasoningContent == "" {
				continue
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					Role:       constant.ValueOf[constant.Tool](),
					ToolCallID: msg.ToolCallID,
					Content: openai.ChatCompletionToolMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
				},
			})
		default:
			if msg.Content == "" {
				continue
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Role: constant.ValueOf[constant.User](),
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
				},
			})
		}
	}
	return out
}

func buildChatToolCalls(calls []ToolCall) []openai.ChatCompletionMessageToolCallParam {
	out := make([]openai.ChatCompletionMessageToolCallParam, 0, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Function.Name == "" {
			continue
		}
		out = append(out, openai.ChatCompletionMessageToolCallParam{
			ID:   call.ID,
			Type: constant.ValueOf[constant.Function](),
			Function: openai.ChatCompletionMessageToolCallFunctionParam{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func buildChatTools(tools []Tool) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		if t.Type != "function" {
			continue
		}
		params := t.Function.Parameters
		if params == nil {
			params = map[string]any{}
		}
		fn := shared.FunctionDefinitionParam{
			Name:       t.Function.Name,
			Parameters: shared.FunctionParameters(params),
			Strict:     param.NewOpt(false),
		}
		if t.Function.Description != "" {
			fn.Description = param.NewOpt(t.Function.Description)
		}
		out = append(out, openai.ChatCompletionToolParam{
			Type:     constant.ValueOf[constant.Function](),
			Function: fn,
		})
	}
	return out
}

func chatCompletionToChat(resp *openai.ChatCompletion) ChatResponse {
	content := ""
	reasoningContent := ""
	toolCalls := []ToolCall(nil)
	if resp != nil && len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		content = msg.Content
		// Extract reasoning content if present
		reasoningContent = msg.ReasoningContent
		for _, call := range msg.ToolCalls {
			toolCalls = append(toolCalls, ToolCall{
				ID:   call.ID,
				Type: "function",
				Function: ToolCallFn{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}
	}
	msg := Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
	}
	model := ""
	created := int64(0)
	id := ""
	usage := Usage{}
	if resp != nil {
		model = resp.Model
		created = resp.Created
		id = resp.ID
		usage = Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return ChatResponse{
		ID:      id,
		Created: created,
		Model:   model,
		Usage:   usage,
		Choices: []ChatChoice{{Index: 0, Message: msg}},
	}
}
