package llm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/openai/openai-go"
)

func streamChatCompletion(ctx context.Context, client *openai.Client, params openai.ChatCompletionNewParams, handler StreamHandler) (*openai.ChatCompletion, error) {
	if client == nil {
		return nil, errors.New("llm client not configured")
	}
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	if stream == nil {
		return nil, errors.New("llm stream not initialized")
	}
	defer stream.Close()
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if handler != nil && len(chunk.Choices) > 0 {
			for _, choice := range chunk.Choices {
				// Handle reasoning content from ExtraFields (for models like GLM, DeepSeek)
				if field, ok := choice.Delta.JSON.ExtraFields["reasoning_content"]; ok && field.Valid() {
					raw := field.Raw()
					if raw != "" {
						var reasoningContent string
						// Raw returns the JSON-encoded value, need to unmarshal it
						if err := json.Unmarshal([]byte(raw), &reasoningContent); err == nil {
							safeCallStreamHandler(handler, StreamEvent{ReasoningDelta: reasoningContent})
						}
					}
				}
				// Handle regular content
				if choice.Delta.Content != "" {
					safeCallStreamHandler(handler, StreamEvent{Delta: choice.Delta.Content})
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
	return &acc.ChatCompletion, nil
}

func safeCallStreamHandler(handler StreamHandler, event StreamEvent) {
	defer func() {
		_ = recover()
	}()
	handler(event)
}
