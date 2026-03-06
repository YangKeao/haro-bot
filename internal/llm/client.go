package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"
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
	httpClient := &http.Client{}
	if options.httpDebug {
		httpClient.Transport = newDebugTransport(http.DefaultTransport, options.httpDebugMaxBod)
	}
	reqOpts := []option.RequestOption{option.WithHTTPClient(httpClient)}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	c := openai.NewClient(reqOpts...)
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
	if ctx != nil {
		ctx = context.WithoutCancel(ctx)
	}
	input := buildChatMessages(req.Messages)
	tools := buildChatTools(req.Tools)
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(req.Model),
		Messages: input,
	}
	if len(tools) > 0 {
		params.Tools = tools
		params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: openai.String("auto"),
		}
	}
	if req.Temperature != 0 {
		params.Temperature = param.NewOpt(req.Temperature)
	}
	if req.ReasoningEnabled {
		if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
			params.ReasoningEffort = shared.ReasoningEffort(effort)
		} else {
			params.ReasoningEffort = shared.ReasoningEffortMedium
		}
	}

	forceStream := true
	log.Debug("chat completions request",
		zap.String("base_url", c.baseURL),
		zap.String("model", req.Model),
		zap.Int("messages", len(req.Messages)),
		zap.Int("input_items", len(input)),
		zap.Int("tools", len(tools)),
		zap.Bool("stream", forceStream),
	)

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	if stream == nil {
		return out, errors.New("llm stream not initialized")
	}
	defer stream.Close()
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if ok := acc.AddChunk(chunk); !ok {
			return out, errors.New("failed to accumulate stream chunk")
		}
	}
	if err := stream.Err(); err != nil {
		if isContextWindowError(err) {
			return out, fmt.Errorf("%w: %v", ErrContextWindowExceeded, err)
		}
		log.Error("chat completions stream error", zap.Duration("latency", time.Since(start)), zap.Error(err))
		return out, err
	}
	if finishReason := firstFinishReason(&acc.ChatCompletion); isContextWindowFinishReason(finishReason) {
		return out, fmt.Errorf("%w: finish_reason=%s", ErrContextWindowExceeded, finishReason)
	}
	out = chatCompletionToChat(&acc.ChatCompletion)
	if len(out.Choices) == 0 || (out.Choices[0].Message.Content == "" && len(out.Choices[0].Message.ToolCalls) == 0) {
		return out, errors.New("empty llm response")
	}

	log.Debug("chat completions response",
		zap.Duration("latency", time.Since(start)),
		zap.Int("choices", len(out.Choices)),
		zap.String("model", out.Model),
	)
	return out, nil
}

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
			if msg.Content == "" && len(assistant.ToolCalls) == 0 {
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
	toolCalls := []ToolCall(nil)
	if resp != nil && len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		content = msg.Content
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
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
	model := ""
	created := int64(0)
	id := ""
	if resp != nil {
		model = resp.Model
		created = resp.Created
		id = resp.ID
	}
	return ChatResponse{
		ID:      id,
		Created: created,
		Model:   model,
		Choices: []ChatChoice{{Index: 0, Message: msg}},
	}
}

func firstFinishReason(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].FinishReason)
}

func isContextWindowFinishReason(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	if strings.Contains(reason, "context_window") {
		return true
	}
	if strings.Contains(reason, "context") && strings.Contains(reason, "exceed") {
		return true
	}
	if strings.Contains(reason, "model_context") {
		return true
	}
	return false
}

func isContextWindowError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context_length_exceeded"):
		return true
	case strings.Contains(msg, "context window"):
		return true
	case strings.Contains(msg, "context_window"):
		return true
	case strings.Contains(msg, "model_context_window_exceeded"):
		return true
	default:
		return false
	}
}
