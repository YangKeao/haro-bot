package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
	"go.uber.org/zap"
)

type openAIChatModel struct {
	baseURL string
	client  *openaisdk.Client
}

type clientOptions struct {
	httpDebug       bool
	httpDebugMaxBod int64
}

type openAIOption func(*clientOptions)

func WithHTTPDebug(enabled bool) openAIOption {
	return func(opts *clientOptions) {
		if opts != nil {
			opts.httpDebug = enabled
		}
	}
}

func WithHTTPDebugMaxBody(maxBytes int64) openAIOption {
	return func(opts *clientOptions) {
		if opts == nil {
			return
		}
		if maxBytes > 0 {
			opts.httpDebugMaxBod = maxBytes
		}
	}
}

func New(baseURL, apiKey string, opts ...openAIOption) llm.ChatModel {
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
	c := openaisdk.NewClient(reqOpts...)
	return &openAIChatModel{
		baseURL: baseURL,
		client:  &c,
	}
}

func (c *openAIChatModel) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	log := logging.L().Named("llm")
	start := time.Now()
	var out llm.ChatResponse
	if c == nil || c.client == nil {
		return out, errors.New("llm client not configured")
	}
	input := buildChatMessages(req.Messages)
	tools := buildChatTools(req.Tools)
	params := openaisdk.ChatCompletionNewParams{
		Model:    openaisdk.ChatModel(req.Model),
		Messages: input,
	}
	if len(tools) > 0 {
		params.Tools = tools
		params.ToolChoice = openaisdk.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: openaisdk.String("auto"),
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
		params.StreamOptions = openaisdk.ChatCompletionStreamOptionsParam{
			IncludeUsage: openaisdk.Bool(true),
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

func extraBodyForPurpose(purpose llm.RequestPurpose) map[string]any {
	switch purpose {
	case llm.PurposeSecurity:
		return map[string]any{
			"thinking": map[string]any{
				"type": "disabled",
			},
		}
	case llm.PurposeMemory:
		return map[string]any{
			"thinking": map[string]any{
				"clear_thinking": false,
			},
		}
	case llm.PurposeChat, "":
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

func normalizeChatCompletionError(err error, resp *openaisdk.ChatCompletion) error {
	if err != nil {
		if isContextWindowError(err) {
			return fmt.Errorf("%w: %v", llm.ErrContextWindowExceeded, err)
		}
		return err
	}
	if finishReason := firstFinishReason(resp); isContextWindowFinishReason(finishReason) {
		return fmt.Errorf("%w: finish_reason=%s", llm.ErrContextWindowExceeded, finishReason)
	}
	return nil
}

func firstFinishReason(resp *openaisdk.ChatCompletion) string {
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
	case strings.Contains(msg, "exceeds") && strings.Contains(msg, "context"):
		return true
	case strings.Contains(msg, "maximum context length"):
		return true
	default:
		return false
	}
}
