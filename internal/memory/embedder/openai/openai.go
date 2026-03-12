package openai

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/memory"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type embedder struct {
	client *openaisdk.Client
	model  string
	mu     sync.Mutex
	dims   int
}

func New(cfg config.MemoryEmbedderConfig) (memory.Embedder, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("memory embedder model required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	apiKey := strings.TrimSpace(cfg.APIKey)
	opts := []option.RequestOption{}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	client := openaisdk.NewClient(opts...)
	return &embedder{client: &client, model: model, dims: cfg.Dimensions}, nil
}

func (e *embedder) Dims() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dims
}

func (e *embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("embedder not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("embedding text required")
	}
	params := openaisdk.EmbeddingNewParams{
		Model: openaisdk.EmbeddingModel(e.model),
		Input: openaisdk.EmbeddingNewParamsInputUnion{OfString: openaisdk.String(text)},
	}
	if e.dims > 0 {
		params.Dimensions = openaisdk.Int(int64(e.dims))
	}
	resp, err := e.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Data) == 0 {
		return nil, errors.New("empty embedding response")
	}
	out := make([]float32, 0, len(resp.Data[0].Embedding))
	for _, v := range resp.Data[0].Embedding {
		out = append(out, float32(v))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dims == 0 {
		e.dims = len(out)
	}
	if e.dims != len(out) {
		return nil, errors.New("embedding dimensions mismatch")
	}
	return out, nil
}
