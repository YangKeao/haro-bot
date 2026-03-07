package llm

import (
	"context"
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
