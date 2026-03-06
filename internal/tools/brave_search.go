package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

const (
	defaultBraveSearchEndpoint       = "https://api.search.brave.com/res/v1/web/search"
	defaultBraveSearchTimeout        = 20 * time.Second
	defaultBraveSearchMaxResults     = 20
	defaultBraveSearchMaxOffset      = 9
	defaultBraveSearchMaxOutputBytes = 1 << 20
)

type BraveSearchTool struct {
	apiKey         string
	endpoint       string
	client         *http.Client
	maxOutputBytes int64
	mu             sync.Mutex
	nextAllowed    time.Time
}

type BraveSearchOption func(*BraveSearchTool)

func WithBraveSearchEndpoint(endpoint string) BraveSearchOption {
	return func(t *BraveSearchTool) {
		if endpoint != "" {
			t.endpoint = endpoint
		}
	}
}

func WithBraveSearchHTTPClient(client *http.Client) BraveSearchOption {
	return func(t *BraveSearchTool) {
		if client != nil {
			t.client = client
		}
	}
}

func WithBraveSearchMaxOutputBytes(n int64) BraveSearchOption {
	return func(t *BraveSearchTool) {
		if n > 0 {
			t.maxOutputBytes = n
		}
	}
}

func NewBraveSearchTool(apiKey string, opts ...BraveSearchOption) *BraveSearchTool {
	t := &BraveSearchTool{
		apiKey:         strings.TrimSpace(apiKey),
		endpoint:       defaultBraveSearchEndpoint,
		client:         &http.Client{Timeout: defaultBraveSearchTimeout},
		maxOutputBytes: defaultBraveSearchMaxOutputBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}
	return t
}

func (t *BraveSearchTool) Name() string { return "brave_search" }

func (t *BraveSearchTool) Description() string {
	return "Search the web using Brave Search. Requires BRAVE_SEARCH_API_KEY. Returns a compact JSON of query and web results."
}

func (t *BraveSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Results per page (1-20). Default is 20.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Page offset (0-9).",
			},
			"country": map[string]any{
				"type":        "string",
				"description": "Country code (ISO 3166-1 alpha-2).",
			},
			"search_lang": map[string]any{
				"type":        "string",
				"description": "Search language (ISO 639-1).",
			},
			"ui_lang": map[string]any{
				"type":        "string",
				"description": "UI language (for example en-US).",
			},
			"freshness": map[string]any{
				"type":        "string",
				"description": "Time filter (pd/pw/pm/py or YYYY-MM-DDtoYYYY-MM-DD).",
			},
			"summary": map[string]any{
				"type":        "boolean",
				"description": "Request a summarizer key when available.",
			},
			"enable_rich_callback": map[string]any{
				"type":        "boolean",
				"description": "Request a rich data callback key when available.",
			},
		},
		"required": []string{"query"},
	}
}

type braveSearchArgs struct {
	Query              string `json:"query"`
	Q                  string `json:"q"`
	Count              int    `json:"count"`
	Offset             int    `json:"offset"`
	Country            string `json:"country"`
	SearchLang         string `json:"search_lang"`
	UILang             string `json:"ui_lang"`
	Freshness          string `json:"freshness"`
	Summary            bool   `json:"summary"`
	EnableRichCallback bool   `json:"enable_rich_callback"`
}

type braveQuery struct {
	Original             string `json:"original,omitempty"`
	MoreResultsAvailable bool   `json:"more_results_available,omitempty"`
}

type braveWebResult struct {
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	Profile     *struct {
		Name     string `json:"name,omitempty"`
		LongName string `json:"long_name,omitempty"`
		URL      string `json:"url,omitempty"`
	} `json:"profile,omitempty"`
}

type braveSummarizer struct {
	Key  string `json:"key,omitempty"`
	Type string `json:"type,omitempty"`
}

type braveWebResponse struct {
	Query braveQuery `json:"query,omitempty"`
	Web   struct {
		Results []braveWebResult `json:"results,omitempty"`
	} `json:"web,omitempty"`
	Summarizer *braveSummarizer `json:"summarizer,omitempty"`
	Rich       json.RawMessage  `json:"rich,omitempty"`
}

type braveSearchOutput struct {
	Query      braveQuery       `json:"query,omitempty"`
	Results    []braveWebResult `json:"results,omitempty"`
	Summarizer *braveSummarizer `json:"summarizer,omitempty"`
	Rich       json.RawMessage  `json:"rich,omitempty"`
}

