package skills

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

type Source struct {
	ID            int64
	SourceType    string
	InstallMethod string
	URL           string
	Ref           string
	Subdir        string
	SkillFilters  []string
	Status        string
	Version       string
	LastSyncAt    *time.Time
	LastError     string
}

type RegistryEntry struct {
	ID          int64
	SourceID    int64
	Name        string
	Description string
	Version     string
	SkillPath   string
	ContentHash string
	Status      string
}

func (s *Store) UpsertSource(ctx context.Context, src Source) (int64, error) {
	if src.SourceType == "" {
		return 0, errors.New("source_type required")
	}
	if src.InstallMethod == "" {
		src.InstallMethod = src.SourceType
	}
	if src.Status == "" {
		src.Status = "active"
	}
	ref := strings.TrimSpace(src.Ref)
	subdir := strings.TrimSpace(src.Subdir)
	skillFiltersJSON, err := marshalSkillFilters(src.SkillFilters)
	if err != nil {
		return 0, err
	}
	record := dbmodel.SkillSource{
		SourceType:    src.SourceType,
		InstallMethod: src.InstallMethod,
		SourceURL:     src.URL,
		SourceRef:     ref,
		SourceSubdir:  subdir,
		SkillFilters:  skillFiltersJSON,
		Status:        src.Status,
	}
	tx := s.db.WithContext(ctx)
	if err := tx.Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"install_method", "skill_filters_json", "status"}),
	}).Create(&record).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("source_type = ? AND source_url = ? AND source_ref = ? AND source_subdir = ?",
		src.SourceType, src.URL, ref, subdir).First(&record).Error; err != nil {
		return 0, err
	}
	return record.ID, nil
}

func (s *Store) ListSources(ctx context.Context, includeDisabled bool) ([]Source, error) {
	tx := s.db.WithContext(ctx)
	if !includeDisabled {
		tx = tx.Where("status = ?", "active")
	}
	var records []dbmodel.SkillSource
	if err := tx.Order("id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(records))
	for _, r := range records {
		version := ""
		if r.Version != nil {
			version = *r.Version
		}
		lastError := ""
		if r.LastError != nil {
			lastError = *r.LastError
		}
		skillFilters, err := unmarshalSkillFilters(r.SkillFilters)
		if err != nil {
			return nil, err
		}
		out = append(out, Source{
			ID:            r.ID,
			SourceType:    r.SourceType,
			InstallMethod: r.InstallMethod,
			URL:           r.SourceURL,
			Ref:           r.SourceRef,
			Subdir:        r.SourceSubdir,
			SkillFilters:  skillFilters,
			Status:        r.Status,
			Version:       version,
			LastSyncAt:    r.LastSyncAt,
			LastError:     lastError,
		})
	}
	return out, nil
}

func (s *Store) DeleteSource(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("source_id required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source_id = ?", id).Delete(&dbmodel.SkillRegistry{}).Error; err != nil {
			return err
		}
		result := tx.Model(&dbmodel.SkillSource{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"status":       "deleted",
				"last_error":   nil,
				"last_sync_at": time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *Store) UpdateSourceSync(ctx context.Context, id int64, version string, errMsg string) error {
	updates := map[string]any{
		"last_sync_at": time.Now(),
		"last_error":   nullIfEmpty(errMsg),
	}
	if version != "" {
		updates["version"] = version
	}
	return s.db.WithContext(ctx).
		Model(&dbmodel.SkillSource{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (s *Store) ReplaceSkillsForSource(ctx context.Context, sourceID int64, entries []RegistryEntry) error {
	if sourceID <= 0 {
		return errors.New("source_id required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("source_id = ?", sourceID).Delete(&dbmodel.SkillRegistry{}).Error; err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Status == "" {
				entry.Status = "active"
			}
			record := dbmodel.SkillRegistry{
				SourceID:    sourceID,
				Name:        entry.Name,
				Description: entry.Description,
				Version:     stringPtrOrNil(entry.Version),
				SkillPath:   entry.SkillPath,
				ContentHash: entry.ContentHash,
				Status:      entry.Status,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) UpsertSkill(ctx context.Context, entry RegistryEntry) error {
	if entry.Status == "" {
		entry.Status = "active"
	}
	record := dbmodel.SkillRegistry{
		SourceID:    entry.SourceID,
		Name:        entry.Name,
		Description: entry.Description,
		Version:     stringPtrOrNil(entry.Version),
		SkillPath:   entry.SkillPath,
		ContentHash: entry.ContentHash,
		Status:      entry.Status,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"description", "version", "skill_path", "content_hash", "status"}),
	}).Create(&record).Error
}

func (s *Store) ListSkills(ctx context.Context) ([]RegistryEntry, error) {
	var records []dbmodel.SkillRegistry
	if err := s.db.WithContext(ctx).
		Joins("JOIN skill_sources ON skill_sources.id = skills_registry.source_id").
		Where("skill_sources.status = ?", "active").
		Order("skills_registry.source_id ASC, skills_registry.name ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return registryEntriesFromRecords(records), nil
}

func (s *Store) ListSkillsBySource(ctx context.Context, sourceID int64) ([]RegistryEntry, error) {
	if sourceID <= 0 {
		return nil, errors.New("source_id required")
	}
	var records []dbmodel.SkillRegistry
	if err := s.db.WithContext(ctx).
		Where("source_id = ?", sourceID).
		Order("name ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return registryEntriesFromRecords(records), nil
}

func registryEntriesFromRecords(records []dbmodel.SkillRegistry) []RegistryEntry {
	out := make([]RegistryEntry, 0, len(records))
	for _, r := range records {
		version := ""
		if r.Version != nil {
			version = *r.Version
		}
		out = append(out, RegistryEntry{
			ID:          r.ID,
			SourceID:    r.SourceID,
			Name:        r.Name,
			Description: r.Description,
			Version:     version,
			SkillPath:   r.SkillPath,
			ContentHash: r.ContentHash,
			Status:      r.Status,
		})
	}
	return out
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func stringPtrOrNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func marshalSkillFilters(filters []string) (datatypes.JSON, error) {
	normalized := normalizeSkillFilters(filters)
	if len(normalized) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(data), nil
}

func unmarshalSkillFilters(raw datatypes.JSON) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var filters []string
	if err := json.Unmarshal(raw, &filters); err != nil {
		return nil, err
	}
	return normalizeSkillFilters(filters), nil
}

func normalizeSkillFilters(filters []string) []string {
	if len(filters) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(filters))
	out := make([]string, 0, len(filters))
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if _, ok := seen[filter]; ok {
			continue
		}
		seen[filter] = struct{}{}
		out = append(out, filter)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
