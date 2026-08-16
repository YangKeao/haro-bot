package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const providerCatalogTTL = 24 * time.Hour

var (
	ErrProviderInUse       = errors.New("provider is used by an active agent")
	ErrAgentTelegramBound  = errors.New("agent is bound to Telegram")
	ErrProviderUnavailable = errors.New("provider is archived")
)

type ReasoningEffortOption struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type ModelCapability struct {
	ID                     string                  `json:"id"`
	DisplayName            string                  `json:"display_name,omitempty"`
	Description            string                  `json:"description,omitempty"`
	ContextWindow          int                     `json:"context_window,omitempty"`
	MaxContextWindow       int                     `json:"max_context_window,omitempty"`
	AutoCompactTokenLimit  int                     `json:"auto_compact_token_limit,omitempty"`
	DefaultReasoningEffort string                  `json:"default_reasoning_effort,omitempty"`
	ReasoningEfforts       []ReasoningEffortOption `json:"reasoning_efforts,omitempty"`
	InputModalities        []string                `json:"input_modalities,omitempty"`
}

type ProviderProfile struct {
	ID               int64             `json:"id"`
	Name             string            `json:"name"`
	BaseURL          string            `json:"base_url"`
	APIKey           string            `json:"-"`
	APIKeyConfigured bool              `json:"api_key_configured"`
	PromptFormat     string            `json:"prompt_format"`
	ModelCount       int               `json:"model_count"`
	ModelsFetchedAt  *time.Time        `json:"models_fetched_at,omitempty"`
	ModelsLastError  string            `json:"models_last_error,omitempty"`
	CatalogStale     bool              `json:"catalog_stale"`
	Models           []ModelCapability `json:"models,omitempty"`
	ArchivedAt       *time.Time        `json:"archived_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type ProviderWrite struct {
	Name         string
	BaseURL      string
	APIKey       string
	PromptFormat string
}

func (s *Store) ListProviders(ctx context.Context, includeArchived bool) ([]ProviderProfile, error) {
	query := s.db.WithContext(ctx).Model(&dbmodel.Provider{})
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var rows []dbmodel.Provider
	if err := query.Order("archived_at IS NOT NULL, name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	providers := make([]ProviderProfile, 0, len(rows))
	for _, row := range rows {
		provider, err := providerFromRow(row)
		if err != nil {
			return nil, err
		}
		provider.Models = nil
		providers = append(providers, provider)
	}
	return providers, nil
}

func (s *Store) GetProvider(ctx context.Context, id int64) (ProviderProfile, error) {
	var row dbmodel.Provider
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return ProviderProfile{}, err
	}
	return providerFromRow(row)
}

func (s *Store) CreateProvider(ctx context.Context, input ProviderWrite) (ProviderProfile, error) {
	row := dbmodel.Provider{Name: input.Name, BaseURL: input.BaseURL, APIKey: input.APIKey, PromptFormat: input.PromptFormat}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ProviderProfile{}, err
	}
	return s.GetProvider(ctx, row.ID)
}

func (s *Store) UpdateProvider(ctx context.Context, id int64, input ProviderWrite, clearCatalog bool) (ProviderProfile, error) {
	updates := map[string]any{
		"name": input.Name, "base_url": input.BaseURL, "api_key": input.APIKey,
		"prompt_format": input.PromptFormat, "updated_at": time.Now(),
	}
	if clearCatalog {
		updates["model_catalog_json"] = ""
		updates["models_fetched_at"] = nil
		updates["models_last_error"] = nil
	}
	result := s.db.WithContext(ctx).Model(&dbmodel.Provider{}).Where("id = ?", id).Updates(updates)
	if err := affected(result); err != nil {
		return ProviderProfile{}, err
	}
	return s.GetProvider(ctx, id)
}

func (s *Store) SetProviderArchived(ctx context.Context, id int64, archived bool) error {
	if archived {
		var count int64
		if err := s.db.WithContext(ctx).Model(&dbmodel.Agent{}).
			Where("provider_id = ? AND archived_at IS NULL", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrProviderInUse
		}
	}
	var value any
	if archived {
		value = time.Now()
	}
	return affected(s.db.WithContext(ctx).Model(&dbmodel.Provider{}).Where("id = ?", id).
		Updates(map[string]any{"archived_at": value, "updated_at": time.Now()}))
}

func (s *Store) SaveProviderCatalog(ctx context.Context, id int64, models []ModelCapability, fetchedAt time.Time) (ProviderProfile, error) {
	encoded, err := json.Marshal(models)
	if err != nil {
		return ProviderProfile{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbmodel.Provider{}).Where("id = ?", id).Updates(map[string]any{
		"model_catalog_json": string(encoded), "models_fetched_at": fetchedAt, "models_last_error": nil,
	})
	if err := affected(result); err != nil {
		return ProviderProfile{}, err
	}
	return s.GetProvider(ctx, id)
}

func (s *Store) SaveProviderCatalogError(ctx context.Context, id int64, discoveryErr error) error {
	message := strings.TrimSpace(discoveryErr.Error())
	if len(message) > 4096 {
		message = message[:4096]
	}
	result := s.db.WithContext(ctx).Model(&dbmodel.Provider{}).Where("id = ?", id).
		UpdateColumn("models_last_error", message)
	return affected(result)
}

func providerFromRow(row dbmodel.Provider) (ProviderProfile, error) {
	models := []ModelCapability{}
	if strings.TrimSpace(row.ModelCatalogJSON) != "" {
		if err := json.Unmarshal([]byte(row.ModelCatalogJSON), &models); err != nil {
			return ProviderProfile{}, err
		}
	}
	lastError := ""
	if row.ModelsLastError != nil {
		lastError = *row.ModelsLastError
	}
	return ProviderProfile{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseURL, APIKey: row.APIKey, APIKeyConfigured: row.APIKey != "",
		PromptFormat: row.PromptFormat, ModelCount: len(models), ModelsFetchedAt: row.ModelsFetchedAt,
		ModelsLastError: lastError, CatalogStale: row.ModelsFetchedAt == nil || time.Since(*row.ModelsFetchedAt) > providerCatalogTTL,
		Models: models, ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func findModelCapability(models []ModelCapability, modelID string) *ModelCapability {
	for i := range models {
		if models[i].ID == modelID {
			return &models[i]
		}
	}
	return nil
}

type TelegramIntegration struct {
	TokenConfigured bool   `json:"token_configured"`
	AgentID         *int64 `json:"agent_id"`
}

func (s *Store) GetTelegramAgentID(ctx context.Context) (*int64, error) {
	var row dbmodel.TelegramIntegration
	if err := s.db.WithContext(ctx).First(&row, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row.AgentID, nil
}

func (s *Store) SetTelegramAgentID(ctx context.Context, agentID *int64) error {
	if agentID != nil {
		agent, err := s.GetAgent(ctx, *agentID)
		if err != nil {
			return err
		}
		if agent.ArchivedAt != nil {
			return errors.New("agent is archived")
		}
	}
	row := dbmodel.TelegramIntegration{ID: 1, AgentID: agentID}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"agent_id", "updated_at"}),
	}).Create(&row).Error
}

func (s *Store) BindSessionAgent(ctx context.Context, sessionID, agentID int64) error {
	return affected(s.db.WithContext(ctx).Model(&dbmodel.Session{}).Where("id = ?", sessionID).Update("agent_id", agentID))
}
