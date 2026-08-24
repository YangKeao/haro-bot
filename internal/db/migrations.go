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

const currentSchemaVersion int64 = 21

var migrations = []migration{
	{version: 1, stmts: initSchemaSQL},
	{version: 2},
	{version: 3, stmts: dropSkillCallsSQL},
	{version: 4, stmts: addMessageSoftDeleteSQL},
	{version: 5, stmts: addSessionSummariesSQL},
	{version: 6, stmts: renameSessionSummariesSQL},
	{version: 7, stmts: renameSessionSummaryIndexesSQL},
	{version: 8, stmts: dropLegacyMemoryTablesSQL},
	{version: 9},
	{version: 10, stmts: addGuidelinessSQL},
	{version: 11, stmts: addSkillSourceFiltersSQL},
	{version: 12, stmts: dropToolAuditSQL},
	{version: 13, stmts: dropLegacyMemoryTablesSQL},
	{version: 14, stmts: dropDeadSchemaSQL},
	{version: 15, stmts: addWebWorkspaceSQL},
	{version: 16, stmts: addAgentAvatarsSQL},
	{version: 17, stmts: addProvidersAndGenericAgentsSQL},
	{version: 18, stmts: addRecentSessionsIndexSQL},
	{version: 19, stmts: addSandboxesSQL},
	{version: 20, stmts: addSandboxRunTTYSQL},
	{version: 21, stmts: addSandboxRuntimeOperationSQL},
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
			return nil
		}); err != nil {
			return fmt.Errorf("apply migration v%d: %w", m.version, err)
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
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)`,
	`CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  channel VARCHAR(32) NOT NULL,
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
	`CREATE TABLE IF NOT EXISTS skill_sources (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  source_type VARCHAR(32) NOT NULL,
  install_method VARCHAR(32) NOT NULL,
 source_url TEXT NOT NULL,
  source_ref VARCHAR(128) DEFAULT '',
  source_subdir VARCHAR(255) DEFAULT '',
  skill_filters_json JSON,
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
}

var addSkillSourceFiltersSQL = []string{
	`ALTER TABLE skill_sources ADD COLUMN IF NOT EXISTS skill_filters_json JSON NULL AFTER source_subdir`,
}

var dropSkillCallsSQL = []string{
	`DROP TABLE IF EXISTS skill_calls`,
}

var dropToolAuditSQL = []string{
	`DROP TABLE IF EXISTS tool_audit`,
}

var dropLegacyMemoryTablesSQL = []string{
	`DROP TABLE IF EXISTS memory_items`,
	`DROP TABLE IF EXISTS memories`,
}

var dropDeadSchemaSQL = []string{
	`DROP TABLE IF EXISTS app_config`,
	`ALTER TABLE users DROP COLUMN IF EXISTS profile_json`,
	`ALTER TABLE sessions DROP COLUMN IF EXISTS summary`,
	`ALTER TABLE sessions DROP COLUMN IF EXISTS status`,
	`ALTER TABLE session_summaries DROP COLUMN IF EXISTS source_entry_ids`,
}

var addWebWorkspaceSQL = []string{
	`CREATE TABLE IF NOT EXISTS web_agents (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(128) NOT NULL,
  description TEXT,
  icon VARCHAR(32) NOT NULL DEFAULT 'sparkles',
	  color VARCHAR(16) NOT NULL DEFAULT '#2563EB',
  instructions LONGTEXT,
  base_url TEXT NOT NULL,
  api_key TEXT,
  model VARCHAR(255) NOT NULL,
  prompt_format VARCHAR(32) NOT NULL DEFAULT 'openai',
  reasoning_enabled TINYINT(1) NOT NULL DEFAULT 0,
  reasoning_effort VARCHAR(32) NOT NULL DEFAULT '',
  context_window INT NOT NULL DEFAULT 0,
  auto_compact_token_limit INT NOT NULL DEFAULT 0,
  effective_context_window_percent INT NOT NULL DEFAULT 95,
  archived_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_web_agents_archived (archived_at)
)`,
	`CREATE TABLE IF NOT EXISTS web_agent_skills (
  agent_id BIGINT NOT NULL,
  skill_name VARCHAR(128) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, skill_name),
  FOREIGN KEY (agent_id) REFERENCES web_agents(id)
)`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS agent_id BIGINT NULL AFTER user_id`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS title VARCHAR(255) NOT NULL DEFAULT '' AFTER channel`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS archived_at TIMESTAMP NULL AFTER title`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_agent_archived_updated ON sessions (agent_id, archived_at, updated_at)`,
	`CREATE TABLE IF NOT EXISTS attachments (
  id VARCHAR(36) PRIMARY KEY,
  session_id BIGINT NOT NULL,
  message_id BIGINT NULL,
  object_key VARCHAR(512) NOT NULL,
  original_name VARCHAR(255) NOT NULL,
  mime_type VARCHAR(64) NOT NULL,
  size_bytes BIGINT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL,
  UNIQUE KEY uniq_attachments_object_key (object_key),
  INDEX idx_attachments_session_message (session_id, message_id),
  INDEX idx_attachments_orphan (message_id, created_at),
  FOREIGN KEY (session_id) REFERENCES sessions(id),
  FOREIGN KEY (message_id) REFERENCES messages(id)
)`,
}

var addAgentAvatarsSQL = []string{
	`ALTER TABLE web_agents ADD COLUMN IF NOT EXISTS avatar_mode VARCHAR(16) NOT NULL DEFAULT 'icon' AFTER color`,
	`ALTER TABLE web_agents ADD COLUMN IF NOT EXISTS avatar_object_key VARCHAR(512) NOT NULL DEFAULT '' AFTER avatar_mode`,
	`ALTER TABLE web_agents ADD COLUMN IF NOT EXISTS avatar_mime_type VARCHAR(64) NOT NULL DEFAULT '' AFTER avatar_object_key`,
	`ALTER TABLE web_agents MODIFY COLUMN color VARCHAR(16) NOT NULL DEFAULT '#2563EB'`,
}

var addProvidersAndGenericAgentsSQL = []string{
	`DROP TABLE IF EXISTS attachments`,
	`DROP TABLE IF EXISTS session_summaries`,
	`DROP TABLE IF EXISTS messages`,
	`DROP TABLE IF EXISTS sessions`,
	`DROP TABLE IF EXISTS telegram_integrations`,
	`DROP TABLE IF EXISTS agent_skills`,
	`DROP TABLE IF EXISTS agents`,
	`DROP TABLE IF EXISTS web_agent_skills`,
	`DROP TABLE IF EXISTS web_agents`,
	`DROP TABLE IF EXISTS providers`,
	`CREATE TABLE providers (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(128) NOT NULL,
  base_url TEXT NOT NULL,
  api_key TEXT,
  prompt_format VARCHAR(32) NOT NULL DEFAULT 'openai',
  model_catalog_json LONGTEXT,
  models_fetched_at TIMESTAMP NULL,
  models_last_error TEXT,
  archived_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_providers_name (name),
  INDEX idx_providers_archived (archived_at)
)`,
	`CREATE TABLE agents (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  provider_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  description TEXT,
  icon VARCHAR(32) NOT NULL DEFAULT 'sparkles',
  color VARCHAR(16) NOT NULL DEFAULT '#2563EB',
  avatar_mode VARCHAR(16) NOT NULL DEFAULT 'icon',
  avatar_object_key VARCHAR(512) NOT NULL DEFAULT '',
  avatar_mime_type VARCHAR(64) NOT NULL DEFAULT '',
  instructions LONGTEXT,
  model VARCHAR(255) NOT NULL,
  reasoning_effort_override VARCHAR(64) NULL,
  context_window_override INT NULL,
  auto_compact_token_limit_override INT NULL,
  effective_context_window_percent INT NOT NULL DEFAULT 95,
  archived_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_agents_provider (provider_id),
  INDEX idx_agents_archived (archived_at),
  FOREIGN KEY (provider_id) REFERENCES providers(id)
)`,
	`CREATE TABLE agent_skills (
  agent_id BIGINT NOT NULL,
  skill_name VARCHAR(128) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, skill_name),
  FOREIGN KEY (agent_id) REFERENCES agents(id)
)`,
	`CREATE TABLE sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  agent_id BIGINT NULL,
  channel VARCHAR(64) NOT NULL,
  title VARCHAR(255) NOT NULL DEFAULT '',
  archived_at TIMESTAMP NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_sessions_agent_archived_updated (agent_id, archived_at, updated_at),
  INDEX idx_sessions_user_channel (user_id, channel),
  FOREIGN KEY (user_id) REFERENCES users(id),
  FOREIGN KEY (agent_id) REFERENCES agents(id)
)`,
	`CREATE TABLE messages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NOT NULL,
  role VARCHAR(16) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  metadata_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL,
  INDEX idx_session_created (session_id, created_at),
  INDEX idx_messages_session_deleted (session_id, deleted_at),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
)`,
	`CREATE TABLE session_summaries (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NOT NULL,
  entry_id BIGINT NOT NULL,
  phase VARCHAR(64) DEFAULT '',
  summary TEXT,
  state_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_session_summaries_session (session_id, id),
  INDEX idx_session_summaries_entry (session_id, entry_id),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
)`,
	`CREATE TABLE attachments (
  id VARCHAR(36) PRIMARY KEY,
  session_id BIGINT NOT NULL,
  message_id BIGINT NULL,
  object_key VARCHAR(512) NOT NULL,
  original_name VARCHAR(255) NOT NULL,
  mime_type VARCHAR(64) NOT NULL,
  size_bytes BIGINT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP NULL,
  UNIQUE KEY uniq_attachments_object_key (object_key),
  INDEX idx_attachments_session_message (session_id, message_id),
  INDEX idx_attachments_orphan (message_id, created_at),
  FOREIGN KEY (session_id) REFERENCES sessions(id),
  FOREIGN KEY (message_id) REFERENCES messages(id)
)`,
	`CREATE TABLE telegram_integrations (
  id BIGINT PRIMARY KEY,
  agent_id BIGINT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (agent_id) REFERENCES agents(id)
)`,
}

var addRecentSessionsIndexSQL = []string{
	`CREATE INDEX IF NOT EXISTS idx_sessions_user_archived_updated ON sessions (user_id, archived_at, updated_at)`,
}

var addSandboxesSQL = []string{
	`CREATE TABLE sandboxes (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(128) NOT NULL,
  description TEXT,
  image TEXT NOT NULL,
  cpu_limit_millis INT NOT NULL DEFAULT 2000,
  memory_limit_mib INT NOT NULL DEFAULT 2048,
  ephemeral_storage_mib INT NOT NULL DEFAULT 10240,
  workspace_storage_mib INT NOT NULL DEFAULT 10240,
  desired_state VARCHAR(16) NOT NULL DEFAULT 'Running',
  revision BIGINT NOT NULL DEFAULT 1,
  applied_revision BIGINT NOT NULL DEFAULT 0,
  kubernetes_name VARCHAR(63) NOT NULL,
  runtime_ca_pem TEXT,
  runtime_client_cert_pem TEXT,
  runtime_client_key_ciphertext LONGTEXT,
  runtime_token_ciphertext LONGTEXT,
  last_error TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_sandboxes_name (name),
  UNIQUE KEY uniq_sandboxes_kubernetes_name (kubernetes_name),
  INDEX idx_sandboxes_state_updated (desired_state, updated_at)
)`,
	`ALTER TABLE agents ADD COLUMN sandbox_id BIGINT NULL AFTER provider_id`,
	`ALTER TABLE agents ADD INDEX idx_agents_sandbox (sandbox_id)`,
	`ALTER TABLE agents ADD CONSTRAINT fk_agents_sandbox FOREIGN KEY (sandbox_id) REFERENCES sandboxes(id) ON DELETE SET NULL`,
	`CREATE TABLE agent_environment_variables (
  agent_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  value_ciphertext LONGTEXT NOT NULL,
  is_secret TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (agent_id, name),
  FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
)`,
	`CREATE TABLE sandbox_runs (
  id VARCHAR(36) PRIMARY KEY,
  sandbox_id BIGINT NOT NULL,
  agent_id BIGINT NOT NULL,
  session_id BIGINT NOT NULL,
  command MEDIUMTEXT NOT NULL,
  status VARCHAR(24) NOT NULL,
  pid BIGINT NULL,
  exit_code INT NULL,
  started_at TIMESTAMP(6) NOT NULL,
  finished_at TIMESTAMP(6) NULL,
  output_tail MEDIUMTEXT,
  output_truncated TINYINT(1) NOT NULL DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_sandbox_runs_sandbox_status (sandbox_id, status, started_at),
  INDEX idx_sandbox_runs_session_status (session_id, status, started_at),
  FOREIGN KEY (sandbox_id) REFERENCES sandboxes(id) ON DELETE CASCADE,
  FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE,
  FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`,
}

var addSandboxRunTTYSQL = []string{
	`ALTER TABLE sandbox_runs ADD COLUMN tty TINYINT(1) NOT NULL DEFAULT 0 AFTER command`,
}

var addSandboxRuntimeOperationSQL = []string{
	`ALTER TABLE sandboxes ADD COLUMN runtime_operation VARCHAR(16) NOT NULL DEFAULT '' AFTER runtime_token_ciphertext`,
	`ALTER TABLE sandboxes ADD COLUMN runtime_operation_started_at TIMESTAMP(6) NULL AFTER runtime_operation`,
	`ALTER TABLE sandboxes ADD COLUMN runtime_operation_pod_uid VARCHAR(64) NOT NULL DEFAULT '' AFTER runtime_operation_started_at`,
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
