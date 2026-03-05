package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	log := logging.L().Named("llm")
	start := time.Now()
	var out ChatResponse
	body, err := json.Marshal(req)
	if err != nil {
		log.Error("chat marshal failed", zap.Error(err))
		return out, err
	}
	url := c.baseURL + "/chat/completions"
	log.Debug("chat request",
		zap.String("url", url),
		zap.String("model", req.Model),
		zap.Int("messages", len(req.Messages)),
		zap.Int("tools", len(req.Tools)),
		zap.Any("body", req),
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Error("chat request build failed", zap.Error(err))
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		log.Error("chat request failed", zap.Duration("latency", time.Since(start)), zap.Error(err))
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		log.Warn("chat non-2xx",
			zap.Int("status", resp.StatusCode),
			zap.Duration("latency", time.Since(start)),
		)
		return out, fmt.Errorf("llm error: %s", string(b))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Error("chat decode failed", zap.Error(err))
		return out, err
	}
	log.Debug("chat response",
		zap.Int("status", resp.StatusCode),
		zap.Duration("latency", time.Since(start)),
		zap.Int("choices", len(out.Choices)),
		zap.String("model", out.Model),
		zap.Any("out", out),
	)
	return out, nil
}
