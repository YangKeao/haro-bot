package guidelines

import (
	"context"
	"errors"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/gorm"
)

// Guidelines represents the bot's core principles, personality, and behavioral rules.
// It is loaded into the system prompt to guide the LLM's behavior.
type Guidelines struct {
	ID        int64
	Content   string
	Version   int
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Manager handles guidelines storage and retrieval
type Manager struct {
	db *gorm.DB
}

// NewManager creates a new guidelines manager
func NewManager(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

// GetActive returns the currently active guidelines, or nil if none exists
func (m *Manager) GetActive(ctx context.Context) (*Guidelines, error) {
	var record dbmodel.Guidelines
	err := m.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("version DESC").
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return fromDBModel(record), nil
}

// Update creates a new version of the guidelines with the given content
// It deactivates previous versions and returns the new guidelines
func (m *Manager) Update(ctx context.Context, content string) (*Guidelines, error) {
	content = trimContent(content)
	if content == "" {
		return nil, errors.New("guidelines content cannot be empty")
	}

	// Get current max version
	var maxVersion int
	var current dbmodel.Guidelines
	err := m.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("version DESC").
		First(&current).Error
	if err == nil {
		maxVersion = current.Version
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Deactivate all existing guideliness
	if err := m.db.WithContext(ctx).
		Model(&dbmodel.Guidelines{}).
		Where("is_active = ?", true).
		Update("is_active", false).Error; err != nil {
		return nil, err
	}

	// Create new version
	newRecord := dbmodel.Guidelines{
		Content:  content,
		Version:  maxVersion + 1,
		IsActive: true,
	}
	if err := m.db.WithContext(ctx).Create(&newRecord).Error; err != nil {
		return nil, err
	}
	return fromDBModel(newRecord), nil
}

// GetAll returns all guidelines versions, newest first
func (m *Manager) GetAll(ctx context.Context, limit int) ([]Guidelines, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var records []dbmodel.Guidelines
	if err := m.db.WithContext(ctx).
		Order("version DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	result := make([]Guidelines, len(records))
	for i, r := range records {
		result[i] = *fromDBModel(r)
	}
	return result, nil
}

// GetByVersion returns a specific guidelines version
func (m *Manager) GetByVersion(ctx context.Context, version int) (*Guidelines, error) {
	var record dbmodel.Guidelines
	err := m.db.WithContext(ctx).
		Where("version = ?", version).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return fromDBModel(record), nil
}

// Rollback reactivates a previous version of the guidelines
func (m *Manager) Rollback(ctx context.Context, version int) (*Guidelines, error) {
	// Find the target version
	target, err := m.GetByVersion(ctx, version)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, errors.New("guidelines version not found")
	}

	// Deactivate all
	if err := m.db.WithContext(ctx).
		Model(&dbmodel.Guidelines{}).
		Where("is_active = ?", true).
		Update("is_active", false).Error; err != nil {
		return nil, err
	}

	// Reactivate target
	if err := m.db.WithContext(ctx).
		Model(&dbmodel.Guidelines{}).
		Where("version = ?", version).
		Update("is_active", true).Error; err != nil {
		return nil, err
	}
	return m.GetByVersion(ctx, version)
}

func fromDBModel(r dbmodel.Guidelines) *Guidelines {
	return &Guidelines{
		ID:        r.ID,
		Content:   r.Content,
		Version:   r.Version,
		IsActive:  r.IsActive,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func trimContent(content string) string {
	// Limit to ~100KB to prevent abuse
	const maxLen = 100 * 1024
	if len(content) > maxLen {
		content = content[:maxLen]
	}
	return content
}
