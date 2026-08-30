package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
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
				item := responses.ResponseInputItemParamOfFunctionCallOutput(message.Content)
				item.OfFunctionCallOutput.CallID = param.NewOpt(message.ToolCallID)
				input = append(input, item)
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
		out = append(out, responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch))
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
	callIDs := make(map[string]string)
	toolNames := make(map[string]string)
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta().Delta
			if delta != "" {
				safeCallStreamHandler(handler, llm.StreamEvent{Delta: delta})
			}
		case "response.reasoning_summary_text.delta":
			value := event.AsResponseReasoningSummaryTextDelta()
			delta := value.Delta
			if delta != "" {
				reasoning.WriteString(delta)
				safeCallStreamHandler(handler, llm.StreamEvent{
					ReasoningDelta: delta,
					Trace: &llm.TraceEvent{Phase: "delta", Sequence: value.SequenceNumber, Delta: delta, Step: llm.TraceStep{
						ID: value.ItemID, Kind: "reasoning", Status: "running", Order: value.OutputIndex,
					}},
				})
			}
		case "response.reasoning_summary_text.done":
			value := event.AsResponseReasoningSummaryTextDone()
			safeCallStreamHandler(handler, llm.StreamEvent{Trace: &llm.TraceEvent{
				Phase: "completed", Sequence: value.SequenceNumber,
				Step: llm.TraceStep{ID: value.ItemID, Kind: "reasoning", Status: "completed", Content: value.Text, Order: value.OutputIndex},
			}})
		case "response.output_item.added":
			added := event.AsResponseOutputItemAdded()
			if added.Item.Type == "function_call" {
				call := added.Item.AsFunctionCall()
				id := traceStepID(call.CallID, call.ID)
				callIDs[call.ID] = id
				toolNames[call.ID] = call.Name
				safeCallStreamHandler(handler, llm.StreamEvent{Trace: &llm.TraceEvent{
					Phase: "started", Sequence: added.SequenceNumber,
					Step: llm.TraceStep{ID: id, Kind: "tool", ToolKind: "function", Name: call.Name, Status: "preparing", Arguments: call.Arguments, Order: added.OutputIndex},
				}})
			}
		case "response.function_call_arguments.delta":
			value := event.AsResponseFunctionCallArgumentsDelta()
			id := traceStepID(callIDs[value.ItemID], value.ItemID)
			safeCallStreamHandler(handler, llm.StreamEvent{Trace: &llm.TraceEvent{
				Phase: "arguments.delta", Sequence: value.SequenceNumber, Delta: value.Delta,
				Step: llm.TraceStep{ID: id, Kind: "tool", ToolKind: "function", Name: toolNames[value.ItemID], Status: "preparing", Order: value.OutputIndex},
			}})
		case "response.function_call_arguments.done":
			value := event.AsResponseFunctionCallArgumentsDone()
			id := traceStepID(callIDs[value.ItemID], value.ItemID)
			name := toolNames[value.ItemID]
			safeCallStreamHandler(handler, llm.StreamEvent{Trace: &llm.TraceEvent{
				Phase: "updated", Sequence: value.SequenceNumber,
				Step: llm.TraceStep{ID: id, Kind: "tool", ToolKind: "function", Name: name, Status: "preparing", Arguments: value.Arguments, Order: value.OutputIndex},
			}})
		case "response.web_search_call.in_progress":
			value := event.AsResponseWebSearchCallInProgress()
			safeCallStreamHandler(handler, hostedToolStreamEvent("started", value.SequenceNumber, value.OutputIndex, value.ItemID, "running", nil))
		case "response.web_search_call.searching":
			value := event.AsResponseWebSearchCallSearching()
			safeCallStreamHandler(handler, hostedToolStreamEvent("updated", value.SequenceNumber, value.OutputIndex, value.ItemID, "searching", nil))
		case "response.web_search_call.completed":
			value := event.AsResponseWebSearchCallCompleted()
			// The following output_item.done event carries the action and sources.
			// Keep this as an update so clients receive one terminal event with the
			// complete hosted-tool detail instead of two completion events.
			safeCallStreamHandler(handler, hostedToolStreamEvent("updated", value.SequenceNumber, value.OutputIndex, value.ItemID, "completed", nil))
		case "response.output_item.done":
			done := event.AsResponseOutputItemDone()
			items[done.OutputIndex] = done.Item
			switch done.Item.Type {
			case "function_call":
				call := done.Item.AsFunctionCall()
				id := traceStepID(call.CallID, call.ID)
				callIDs[call.ID] = id
				safeCallStreamHandler(handler, llm.StreamEvent{Trace: &llm.TraceEvent{
					Phase: "updated", Sequence: done.SequenceNumber,
					Step: llm.TraceStep{ID: id, Kind: "tool", ToolKind: "function", Name: call.Name, Status: "preparing", Arguments: call.Arguments, Order: done.OutputIndex},
				}})
			case "web_search_call":
				search := done.Item.AsWebSearchCall()
				safeCallStreamHandler(handler, hostedToolStreamEvent("completed", done.SequenceNumber, done.OutputIndex, search.ID, string(search.Status), responseItemDetail(done.Item)))
			}
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

