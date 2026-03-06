package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
}

const currentSchemaVersion int64 = 6

var migrations = []migration{
	{version: 1, stmts: initSchemaSQL},
	{version: 2, stmts: appConfigSQL},
	{version: 3, stmts: dropSkillCallsSQL},
	{version: 4, stmts: addMessageSoftDeleteSQL},
	{version: 5, stmts: addSessionSummariesSQL},
	{version: 6, stmts: renameSessionSummariesSQL},
}

func applyMigrations(db *gorm.DB) error {
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
			return setSchemaVersion(tx, m.version)
		}); err != nil {
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
