package openai

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"go.uber.org/zap"
)

func (c *openAIChatModel) chatResponses(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	log := logging.L().Named("llm")
	start := time.Now()
	params := buildResponsesParams(req)
	var out llm.ChatResponse

	for attempt := 1; attempt <= maxEmptyResponseAttempts; attempt++ {
		log.Debug("responses request",
			zap.String("base_url", c.baseURL),
			zap.String("model", req.Model),
			zap.Int("messages", len(req.Messages)),
			zap.Int("tools", len(params.Tools)),
			zap.Bool("hosted_web_search", req.HostedWebSearch),
			zap.Int("attempt", attempt),
		)
		response, reasoning, err := streamResponse(ctx, c.client, params, req.StreamHandler)
		if err != nil {
			if isContextWindowError(err) {
				return out, fmt.Errorf("%w: %v", llm.ErrContextWindowExceeded, err)
			}
			return out, err
		}
		if err := normalizeResponseStatus(response); err != nil {
			return out, err
		}
		out = responseToChat(response, reasoning)
		if !isEmptyChatResponse(out) {
			log.Debug("responses response",
				zap.Duration("latency", time.Since(start)),
				zap.Int("attempt", attempt),
				zap.String("model", out.Model),
				zap.Int64("prompt_tokens", out.Usage.PromptTokens),
				zap.Int64("completion_tokens", out.Usage.CompletionTokens),
				zap.Int64("total_tokens", out.Usage.TotalTokens),
			)
			return out, nil
		}
		if attempt == maxEmptyResponseAttempts || ctx.Err() != nil {
			return out, errors.New("empty llm response")
		}
		timer := time.NewTimer(time.Duration(attempt) * 200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return out, ctx.Err()
		case <-timer.C:
		}
	}
	return out, errors.New("empty llm response")
}

func buildResponsesParams(req llm.ChatRequest) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(req.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: buildResponsesInput(req.Messages),
		},
		Store: param.NewOpt(false),
		Tools: buildResponsesTools(req.Tools, req.HostedWebSearch),
	}
	if len(params.Tools) > 0 {
		params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
		}
	}
	if req.Temperature != 0 {
		params.Temperature = param.NewOpt(req.Temperature)
	}
	if req.ReasoningEnabled {
		effort := strings.TrimSpace(req.ReasoningEffort)
		if effort == "" {
			effort = string(shared.ReasoningEffortMedium)
		}
		params.Reasoning = shared.ReasoningParam{
			Effort:  shared.ReasoningEffort(effort),
			Summary: shared.ReasoningSummaryAuto,
		}
	}
	if req.HostedWebSearch {
		params.Include = append(params.Include, responses.ResponseIncludable("web_search_call.action.sources"))
	}
	return params
}

func buildResponsesInput(messages []llm.Message) responses.ResponseInputParam {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "tool":
			if message.ToolCallID != "" {
				input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(message.ToolCallID, message.Content))
			}
		case "assistant":
			if message.Content != "" {
				input = append(input, responses.ResponseInputItemParamOfMessage(message.Content, responses.EasyInputMessageRoleAssistant))
			}
			for _, call := range message.ToolCalls {
				if call.ID == "" || call.Function.Name == "" {
					continue
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(
					call.Function.Arguments, call.ID, call.Function.Name,
				))
			}
		case "system", "developer":
			if message.Content != "" {
				input = append(input, responses.ResponseInputItemParamOfMessage(
					message.Content, responses.EasyInputMessageRole(message.Role),
				))
			}
		default:
			content := make(responses.ResponseInputMessageContentListParam, 0, len(message.Images)+1)
			if message.Content != "" {
				content = append(content, responses.ResponseInputContentParamOfInputText(message.Content))
			}
			for _, image := range message.Images {
				if image.URL == "" {
					continue
				}
				part := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
				part.OfInputImage.ImageURL = param.NewOpt(image.URL)
				content = append(content, part)
			}
			if len(content) > 0 {
				input = append(input, responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser))
			}
		}
	}
	return input
}

