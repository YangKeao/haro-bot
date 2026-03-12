package openai

import (
	"github.com/YangKeao/haro-bot/internal/llm"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"
)

func buildChatMessages(messages []llm.Message) []openaisdk.ChatCompletionMessageParamUnion {
	out := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if msg.Content == "" {
				continue
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{
				OfSystem: &openaisdk.ChatCompletionSystemMessageParam{
					Role: constant.ValueOf[constant.System](),
					Content: openaisdk.ChatCompletionSystemMessageParamContentUnion{
						OfString: openaisdk.String(msg.Content),
					},
				},
			})
		case "developer":
			if msg.Content == "" {
				continue
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{
				OfDeveloper: &openaisdk.ChatCompletionDeveloperMessageParam{
					Role: constant.ValueOf[constant.Developer](),
					Content: openaisdk.ChatCompletionDeveloperMessageParamContentUnion{
						OfString: openaisdk.String(msg.Content),
					},
				},
			})
		case "assistant":
			assistant := openaisdk.ChatCompletionAssistantMessageParam{
				Role: constant.ValueOf[constant.Assistant](),
			}
			if msg.Content != "" {
				assistant.Content = openaisdk.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openaisdk.String(msg.Content),
				}
			}
			if len(msg.ToolCalls) > 0 {
				assistant.ToolCalls = buildChatToolCalls(msg.ToolCalls)
			}
			// Add reasoning content if present (for models like GLM, DeepSeek)
			if msg.ReasoningContent != "" {
				assistant.SetExtraFields(map[string]any{"reasoning_content": msg.ReasoningContent})
			}
			if msg.Content == "" && len(assistant.ToolCalls) == 0 && msg.ReasoningContent == "" {
				continue
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{
				OfTool: &openaisdk.ChatCompletionToolMessageParam{
					Role:       constant.ValueOf[constant.Tool](),
					ToolCallID: msg.ToolCallID,
					Content: openaisdk.ChatCompletionToolMessageParamContentUnion{
						OfString: openaisdk.String(msg.Content),
					},
				},
			})
		default:
			if msg.Content == "" {
				continue
			}
			out = append(out, openaisdk.ChatCompletionMessageParamUnion{
				OfUser: &openaisdk.ChatCompletionUserMessageParam{
					Role: constant.ValueOf[constant.User](),
					Content: openaisdk.ChatCompletionUserMessageParamContentUnion{
						OfString: openaisdk.String(msg.Content),
					},
				},
			})
		}
	}
	return out
}

func buildChatToolCalls(calls []llm.ToolCall) []openaisdk.ChatCompletionMessageToolCallParam {
	out := make([]openaisdk.ChatCompletionMessageToolCallParam, 0, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Function.Name == "" {
			continue
		}
		out = append(out, openaisdk.ChatCompletionMessageToolCallParam{
			ID:   call.ID,
			Type: constant.ValueOf[constant.Function](),
			Function: openaisdk.ChatCompletionMessageToolCallFunctionParam{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

func buildChatTools(tools []llm.Tool) []openaisdk.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openaisdk.ChatCompletionToolParam, 0, len(tools))
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
		out = append(out, openaisdk.ChatCompletionToolParam{
			Type:     constant.ValueOf[constant.Function](),
			Function: fn,
		})
	}
	return out
}

// chatCompletionToChat converts openai ChatCompletion to our ChatResponse.
// reasoningContent is the accumulated reasoning content from streaming (for GLM/DeepSeek).
func chatCompletionToChat(resp *openaisdk.ChatCompletion, reasoningContent string) llm.ChatResponse {
	content := ""
	toolCalls := []llm.ToolCall(nil)
	if resp != nil && len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		content = msg.Content
		for _, call := range msg.ToolCalls {
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   call.ID,
				Type: "function",
				Function: llm.ToolCallFn{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}
	}
	msg := llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
	}
	model := ""
	created := int64(0)
	id := ""
	usage := llm.Usage{}
	if resp != nil {
		model = resp.Model
		created = resp.Created
		id = resp.ID
		usage = llm.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return llm.ChatResponse{
		ID:      id,
		Created: created,
		Model:   model,
		Usage:   usage,
		Choices: []llm.ChatChoice{{Index: 0, Message: msg}},
	}
}
