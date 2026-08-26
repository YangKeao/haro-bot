package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
)

const maxModelCatalogBytes = 4 << 20
const maxProviderUsageBytes = 1 << 20

var errProviderUsageUnsupported = errors.New("provider does not expose usage")

type ProviderUsageWindow struct {
	Kind          string     `json:"kind"`
	UsedPercent   float64    `json:"used_percent"`
	WindowSeconds int64      `json:"window_seconds,omitempty"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
}

type ProviderUsageLimit struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Allowed      bool                  `json:"allowed"`
	LimitReached bool                  `json:"limit_reached"`
	Windows      []ProviderUsageWindow `json:"windows"`
}

type ProviderUsage struct {
	FetchedAt time.Time            `json:"fetched_at"`
	Limits    []ProviderUsageLimit `json:"limits"`
}

type providerInput struct {
	Name             string  `json:"name"`
	BaseURL          string  `json:"base_url"`
	APIKey           *string `json:"api_key"`
	ClearAPIKey      bool    `json:"clear_api_key"`
	APIMode          string  `json:"api_mode"`
	WebSearchEnabled bool    `json:"web_search_enabled"`
	PromptFormat     string  `json:"prompt_format"`
}

func normalizeProviderInput(input providerInput, fallbackAPIKey string) (ProviderWrite, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if input.Name == "" || len([]rune(input.Name)) > 128 {
		return ProviderWrite{}, errors.New("name is required and must be at most 128 characters")
	}
	if !validateHTTPURL(input.BaseURL) {
		return ProviderWrite{}, errors.New("base_url must be an http or https URL")
	}
	apiMode := strings.ToLower(strings.TrimSpace(input.APIMode))
	if apiMode == "" {
		apiMode = "chat_completions"
	}
	if apiMode != "chat_completions" && apiMode != "responses" {
		return ProviderWrite{}, errors.New("api_mode must be chat_completions or responses")
	}
	if input.WebSearchEnabled && apiMode != "responses" {
		return ProviderWrite{}, errors.New("web_search_enabled requires the Responses API")
	}
	apiKey := fallbackAPIKey
	if input.ClearAPIKey {
		apiKey = ""
	} else if input.APIKey != nil {
		apiKey = *input.APIKey
	}
	return ProviderWrite{
		Name: input.Name, BaseURL: input.BaseURL, APIKey: apiKey,
		APIMode: apiMode, WebSearchEnabled: input.WebSearchEnabled,
		PromptFormat: string(config.NormalizePromptFormat(input.PromptFormat)),
	}, nil
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListProviders(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	provider, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	provider.Models = nil
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var input providerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	write, err := normalizeProviderInput(input, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", err.Error())
		return
	}
	provider, err := s.store.CreateProvider(r.Context(), write)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	provider.Models = nil
	writeJSON(w, http.StatusCreated, provider)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	existing, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var input providerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	write, err := normalizeProviderInput(input, existing.APIKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_provider", err.Error())
		return
	}
	connectionChanged := write.BaseURL != existing.BaseURL || write.APIKey != existing.APIKey
	provider, err := s.store.UpdateProvider(r.Context(), id, write, connectionChanged)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.runtimes.InvalidateProvider(id)
	provider.Models = nil
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleArchiveProvider(w http.ResponseWriter, r *http.Request) {
	s.setProviderArchived(w, r, true)
}

func (s *Server) handleRestoreProvider(w http.ResponseWriter, r *http.Request) {
	s.setProviderArchived(w, r, false)
}

func (s *Server) setProviderArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.store.SetProviderArchived(r.Context(), id, archived); err != nil {
		writeStoreError(w, err)
		return
	}
	s.runtimes.InvalidateProvider(id)
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

func (s *Server) handleGetProviderModels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	provider, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models": provider.Models, "fetched_at": provider.ModelsFetchedAt,
		"last_error": provider.ModelsLastError, "stale": provider.CatalogStale,
	})
}

func (s *Server) handleGetProviderUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	provider, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	usage, err := fetchProviderUsage(ctx, http.DefaultClient, provider.BaseURL, provider.APIKey)
	if err != nil {
		switch {
		case errors.Is(err, errProviderUsageUnsupported):
			writeError(w, http.StatusNotImplemented, "provider_usage_unsupported", "This provider does not expose compatible usage data.")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "provider_usage_timeout", "The provider usage request timed out.")
		default:
			writeError(w, http.StatusBadGateway, "provider_usage_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleRefreshProviderModels(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "providerID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	provider, err := s.store.GetProvider(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if provider.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "provider_archived", "provider is archived")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := discoverProviderModels(ctx, http.DefaultClient, provider.BaseURL, provider.APIKey)
	if err != nil {
		_ = s.store.SaveProviderCatalogError(r.Context(), id, err)
		writeError(w, http.StatusBadGateway, "model_discovery_failed", err.Error())
		return
	}
	provider, err = s.store.SaveProviderCatalog(r.Context(), id, models, time.Now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.runtimes.InvalidateProvider(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"models": provider.Models, "fetched_at": provider.ModelsFetchedAt,
		"last_error": provider.ModelsLastError, "stale": false,
	})
}

func discoverProviderModels(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]ModelCapability, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}
	if len(body) > maxModelCatalogBytes {
		return nil, errors.New("models response exceeds 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned %s", resp.Status)
	}
	return normalizeModelCatalog(body)
}

func fetchProviderUsage(ctx context.Context, client *http.Client, baseURL, apiKey string) (ProviderUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/usage", nil)
	if err != nil {
		return ProviderUsage{}, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ProviderUsage{}, fmt.Errorf("provider usage request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		return ProviderUsage{}, fmt.Errorf("%w: provider returned %s", errProviderUsageUnsupported, resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ProviderUsage{}, fmt.Errorf("provider usage request returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderUsageBytes+1))
	if err != nil {
		return ProviderUsage{}, fmt.Errorf("read provider usage response: %w", err)
	}
	if len(body) > maxProviderUsageBytes {
		return ProviderUsage{}, errors.New("provider usage response exceeds 1 MiB")
	}
	var usage ProviderUsage
	if err := json.Unmarshal(body, &usage); err != nil {
		return ProviderUsage{}, errors.New("provider returned invalid usage JSON")
	}
	if err := validateProviderUsage(usage); err != nil {
		return ProviderUsage{}, err
	}
	return usage, nil
}

func validateProviderUsage(usage ProviderUsage) error {
	if usage.FetchedAt.IsZero() || usage.Limits == nil {
		return errors.New("provider returned malformed usage data")
	}
	for _, limit := range usage.Limits {
		if strings.TrimSpace(limit.ID) == "" || strings.TrimSpace(limit.Name) == "" || limit.Windows == nil {
			return errors.New("provider returned malformed usage data")
		}
		for _, window := range limit.Windows {
			if (window.Kind != "primary" && window.Kind != "secondary") || math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) || window.UsedPercent < 0 || window.WindowSeconds < 0 {
				return errors.New("provider returned malformed usage data")
			}
		}
	}
	return nil
}

func normalizeModelCatalog(body []byte) ([]ModelCapability, error) {
	var root struct {
		Data []map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, errors.New("provider returned invalid models JSON")
	}
	seen := make(map[string]struct{})
	models := make([]ModelCapability, 0, len(root.Data))
	for _, raw := range root.Data {
		id := stringField(raw, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		model := ModelCapability{
			ID: id, DisplayName: stringField(raw, "display_name"), Description: stringField(raw, "description"),
			ContextWindow:          numberField(raw, "context_window", "context_length"),
			MaxContextWindow:       numberField(raw, "max_context_window"),
			AutoCompactTokenLimit:  numberField(raw, "auto_compact_token_limit"),
			DefaultReasoningEffort: firstStringField(raw, "default_reasoning_level", "default_reasoning_effort"),
			InputModalities:        stringSliceField(raw, "input_modalities"),
		}
		model.ReasoningEfforts = reasoningEffortsField(raw)
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, errors.New("provider returned an empty models list")
	}
	return models, nil
}

func stringField(raw map[string]any, key string) string {
	value, _ := raw[key].(string)
	return strings.TrimSpace(value)
}

func firstStringField(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func numberField(raw map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := raw[key].(type) {
		case json.Number:
			parsed, err := value.Int64()
			if err == nil && parsed > 0 && parsed <= int64(^uint(0)>>1) {
				return int(parsed)
			}
		case float64:
			if value > 0 {
				return int(value)
			}
		}
	}
	return 0
}

func stringSliceField(raw map[string]any, key string) []string {
	values, _ := raw[key].([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func reasoningEffortsField(raw map[string]any) []ReasoningEffortOption {
	var values []any
	for _, key := range []string{"supported_reasoning_levels", "reasoning_efforts", "supported_reasoning_efforts"} {
		if candidate, ok := raw[key].([]any); ok {
			values = candidate
			break
		}
	}
	seen := make(map[string]struct{})
	out := make([]ReasoningEffortOption, 0, len(values))
	for _, value := range values {
		effort := ""
		description := ""
		switch item := value.(type) {
		case string:
			effort = strings.TrimSpace(item)
		case map[string]any:
			effort = firstStringField(item, "effort", "value", "id")
			description = stringField(item, "description")
		}
		if effort == "" {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		out = append(out, ReasoningEffortOption{Value: effort, Description: description})
	}
	return out
}

func (s *Server) handleGetTelegramIntegration(w http.ResponseWriter, r *http.Request) {
	agentID, err := s.store.GetTelegramAgentID(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TelegramIntegration{TokenConfigured: s.telegramTokenConfigured, AgentID: agentID})
}

func (s *Server) handleUpdateTelegramIntegration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentID *int64 `json:"agent_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.AgentID != nil && *input.AgentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_agent", "agent_id must be positive or null")
		return
	}
	if err := s.store.SetTelegramAgentID(r.Context(), input.AgentID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TelegramIntegration{TokenConfigured: s.telegramTokenConfigured, AgentID: input.AgentID})
}