func (t *BraveSearchTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	var a braveSearchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	query := strings.TrimSpace(a.Query)
	if query == "" {
		query = strings.TrimSpace(a.Q)
	}
	if query == "" {
		return "", errors.New("query required")
	}
	if strings.TrimSpace(t.apiKey) == "" {
		return "", errors.New("brave search api key not configured")
	}
	count := a.Count
	if count == 0 {
		count = defaultBraveSearchMaxResults
	}
	if count < 1 || count > defaultBraveSearchMaxResults {
		return "", fmt.Errorf("count must be between 1 and %d", defaultBraveSearchMaxResults)
	}
	if a.Offset < 0 || a.Offset > defaultBraveSearchMaxOffset {
		return "", fmt.Errorf("offset must be between 0 and %d", defaultBraveSearchMaxOffset)
	}
	endpoint := strings.TrimSpace(t.endpoint)
	if endpoint == "" {
		return "", errors.New("brave search endpoint not configured")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("count", strconv.Itoa(count))
	q.Set("offset", strconv.Itoa(a.Offset))
	if a.Country != "" {
		q.Set("country", a.Country)
	}
	if a.SearchLang != "" {
		q.Set("search_lang", a.SearchLang)
	}
	if a.UILang != "" {
		q.Set("ui_lang", a.UILang)
	}
	if a.Freshness != "" {
		q.Set("freshness", a.Freshness)
	}
	if a.Summary {
		q.Set("summary", "1")
	}
	if a.EnableRichCallback {
		q.Set("enable_rich_callback", "1")
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	log := logging.L().Named("brave_search")
	attempt := 0
	for {
		if err := t.waitForRateLimit(ctx); err != nil {
			return "", err
		}
		resp, err := t.client.Do(req)
		if err != nil {
			return "", err
		}
		wait := t.updateRateLimit(resp.Header)
		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			if wait <= 0 {
				wait = backoffDuration(attempt)
			}
			attempt++
			log.Debug("brave rate limited",
				zap.Duration("wait", wait),
				zap.Int("attempt", attempt),
			)
			if err := sleepWithContext(ctx, wait); err != nil {
				return "", err
			}
			continue
		}
		defer resp.Body.Close()

		body, err := readWithLimit(resp.Body, t.maxOutputBytes)
		if err != nil {
			return "", err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return string(body), fmt.Errorf("brave search failed: %s", resp.Status)
		}

		var decoded braveWebResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return string(body), err
		}
		out := braveSearchOutput{
			Query:      decoded.Query,
			Results:    decoded.Web.Results,
			Summarizer: decoded.Summarizer,
			Rich:       decoded.Rich,
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return "", err
		}
		return string(payload), nil
	}
}

func readWithLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(r)
	}
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		body = body[:maxBytes]
	}
	return body, nil
}

func (t *BraveSearchTool) waitForRateLimit(ctx context.Context) error {
	t.mu.Lock()
	wait := time.Until(t.nextAllowed)
	t.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	return sleepWithContext(ctx, wait)
}

func (t *BraveSearchTool) updateRateLimit(h http.Header) time.Duration {
	limits := parseRateLimitInts(h.Get("X-RateLimit-Limit"))
	remaining := parseRateLimitInts(h.Get("X-RateLimit-Remaining"))
	reset := parseRateLimitInts(h.Get("X-RateLimit-Reset"))
	wait := rateLimitWait(remaining, reset, limits)
	if wait <= 0 {
		wait = retryAfterWait(h)
	}
	if wait > 0 {
		t.mu.Lock()
		next := time.Now().Add(wait)
		if next.After(t.nextAllowed) {
			t.nextAllowed = next
		}
		t.mu.Unlock()
	}
	return wait
}

func parseRateLimitInts(header string) []int {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		val, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, val)
	}
	return out
}

func rateLimitWait(remaining, reset, limits []int) time.Duration {
	if len(remaining) == 0 || len(reset) == 0 {
		if len(reset) > 0 && reset[0] > 0 {
			return time.Duration(reset[0]) * time.Second
		}
		return 0
	}
	waitSec := 0
	for i, rem := range remaining {
		if rem > 0 {
			continue
		}
		if i < len(limits) && limits[i] <= 0 {
			continue
		}
		if i >= len(reset) {
			continue
		}
		if reset[i] > waitSec {
			waitSec = reset[i]
		}
	}
	if waitSec <= 0 && len(reset) > 0 && reset[0] > 0 {
		waitSec = reset[0]
	}
	if waitSec <= 0 {
		return 0
	}
	return time.Duration(waitSec) * time.Second
}

func retryAfterWait(h http.Header) time.Duration {
	value := strings.TrimSpace(h.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if ts, err := http.ParseTime(value); err == nil {
		d := time.Until(ts)
		if d > 0 {
			return d
		}
	}
	return 0
}

func backoffDuration(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	wait := time.Second
	for i := 0; i < attempt; i++ {
		wait *= 2
		if wait > 10*time.Second {
			return 10 * time.Second
		}
	}
	return wait
}
