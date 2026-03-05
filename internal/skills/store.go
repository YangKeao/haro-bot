package skills

import (
	"context"
	"errors"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
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
	record := dbmodel.SkillSource{
		SourceType:    src.SourceType,
		InstallMethod: src.InstallMethod,
		SourceURL:     src.URL,
		SourceRef:     ref,
		SourceSubdir:  subdir,
		Status:        src.Status,
	}
	tx := s.db.WithContext(ctx)
	if err := tx.Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"install_method", "status"}),
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
	if err := tx.Find(&records).Error; err != nil {
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
		out = append(out, Source{
			ID:            r.ID,
			SourceType:    r.SourceType,
			InstallMethod: r.InstallMethod,
			URL:           r.SourceURL,
			Ref:           r.SourceRef,
			Subdir:        r.SourceSubdir,
			Status:        r.Status,
			Version:       version,
			LastSyncAt:    r.LastSyncAt,
			LastError:     lastError,
		})
	}
	return out, nil
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
	if err := s.db.WithContext(ctx).Find(&records).Error; err != nil {
		return nil, err
	}
	return registryEntriesFromRecords(records), nil
}

func (s *Store) ListSkillsBySource(ctx context.Context, sourceID int64) ([]RegistryEntry, error) {
	if sourceID <= 0 {
		return nil, errors.New("source_id required")
	}
	var records []dbmodel.SkillRegistry
	if err := s.db.WithContext(ctx).Where("source_id = ?", sourceID).Find(&records).Error; err != nil {
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