func hostedToolStreamEvent(phase string, sequence, order int64, id, status string, detail any) llm.StreamEvent {
	return llm.StreamEvent{Trace: &llm.TraceEvent{
		Phase: phase, Sequence: sequence,
		Step: llm.TraceStep{ID: id, Kind: "tool", ToolKind: "hosted", Name: "web_search", Status: status, Detail: detail, Order: order},
	}}
}

func traceStepID(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

func responseItemDetail(item responses.ResponseOutputItemUnion) any {
	raw := item.RawJSON()
	if raw == "" || !json.Valid([]byte(raw)) {
		return nil
	}
	return json.RawMessage(raw)
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
	message := llm.Message{Role: "assistant"}
	var orderedReasoning strings.Builder
	if response != nil {
		message.Content = response.OutputText()
		for index, item := range response.Output {
			switch item.Type {
			case "function_call":
				call := item.AsFunctionCall()
				message.ToolCalls = append(message.ToolCalls, llm.ToolCall{
					ID: call.CallID, Type: "function",
					Function: llm.ToolCallFn{Name: call.Name, Arguments: call.Arguments},
				})
				message.TraceSteps = append(message.TraceSteps, llm.TraceStep{
					ID: traceStepID(call.CallID, call.ID), Kind: "tool", ToolKind: "function", Name: call.Name,
					Status: "preparing", Arguments: call.Arguments, Order: int64(index), Detail: responseItemDetail(item),
				})
			case "reasoning":
				var content strings.Builder
				parts := item.AsReasoning().Summary
				for _, part := range parts {
					content.WriteString(part.Text)
				}
				text := content.String()
				if text == "" {
					continue
				}
				orderedReasoning.WriteString(text)
				message.TraceSteps = append(message.TraceSteps, llm.TraceStep{
					ID: traceStepID(item.ID, fmt.Sprintf("reasoning-%d", index)), Kind: "reasoning", Status: "completed",
					Content: text, Order: int64(index),
				})
			case "web_search_call":
				search := item.AsWebSearchCall()
				message.TraceSteps = append(message.TraceSteps, llm.TraceStep{
					ID: traceStepID(search.ID, fmt.Sprintf("web-search-%d", index)), Kind: "tool", ToolKind: "hosted", Name: "web_search",
					Status: string(search.Status), Order: int64(index), Detail: responseItemDetail(item),
				})
			}
		}
	}
	message.ReasoningContent = orderedReasoning.String()
	if message.ReasoningContent == "" {
		message.ReasoningContent = reasoningContent
		if reasoningContent != "" {
			message.TraceSteps = append([]llm.TraceStep{{ID: "reasoning", Kind: "reasoning", Status: "completed", Content: reasoningContent}}, message.TraceSteps...)
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
