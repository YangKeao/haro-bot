package tools

import (
	"context"
	"encoding/json"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AuditStore struct {
	db *gorm.DB
}

func NewAuditStore(db *gorm.DB) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) Record(ctx context.Context, entry AuditEntry) error {
	var metaJSON []byte
	if entry.Metadata != nil {
		b, err := json.Marshal(entry.Metadata)
		if err != nil {
			return err
		}
		metaJSON = b
	}
	var meta datatypes.JSON
	if metaJSON != nil {
		meta = datatypes.JSON(metaJSON)
	}
	record := dbmodel.ToolAudit{
		SessionID: nullIfZero(entry.SessionID),
		UserID:    nullIfZero(entry.UserID),
		Tool:      entry.Tool,
		Path:      entry.Path,
		Allowed:   entry.Allowed,
		Status:    entry.Status,
		Reason:    nullIfEmpty(entry.Reason),
		Metadata:  meta,
	}
	return s.db.WithContext(ctx).Create(&record).Error
}

func nullIfZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullIfEmpty(v string) string {
	if v == "" {
		return ""
	}
	return v
}