func buildResponsesTools(tools []llm.Tool, hostedWebSearch bool) []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, 0, len(tools)+1)
	for _, tool := range tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		parameters := tool.Function.Parameters
		if parameters == nil {
			parameters = map[string]any{}
		}
		item := responses.ToolParamOfFunction(tool.Function.Name, parameters, false)
		if tool.Function.Description != "" {
			item.OfFunction.Description = param.NewOpt(tool.Function.Description)
		}
		out = append(out, item)
	}
	if hostedWebSearch {
		out = append(out, responses.ToolParamOfWebSearchPreview(responses.WebSearchToolTypeWebSearchPreview))
	}
	return out
}

func streamResponse(ctx context.Context, client *openaisdk.Client, params responses.ResponseNewParams, handler llm.StreamHandler) (*responses.Response, string, error) {
	if client == nil {
		return nil, "", errors.New("llm client not configured")
	}
	stream := client.Responses.NewStreaming(ctx, params)
	if stream == nil {
		return nil, "", errors.New("responses stream not initialized")
	}
	defer stream.Close()

	var terminal *responses.Response
	var reasoning strings.Builder
	items := make(map[int64]responses.ResponseOutputItemUnion)
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta().Delta
			if delta != "" {
				safeCallStreamHandler(handler, llm.StreamEvent{Delta: delta})
			}
		case "response.reasoning_summary_text.delta":
			delta := event.AsResponseReasoningSummaryTextDelta().Delta
			if delta != "" {
				reasoning.WriteString(delta)
				safeCallStreamHandler(handler, llm.StreamEvent{ReasoningDelta: delta})
			}
		case "response.output_item.done":
			done := event.AsResponseOutputItemDone()
			items[done.OutputIndex] = done.Item
		case "response.completed", "response.failed", "response.incomplete":
			value := event.Response
			terminal = &value
		case "error":
			return nil, reasoning.String(), errors.New(event.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, reasoning.String(), err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, reasoning.String(), ctx.Err()
	}
	if terminal == nil {
		return nil, reasoning.String(), errors.New("responses stream ended without a terminal response")
	}
	if len(terminal.Output) == 0 && len(items) > 0 {
		indexes := make([]int, 0, len(items))
		for index := range items {
			indexes = append(indexes, int(index))
		}
		sort.Ints(indexes)
		terminal.Output = make([]responses.ResponseOutputItemUnion, 0, len(indexes))
		for _, index := range indexes {
			terminal.Output = append(terminal.Output, items[int64(index)])
		}
	}
	return terminal, reasoning.String(), nil
}

func normalizeResponseStatus(response *responses.Response) error {
	if response == nil {
		return errors.New("empty Responses API response")
	}
	if response.Error.Message != "" {
		err := errors.New(response.Error.Message)
		if isContextWindowError(err) {
			return fmt.Errorf("%w: %v", llm.ErrContextWindowExceeded, err)
		}
		return err
	}
	switch response.Status {
	case "completed":
		return nil
	case "incomplete":
		reason := strings.TrimSpace(response.IncompleteDetails.Reason)
		if reason == "" {
			reason = "unknown reason"
		}
		return fmt.Errorf("response incomplete: %s", reason)
	case "failed", "cancelled", "canceled":
		return fmt.Errorf("response %s", response.Status)
	default:
		return fmt.Errorf("unexpected response status %q", response.Status)
	}
}

func responseToChat(response *responses.Response, reasoningContent string) llm.ChatResponse {
	message := llm.Message{Role: "assistant", ReasoningContent: reasoningContent}
	if response != nil {
		message.Content = response.OutputText()
		for _, item := range response.Output {
			switch item.Type {
			case "function_call":
				call := item.AsFunctionCall()
				message.ToolCalls = append(message.ToolCalls, llm.ToolCall{
					ID: call.CallID, Type: "function",
					Function: llm.ToolCallFn{Name: call.Name, Arguments: call.Arguments},
				})
			case "reasoning":
				if message.ReasoningContent != "" {
					continue
				}
				parts := item.AsReasoning().Summary
				for _, part := range parts {
					message.ReasoningContent += part.Text
				}
			}
		}
	}
	result := llm.ChatResponse{Choices: []llm.ChatChoice{{Index: 0, Message: message}}}
	if response != nil {
		result.ID = response.ID
		result.Created = int64(response.CreatedAt)
		result.Model = string(response.Model)
		result.Usage = llm.Usage{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.TotalTokens,
		}
	}
	return result
}
