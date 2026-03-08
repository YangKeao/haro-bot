package memory

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dims() int
}

type OpenAIEmbedder struct {
	client *openai.Client
	model  string
	mu     sync.Mutex
	dims   int
}

func NewOpenAIEmbedder(cfg config.MemoryEmbedderConfig) (*OpenAIEmbedder, error) {
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
	client := openai.NewClient(opts...)
	return &OpenAIEmbedder{client: &client, model: model, dims: cfg.Dimensions}, nil
}

func (e *OpenAIEmbedder) Dims() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dims
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("embedder not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("embedding text required")
	}
	params := openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(e.model),
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
	}
	if e.dims > 0 {
		params.Dimensions = openai.Int(int64(e.dims))
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
