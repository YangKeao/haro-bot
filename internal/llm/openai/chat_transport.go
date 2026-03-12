package openai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/YangKeao/haro-bot/internal/llm"
	openaisdk "github.com/openai/openai-go"
)

// streamResult holds the result of streaming completion
type streamResult struct {
	completion       *openaisdk.ChatCompletion
	reasoningContent string // Accumulated reasoning content (for GLM/DeepSeek)
}

func streamChatCompletion(ctx context.Context, client *openaisdk.Client, params openaisdk.ChatCompletionNewParams, handler llm.StreamHandler) (*streamResult, error) {
	if client == nil {
		return nil, errors.New("llm client not configured")
	}
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	if stream == nil {
		return nil, errors.New("llm stream not initialized")
	}
	defer stream.Close()

	var acc openaisdk.ChatCompletionAccumulator

	// Accumulate reasoning content separately since openai-go's Accumulator doesn't handle ExtraFields
	var reasoningBuilder strings.Builder

	for stream.Next() {
		chunk := stream.Current()
		if handler != nil && len(chunk.Choices) > 0 {
			for _, choice := range chunk.Choices {
				// Handle reasoning content from ExtraFields (for models like GLM, DeepSeek)
				// Note: field.Valid() may return false for unknown fields, but Raw() still has the value
				if field, ok := choice.Delta.JSON.ExtraFields["reasoning_content"]; ok {
					raw := field.Raw()
					if raw != "" {
						var reasoningContent string
						// Raw returns the JSON-encoded value, need to unmarshal it
						if err := json.Unmarshal([]byte(raw), &reasoningContent); err == nil {
							// Accumulate reasoning content
							reasoningBuilder.WriteString(reasoningContent)
							// Stream to handler
							safeCallStreamHandler(handler, llm.StreamEvent{ReasoningDelta: reasoningContent})
						}
					}
				}
				// Handle regular content
				if choice.Delta.Content != "" {
					safeCallStreamHandler(handler, llm.StreamEvent{Delta: choice.Delta.Content})
				}
			}
		}
		if ok := acc.AddChunk(chunk); !ok {
			return nil, errors.New("failed to accumulate stream chunk")
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &streamResult{
		completion:       &acc.ChatCompletion,
		reasoningContent: reasoningBuilder.String(),
	}, nil
}

func safeCallStreamHandler(handler llm.StreamHandler, event llm.StreamEvent) {
	defer func() {
		_ = recover()
	}()
	handler(event)
}
