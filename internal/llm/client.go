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
	"github.com/openai/openai-go/shared"
	"go.uber.org/zap"
)

type OpenAIChatModel struct {
	baseURL string
	apiKey  string
	http    *http.Client
	client  *openai.Client
}

// Client is kept as a compatibility alias for the default OpenAI-compatible provider.
type Client = OpenAIChatModel

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

func NewOpenAIChatModel(baseURL, apiKey string, opts ...ClientOption) *OpenAIChatModel {
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
	return &OpenAIChatModel{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    httpClient,
		client:  &c,
	}
}

func NewClient(baseURL, apiKey string, opts ...ClientOption) *Client {
	return NewOpenAIChatModel(baseURL, apiKey, opts...)
}

func (c *OpenAIChatModel) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	log := logging.L().Named("llm")
	start := time.Now()
	var out ChatResponse
	if c == nil || c.client == nil {
		return out, errors.New("llm client not configured")
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
	if extra := extraBodyForPurpose(req.Purpose); len(extra) > 0 {
		params.SetExtraFields(extra)
	}

	forceStream := true
	if forceStream {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}
	log.Debug("chat completions request",
		zap.String("base_url", c.baseURL),
		zap.String("model", req.Model),
		zap.Int("messages", len(req.Messages)),
		zap.Int("input_items", len(input)),
		zap.Int("tools", len(tools)),
		zap.Bool("stream", forceStream),
	)

	result, err := streamChatCompletion(ctx, c.client, params, req.StreamHandler)
	if err != nil {
		if norm := normalizeChatCompletionError(err, nil); norm != nil {
			return out, norm
		}
		log.Error("chat completions stream error", zap.Duration("latency", time.Since(start)), zap.Error(err))
		return out, err
	}
	if norm := normalizeChatCompletionError(nil, result.completion); norm != nil {
		return out, norm
	}
	out = chatCompletionToChat(result.completion, result.reasoningContent)
	if len(out.Choices) == 0 || (out.Choices[0].Message.Content == "" && len(out.Choices[0].Message.ToolCalls) == 0) {
		return out, errors.New("empty llm response")
	}

	log.Debug("chat completions response",
		zap.Duration("latency", time.Since(start)),
		zap.Int("choices", len(out.Choices)),
		zap.String("model", out.Model),
		zap.Int64("prompt_tokens", out.Usage.PromptTokens),
		zap.Int64("completion_tokens", out.Usage.CompletionTokens),
		zap.Int64("total_tokens", out.Usage.TotalTokens),
	)
	return out, nil
}
func extraBodyForPurpose(purpose RequestPurpose) map[string]any {
	switch purpose {
	case PurposeSecurity:
		return map[string]any{
			"thinking": map[string]any{
				"type": "disabled",
			},
		}
	case PurposeMemory:
		return map[string]any{
			"thinking": map[string]any{
				"clear_thinking": false,
			},
		}
	case PurposeChat, "":
		return map[string]any{
			"thinking": map[string]any{
				"clear_thinking": false,
			},
		}
	default:
		return map[string]any{
			"thinking": map[string]any{
				"clear_thinking": false,
			},
		}
	}
}
