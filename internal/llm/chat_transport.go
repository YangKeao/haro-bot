package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/openai/openai-go"
	"go.uber.org/zap"
)

var transportLog = zap.L().Named("chat_transport")

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
	var streamErr error
	var mu sync.Mutex

	// Read stream in a goroutine to allow context cancellation
	streamChan := make(chan openai.ChatCompletionChunk, 100)
	go func() {
		defer close(streamChan)
		for stream.Next() {
			chunk := stream.Current()
			select {
			case streamChan <- chunk:
			case <-ctx.Done():
				return
			}
		}
		mu.Lock()
		streamErr = stream.Err()
		mu.Unlock()
	}()

	// Process chunks with context cancellation support
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-streamChan:
			if !ok {
				// Stream finished
				mu.Lock()
				err := streamErr
				mu.Unlock()
				if err != nil {
					return nil, err
				}
				return &acc.ChatCompletion, nil
			}

			if handler != nil && len(chunk.Choices) > 0 {
				for _, choice := range chunk.Choices {
					// Debug: log all ExtraFields
					if len(choice.Delta.JSON.ExtraFields) > 0 {
						transportLog.Debug("chunk has ExtraFields", 
							zap.Int("count", len(choice.Delta.JSON.ExtraFields)),
							zap.String("raw", choice.Delta.RawJSON()))
						for key, field := range choice.Delta.JSON.ExtraFields {
							transportLog.Debug("ExtraField", 
								zap.String("key", key), 
								zap.Bool("valid", field.Valid()),
								zap.String("raw", field.Raw()))
						}
					}
					
					// Handle reasoning content from ExtraFields (for models like GLM, DeepSeek)
					if field, ok := choice.Delta.JSON.ExtraFields["reasoning_content"]; ok && field.Valid() {
						raw := field.Raw()
						if raw != "" {
							var reasoningContent string
							// Raw returns the JSON-encoded value, need to unmarshal it
							if err := json.Unmarshal([]byte(raw), &reasoningContent); err == nil {
								transportLog.Debug("extracted reasoning_content", zap.String("content", reasoningContent))
								safeCallStreamHandler(handler, StreamEvent{ReasoningDelta: reasoningContent})
							} else {
								transportLog.Debug("failed to unmarshal reasoning_content", zap.Error(err))
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
	}
}

func safeCallStreamHandler(handler StreamHandler, event StreamEvent) {
	defer func() {
		_ = recover()
	}()
	handler(event)
}
