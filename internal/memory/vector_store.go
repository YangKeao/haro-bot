package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type VectorStore interface {
	EnsureSchema(ctx context.Context, cfg config.MemoryConfig) error
	Insert(ctx context.Context, item MemoryItem, vector []float32) (int64, error)
	Update(ctx context.Context, item MemoryItem, vector []float32) error
	Delete(ctx context.Context, id int64) error
	Search(ctx context.Context, userID int64, sessionID *int64, vector []float32, limit int) ([]MemoryItem, error)
}

type TiDBVectorStore struct {
	db       *gorm.DB
	table    string
	distance string
}

func NewTiDBVectorStore(db *gorm.DB, distance string) *TiDBVectorStore {
	return &TiDBVectorStore{db: db, table: "memories", distance: distance}
}

func (s *TiDBVectorStore) EnsureSchema(ctx context.Context, cfg config.MemoryConfig) error {
	if s == nil || s.db == nil {
		return errors.New("vector store db required")
	}
	if cfg.Embedder.Dimensions <= 0 {
		return errors.New("memory embedder dimensions required")
	}
	embeddingType := "VECTOR"
	embeddingType = fmt.Sprintf("VECTOR(%d)", cfg.Embedder.Dimensions)
	createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  session_id BIGINT NULL,
  type VARCHAR(32) NOT NULL,
  content TEXT NOT NULL,
  metadata_json JSON,
  embedding %s,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_created (user_id, created_at),
  INDEX idx_session_created (session_id, created_at)
)`, s.table, embeddingType)
	if err := s.db.WithContext(ctx).Exec(createSQL).Error; err != nil {
		return err
	}
	if err := s.ensureVectorDimensions(ctx, cfg.Embedder.Dimensions); err != nil {
		return err
	}
	return nil
}

func (s *TiDBVectorStore) ensureVectorDimensions(ctx context.Context, dims int) error {
	if dims <= 0 {
		return errors.New("vector dimensions required")
	}
	var columnType string
	err := s.db.WithContext(ctx).
		Raw(`SELECT COLUMN_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'embedding'`, s.table).
		Scan(&columnType).Error
	if err != nil {
		return err
	}
	columnType = strings.ToLower(strings.TrimSpace(columnType))
	if columnType == "" {
		return errors.New("embedding column not found")
	}
	if strings.HasPrefix(columnType, "vector(") {
		start := strings.Index(columnType, "(")
		end := strings.Index(columnType, ")")
		if start > 0 && end > start+1 {
			if parsed, err := strconv.Atoi(columnType[start+1 : end]); err == nil && parsed != dims {
				return fmt.Errorf("embedding dimensions mismatch: column=%d config=%d", parsed, dims)
			}
			return nil
		}
	}
	if columnType == "vector" {
		alter := fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN embedding VECTOR(%d)", s.table, dims)
		return s.db.WithContext(ctx).Exec(alter).Error
	}
	return fmt.Errorf("unexpected embedding column type: %s", columnType)
}

func (s *TiDBVectorStore) ensureVectorIndex(ctx context.Context, distance string) error {
	log := logging.L().Named("memory_vector")
	distance = strings.ToLower(strings.TrimSpace(distance))
	funcName := "VEC_COSINE_DISTANCE"
	if distance == "l2" || distance == "euclidean" {
		funcName = "VEC_L2_DISTANCE"
	}
	indexName := fmt.Sprintf("idx_%s_embedding", s.table)
	var count int
	if err := s.db.WithContext(ctx).
		Raw(`SELECT COUNT(1) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`, s.table, indexName).
		Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	indexSQL := fmt.Sprintf("ALTER TABLE %s ADD VECTOR INDEX %s ((%s(embedding)))", s.table, indexName, funcName)
	if err := s.db.WithContext(ctx).Exec(indexSQL).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate key name") {
			log.Debug("vector index already exists", zap.String("index", indexName))
			return nil
		}
		log.Warn("vector index create failed", zap.Error(err))
		return err
	}
	return nil
}

func (s *TiDBVectorStore) ensureTiFlashReplica(ctx context.Context) error {
	log := logging.L().Named("memory_vector")
	setSQL := fmt.Sprintf("ALTER TABLE %s SET TIFLASH REPLICA 1", s.table)
	if err := s.db.WithContext(ctx).Exec(setSQL).Error; err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		var row tiflashReplicaRow
		err := s.db.WithContext(ctx).
			Raw("SELECT AVAILABLE, PROGRESS FROM information_schema.tiflash_replica WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", s.table).
			Scan(&row).Error
		if err != nil {
			return err
		}
		if row.Available == 1 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	log.Warn("tiflash replica not ready", zap.String("table", s.table))
	return errors.New("tiflash replica not ready")
}

func (s *TiDBVectorStore) Insert(ctx context.Context, item MemoryItem, vector []float32) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("vector store db required")
	}
	if item.UserID == 0 {
		return 0, errors.New("user_id required")
	}
	vec := vectorLiteral(vector)
	meta := jsonString(item.Metadata)
	var sessionID any
	if item.SessionID != nil && *item.SessionID != 0 {
		sessionID = *item.SessionID
	}
	res := s.db.WithContext(ctx).Exec(
		fmt.Sprintf("INSERT INTO %s (user_id, session_id, type, content, metadata_json, embedding) VALUES (?, ?, ?, ?, ?, ?)", s.table),
		item.UserID, sessionID, item.Type, item.Content, meta, vec,
	)
	if res.Error != nil {
		return 0, res.Error
	}
	var id int64
	if err := s.db.WithContext(ctx).Raw("SELECT LAST_INSERT_ID()").Scan(&id).Error; err != nil {
		return 0, err
	}
	return id, nil
}

func (s *TiDBVectorStore) Update(ctx context.Context, item MemoryItem, vector []float32) error {
	if s == nil || s.db == nil {
		return errors.New("vector store db required")
	}
	if item.ID == 0 {
		return errors.New("memory id required")
	}
	vec := vectorLiteral(vector)
	meta := jsonString(item.Metadata)
	return s.db.WithContext(ctx).Exec(
		fmt.Sprintf("UPDATE %s SET type = ?, content = ?, metadata_json = ?, embedding = ? WHERE id = ?", s.table),
		item.Type, item.Content, meta, vec, item.ID,
	).Error
}

func (s *TiDBVectorStore) Delete(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return errors.New("vector store db required")
	}
	if id == 0 {
		return errors.New("memory id required")
	}
	return s.db.WithContext(ctx).Exec(
		fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.table),
		id,
	).Error
}

func (s *TiDBVectorStore) Search(ctx context.Context, userID int64, sessionID *int64, vector []float32, limit int) ([]MemoryItem, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("vector store db required")
	}
	if userID == 0 {
		return nil, errors.New("user_id required")
	}
	if limit <= 0 {
		limit = 10
	}
	vec := vectorLiteral(vector)
	where := "user_id = ?"
	args := []any{vec, userID}
	if sessionID != nil && *sessionID != 0 {
		where += " AND session_id = ?"
		args = append(args, *sessionID)
	}
	funcName := "VEC_COSINE_DISTANCE"
	if strings.ToLower(strings.TrimSpace(s.distance)) == "l2" || strings.ToLower(strings.TrimSpace(s.distance)) == "euclidean" {
		funcName = "VEC_L2_DISTANCE"
	}
	args = append(args, limit)
	query := fmt.Sprintf(
		"SELECT id, user_id, session_id, type, content, metadata_json, created_at, updated_at, %s(embedding, ?) AS distance FROM %s WHERE %s ORDER BY distance LIMIT ?",
		funcName, s.table, where,
	)
	rows := []vectorRow{}
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]MemoryItem, 0, len(rows))
	for _, row := range rows {
		item := row.toItem()
		item.Score = scoreFromDistance(row.Distance, s.distance)
		items = append(items, item)
	}
	return items, nil
}

type vectorRow struct {
	ID        int64   `gorm:"column:id"`
	UserID    int64   `gorm:"column:user_id"`
	SessionID *int64  `gorm:"column:session_id"`
	Type      string  `gorm:"column:type"`
	Content   string  `gorm:"column:content"`
	Metadata  []byte  `gorm:"column:metadata_json"`
	CreatedAt string  `gorm:"column:created_at"`
	UpdatedAt string  `gorm:"column:updated_at"`
	Distance  float64 `gorm:"column:distance"`
}

type tiflashReplicaRow struct {
	Available int     `gorm:"column:AVAILABLE"`
	Progress  float64 `gorm:"column:PROGRESS"`
}

func (r vectorRow) toItem() MemoryItem {
	item := MemoryItem{
		ID:        r.ID,
		UserID:    r.UserID,
		SessionID: r.SessionID,
		Type:      r.Type,
		Content:   r.Content,
	}
	if len(r.Metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(r.Metadata, &meta); err == nil {
			item.Metadata = meta
		}
	}
	return item
}

func vectorLiteral(vector []float32) string {
	if len(vector) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(vector))
	for _, v := range vector {
		parts = append(parts, strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func jsonString(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(b)
}

func scoreFromDistance(distance float64, metric string) float64 {
	metric = strings.ToLower(strings.TrimSpace(metric))
	if metric == "l2" || metric == "euclidean" {
		return 1 / (1 + distance)
	}
	return 1 - distance
}
