package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type schemaMigration struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	Version   int64     `gorm:"column:version"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (schemaMigration) TableName() string { return "schema_migrations" }

type migration struct {
	version int64
	stmts   []string
	apply   func(*gorm.DB, config.MemoryConfig) error
}

const currentSchemaVersion int64 = 11

var migrations = []migration{
	{version: 1, stmts: initSchemaSQL},
	{version: 2, stmts: appConfigSQL},
	{version: 3, stmts: dropSkillCallsSQL},
	{version: 4, stmts: addMessageSoftDeleteSQL},
	{version: 5, stmts: addSessionSummariesSQL},
	{version: 6, stmts: renameSessionSummariesSQL},
	{version: 7, stmts: renameSessionSummaryIndexesSQL},
	{version: 8, stmts: replaceMemoriesTableSQL},
	{version: 9, apply: applyMemoryVectorIndex},
	{version: 10, stmts: addGuidelinessSQL},
	{version: 11, stmts: addOAuthTokensSQL},
}

func applyMigrations(db *gorm.DB, memCfg config.MemoryConfig) error {
	log := logging.L().Named("migrations")
	if db == nil {
		return errors.New("db required")
	}
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		log.Error("auto migrate schema_migrations failed", zap.Error(err))
		return err
	}
	current, err := getSchemaVersion(db)
	if err != nil {
		log.Error("get schema version failed", zap.Error(err))
		return err
	}
	if current > currentSchemaVersion {
		return fmt.Errorf("db schema version %d is newer than supported %d", current, currentSchemaVersion)
	}
	log.Info("current schema version", zap.Int64("version", current), zap.Int64("latest", currentSchemaVersion))
	for _, m := range migrations {
		if current >= m.version {
			continue
		}
		log.Info("applying migration", zap.Int64("version", m.version))
		if err := db.Transaction(func(tx *gorm.DB) error {
			for _, stmt := range m.stmts {
				if strings.TrimSpace(stmt) == "" {
					continue
				}
				if err := tx.Exec(stmt).Error; err != nil {
					log.Error("migration statement failed", zap.Int64("version", m.version), zap.Error(err))
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("apply migration v%d: %w", m.version, err)
		}
		if m.apply != nil {
			if err := m.apply(db, memCfg); err != nil {
				log.Error("migration hook failed", zap.Int64("version", m.version), zap.Error(err))
				return fmt.Errorf("apply migration v%d: %w", m.version, err)
			}
		}
		if err := setSchemaVersion(db, m.version); err != nil {
			return fmt.Errorf("apply migration v%d: %w", m.version, err)
		}
		current = m.version
		log.Info("migration applied", zap.Int64("version", current))
	}
	return nil
}

func getSchemaVersion(db *gorm.DB) (int64, error) {
	var row schemaMigration
	if err := db.First(&row, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.Version, nil
}

func setSchemaVersion(db *gorm.DB, version int64) error {
	row := schemaMigration{
		ID:      1,
		Version: version,
	}
	return db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&row).Error
}

var initSchemaSQL = []string{
	`CREATE TABLE IF NOT EXISTS users (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  telegram_id BIGINT UNIQUE,
  external_id VARCHAR(255) UNIQUE,
  profile_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)`,
	`CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  channel VARCHAR(32) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  summary TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_user_channel (user_id, channel),
  FOREIGN KEY (user_id) REFERENCES users(id)
)`,
	`CREATE TABLE IF NOT EXISTS messages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NOT NULL,
  role VARCHAR(16) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  metadata_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_session_created (session_id, created_at),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
)`,
	`CREATE TABLE IF NOT EXISTS memories (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  type VARCHAR(32) NOT NULL,
  content TEXT NOT NULL,
  importance INT DEFAULT 0,
  embedding BLOB,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_created (user_id, created_at),
  FOREIGN KEY (user_id) REFERENCES users(id)
)`,
	`CREATE TABLE IF NOT EXISTS skill_sources (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  source_type VARCHAR(32) NOT NULL,
  install_method VARCHAR(32) NOT NULL,
  source_url TEXT NOT NULL,
  source_ref VARCHAR(128) DEFAULT '',
  source_subdir VARCHAR(255) DEFAULT '',
  status VARCHAR(16) DEFAULT 'active',
  version VARCHAR(64),
  last_sync_at TIMESTAMP NULL,
  last_error TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_source (source_type(32), source_url(255), source_ref(128), source_subdir(255))
)`,
	`CREATE TABLE IF NOT EXISTS skills_registry (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  source_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  version VARCHAR(64),
  skill_path TEXT NOT NULL,
  content_hash CHAR(64),
  status VARCHAR(16) DEFAULT 'active',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_source_name (source_id, name),
  FOREIGN KEY (source_id) REFERENCES skill_sources(id)
)`,
	`CREATE TABLE IF NOT EXISTS tool_audit (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NULL,
  user_id BIGINT NULL,
  tool VARCHAR(32) NOT NULL,
  path TEXT,
  allowed TINYINT(1) NOT NULL,
  status VARCHAR(16) NOT NULL,
  reason TEXT,
  metadata_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`,
}

var appConfigSQL = []string{
	`CREATE TABLE IF NOT EXISTS app_config (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  config_json JSON NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)`,
}

var dropSkillCallsSQL = []string{
	`DROP TABLE IF EXISTS skill_calls`,
}

var replaceMemoriesTableSQL = []string{
	`DROP TABLE IF EXISTS memory_items`,
	`DROP TABLE IF EXISTS memories`,
	`CREATE TABLE IF NOT EXISTS memories (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  session_id BIGINT NULL,
  type VARCHAR(32) NOT NULL,
  content TEXT NOT NULL,
  metadata_json JSON,
  embedding VECTOR,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_created (user_id, created_at),
  INDEX idx_session_created (session_id, created_at)
)`,
}

var addMessageSoftDeleteSQL = []string{
	`ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMP NULL`,
	`CREATE INDEX idx_messages_session_deleted ON messages (session_id, deleted_at)`,
}

var addSessionSummariesSQL = []string{
	`CREATE TABLE IF NOT EXISTS session_anchors (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NOT NULL,
  entry_id BIGINT NOT NULL,
  phase VARCHAR(64) DEFAULT '',
  summary TEXT,
  state_json JSON,
  source_entry_ids JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_session_anchors_session (session_id, id),
  INDEX idx_session_anchors_entry (session_id, entry_id),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
)`,
}

var renameSessionSummariesSQL = []string{
	`RENAME TABLE session_anchors TO session_summaries`,
}

var renameSessionSummaryIndexesSQL = []string{
	`ALTER TABLE session_summaries RENAME INDEX idx_session_anchors_session TO idx_session_summaries_session`,
	`ALTER TABLE session_summaries RENAME INDEX idx_session_anchors_entry TO idx_session_summaries_entry`,
}

func applyMemoryVectorIndex(db *gorm.DB, cfg config.MemoryConfig) error {
	if cfg.Embedder.Dimensions <= 0 {
		return errors.New("memory embedder dimensions required for vector index")
	}
	if err := ensureMemoryEmbeddingDimensions(db, cfg.Embedder.Dimensions); err != nil {
		return err
	}
	if err := ensureMemoryTiFlashReplica(db); err != nil {
		return err
	}
	return ensureMemoryVectorIndex(db, cfg.Vector.Distance)
}

func ensureMemoryEmbeddingDimensions(db *gorm.DB, dims int) error {
	var columnType string
	err := db.
		Raw(`SELECT COLUMN_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'memories' AND COLUMN_NAME = 'embedding'`).
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
		alter := fmt.Sprintf("ALTER TABLE memories MODIFY COLUMN embedding VECTOR(%d)", dims)
		return db.Exec(alter).Error
	}
	return fmt.Errorf("unexpected embedding column type: %s", columnType)
}

func ensureMemoryTiFlashReplica(db *gorm.DB) error {
	setSQL := "ALTER TABLE memories SET TIFLASH REPLICA 1"
	if err := db.Exec(setSQL).Error; err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		var row struct {
			Available int     `gorm:"column:AVAILABLE"`
			Progress  float64 `gorm:"column:PROGRESS"`
		}
		err := db.
			Raw("SELECT AVAILABLE, PROGRESS FROM information_schema.tiflash_replica WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'memories'").
			Scan(&row).Error
		if err != nil {
			return err
		}
		if row.Available == 1 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return errors.New("tiflash replica not ready")
}

func ensureMemoryVectorIndex(db *gorm.DB, distance string) error {
	distance = strings.ToLower(strings.TrimSpace(distance))
	funcName := "VEC_COSINE_DISTANCE"
	if distance == "l2" || distance == "euclidean" {
		funcName = "VEC_L2_DISTANCE"
	}
	indexName := "idx_memories_embedding"
	var count int
	if err := db.
		Raw(`SELECT COUNT(1) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'memories' AND index_name = ?`, indexName).
		Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	indexSQL := fmt.Sprintf("ALTER TABLE memories ADD VECTOR INDEX %s ((%s(embedding)))", indexName, funcName)
	if err := db.Exec(indexSQL).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate key name") {
			return nil
		}
		return err
	}
	return nil
}

var addGuidelinessSQL = []string{
	`CREATE TABLE IF NOT EXISTS constitutions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  content LONGTEXT NOT NULL,
  version INT NOT NULL,
  is_active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_constitutions_version (version),
  INDEX idx_constitutions_active (is_active)
)`,
}

var addOAuthTokensSQL = []string{
	`CREATE TABLE IF NOT EXISTS oauth_tokens (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  provider VARCHAR(32) NOT NULL,
  access_token TEXT NOT NULL,
  refresh_token TEXT,
  expires_at TIMESTAMP NULL,
  email VARCHAR(255),
  account_id VARCHAR(128),
  extra_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_provider (provider)
)`,
}
