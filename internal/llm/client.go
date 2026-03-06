package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"go.uber.org/zap"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	client  *openai.Client
}

type clientOptions struct {
	httpDebug       bool
	httpDebugMaxBod int64
}

type ClientOption func(*clientOptions)

func WithHTTPDebug(enabled bool) ClientOption {
	return func(opts *clientOptions) {
		if opts != nil {
			opts.httpDebug = enabled
		}
	}
}

func WithHTTPDebugMaxBody(maxBytes int64) ClientOption {
	return func(opts *clientOptions) {
		if opts == nil {
			return
		}
		if maxBytes > 0 {
			opts.httpDebugMaxBod = maxBytes
		}
	}
}

func NewClient(baseURL, apiKey string, opts ...ClientOption) *Client {
	options := clientOptions{httpDebugMaxBod: defaultHTTPDebugMaxBody}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	httpClient := &http.Client{Timeout: 60 * time.Second}
	if options.httpDebug {
		httpClient.Transport = newDebugTransport(http.DefaultTransport, options.httpDebugMaxBod)
	}
	opts := []option.RequestOption{option.WithHTTPClient(httpClient)}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	c := openai.NewClient(opts...)
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    httpClient,
		client:  &c,
	}
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	log := logging.L().Named("llm")
	start := time.Now()
	var out ChatResponse
	if c == nil || c.client == nil {
		return out, errors.New("llm client not configured")
	}
	input := buildResponseInput(req.Messages)
	tools := buildResponseTools(req.Tools)
	params := responses.ResponseNewParams{
		Model: openai.ResponsesModel(req.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam(input),
		},
	}
	if len(tools) > 0 {
		params.Tools = tools
		params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
		}
	}
	if req.Temperature != 0 {
		params.Temperature = param.NewOpt(req.Temperature)
	}
	if req.ReasoningEnabled {
		reasoning := shared.ReasoningParam{}
		if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
			reasoning.Effort = shared.ReasoningEffort(effort)
		} else {
			reasoning.Effort = shared.ReasoningEffortMedium
		}
		params.Reasoning = reasoning
	}

	log.Debug("responses request",
		zap.String("base_url", c.baseURL),
		zap.String("model", req.Model),
		zap.Int("messages", len(req.Messages)),
		zap.Int("input_items", len(input)),
		zap.Int("tools", len(tools)),
		zap.Bool("stream", req.Stream),
	)

	if req.Stream {
		stream := c.client.Responses.NewStreaming(ctx, params)
		if stream == nil {
			return out, errors.New("llm stream not initialized")
		}
		defer stream.Close()
		var final *responses.Response
		for stream.Next() {
			event := stream.Current()
			switch ev := event.AsAny().(type) {
			case responses.ResponseCompletedEvent:
				final = &ev.Response
			case responses.ResponseFailedEvent:
				msg := ev.Response.Error.Message
				if msg == "" {
					msg = "response failed"
				}
				return out, errors.New(msg)
			case responses.ResponseErrorEvent:
				if ev.Message != "" {
					return out, errors.New(ev.Message)
				}
				return out, errors.New("response error")
			}
		}
		if err := stream.Err(); err != nil {
			log.Error("responses stream error", zap.Duration("latency", time.Since(start)), zap.Error(err))
			return out, err
		}
		if final == nil {
			return out, errors.New("empty llm response")
		}
		out = responseToChat(final)
	} else {
		resp, err := c.client.Responses.New(ctx, params)
		if err != nil {
			log.Error("responses request failed", zap.Duration("latency", time.Since(start)), zap.Error(err))
			return out, err
		}
		out = responseToChat(resp)
	}

	log.Debug("responses response",
		zap.Duration("latency", time.Since(start)),
		zap.Int("choices", len(out.Choices)),
		zap.String("model", out.Model),
	)
	return out, nil
}

func buildResponseInput(messages []Message) []responses.ResponseInputItemUnionParam {
	out := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			out = append(out, responses.ResponseInputItemParamOfFunctionCallOutput(msg.ToolCallID, msg.Content))
			continue
		case "assistant":
			if msg.Content != "" {
				out = append(out, responses.ResponseInputItemParamOfMessage(msg.Content, mapRole(msg.Role)))
			}
			if len(msg.ToolCalls) > 0 {
				for _, call := range msg.ToolCalls {
					if call.ID == "" {
						continue
					}
					out = append(out, responses.ResponseInputItemParamOfFunctionCall(
						call.Function.Arguments,
						call.ID,
						call.Function.Name,
					))
				}
			}
			continue
		default:
			if msg.Content == "" {
				continue
			}
			out = append(out, responses.ResponseInputItemParamOfMessage(msg.Content, mapRole(msg.Role)))
		}
	}
	return out
}

func buildResponseTools(tools []Tool) []responses.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.Type != "function" {
			continue
		}
		params := t.Function.Parameters
		if params == nil {
			params = map[string]any{}
		}
		fn := responses.FunctionToolParam{
			Name:       t.Function.Name,
			Parameters: params,
			Strict:     param.NewOpt(false),
		}
		if t.Function.Description != "" {
			fn.Description = param.NewOpt(t.Function.Description)
		}
		out = append(out, responses.ToolUnionParam{OfFunction: &fn})
	}
	return out
}

func mapRole(role string) responses.EasyInputMessageRole {
	switch role {
	case "system":
		return responses.EasyInputMessageRoleSystem
	case "assistant":
		return responses.EasyInputMessageRoleAssistant
	case "developer":
		return responses.EasyInputMessageRoleDeveloper
	default:
		return responses.EasyInputMessageRoleUser
	}
}

func responseToChat(resp *responses.Response) ChatResponse {
	content := ""
	toolCalls := []ToolCall(nil)
	if resp != nil {
		content = resp.OutputText()
		for _, item := range resp.Output {
			if item.Type != "function_call" {
				continue
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ToolCallFn{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}
	msg := Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
	model := ""
	created := int64(0)
	id := ""
	if resp != nil {
		model = string(resp.Model)
		created = int64(resp.CreatedAt)
		id = resp.ID
	}
	return ChatResponse{
		ID:      id,
		Created: created,
		Model:   model,
		Choices: []ChatChoice{{Index: 0, Message: msg}},
	}
}
