package db

import (
	"time"

	"gorm.io/datatypes"
)

type User struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	TelegramID *int64    `gorm:"column:telegram_id"`
	ExternalID *string   `gorm:"column:external_id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (User) TableName() string { return "users" }

type Session struct {
	ID         int64      `gorm:"primaryKey;autoIncrement"`
	UserID     int64      `gorm:"column:user_id"`
	AgentID    *int64     `gorm:"column:agent_id"`
	Channel    string     `gorm:"column:channel;size:32"`
	Title      string     `gorm:"column:title;size:255"`
	ArchivedAt *time.Time `gorm:"column:archived_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
}

func (Session) TableName() string { return "sessions" }

type Message struct {
	ID        int64          `gorm:"primaryKey;autoIncrement"`
	SessionID int64          `gorm:"column:session_id"`
	Role      string         `gorm:"column:role;size:16"`
	Content   string         `gorm:"column:content;type:mediumtext"`
	Metadata  datatypes.JSON `gorm:"column:metadata_json;type:json"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	DeletedAt *time.Time     `gorm:"column:deleted_at"`
}

func (Message) TableName() string { return "messages" }

type SessionSummary struct {
	ID        int64          `gorm:"primaryKey;autoIncrement"`
	SessionID int64          `gorm:"column:session_id"`
	EntryID   int64          `gorm:"column:entry_id"`
	Phase     string         `gorm:"column:phase;size:64"`
	Summary   string         `gorm:"column:summary;type:text"`
	StateJSON datatypes.JSON `gorm:"column:state_json;type:json"`
	CreatedAt time.Time      `gorm:"column:created_at"`
}

func (SessionSummary) TableName() string { return "session_summaries" }

type Provider struct {
	ID               int64      `gorm:"primaryKey;autoIncrement"`
	Name             string     `gorm:"column:name;size:128"`
	BaseURL          string     `gorm:"column:base_url;type:text"`
	APIKey           string     `gorm:"column:api_key;type:text"`
	PromptFormat     string     `gorm:"column:prompt_format;size:32"`
	ModelCatalogJSON string     `gorm:"column:model_catalog_json;type:longtext"`
	ModelsFetchedAt  *time.Time `gorm:"column:models_fetched_at"`
	ModelsLastError  *string    `gorm:"column:models_last_error;type:text"`
	ArchivedAt       *time.Time `gorm:"column:archived_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (Provider) TableName() string { return "providers" }

type Agent struct {
	ID                            int64      `gorm:"primaryKey;autoIncrement"`
	ProviderID                    int64      `gorm:"column:provider_id"`
	Name                          string     `gorm:"column:name;size:128"`
	Description                   string     `gorm:"column:description;type:text"`
	Icon                          string     `gorm:"column:icon;size:32"`
	Color                         string     `gorm:"column:color;size:16"`
	AvatarMode                    string     `gorm:"column:avatar_mode;size:16"`
	AvatarObjectKey               string     `gorm:"column:avatar_object_key;size:512"`
	AvatarMIMEType                string     `gorm:"column:avatar_mime_type;size:64"`
	Instructions                  string     `gorm:"column:instructions;type:longtext"`
	Model                         string     `gorm:"column:model;size:255"`
	ReasoningEffortOverride       *string    `gorm:"column:reasoning_effort_override;size:64"`
	ContextWindowOverride         *int       `gorm:"column:context_window_override"`
	AutoCompactTokenLimitOverride *int       `gorm:"column:auto_compact_token_limit_override"`
	EffectiveContextWindowPercent int        `gorm:"column:effective_context_window_percent"`
	ArchivedAt                    *time.Time `gorm:"column:archived_at"`
	CreatedAt                     time.Time  `gorm:"column:created_at"`
	UpdatedAt                     time.Time  `gorm:"column:updated_at"`
}

func (Agent) TableName() string { return "agents" }

type AgentSkill struct {
	AgentID   int64     `gorm:"primaryKey;column:agent_id"`
	SkillName string    `gorm:"primaryKey;column:skill_name;size:128"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (AgentSkill) TableName() string { return "agent_skills" }

type TelegramIntegration struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	AgentID   *int64    `gorm:"column:agent_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (TelegramIntegration) TableName() string { return "telegram_integrations" }

type Attachment struct {
	ID           string     `gorm:"primaryKey;column:id;size:36"`
	SessionID    int64      `gorm:"column:session_id"`
	MessageID    *int64     `gorm:"column:message_id"`
	ObjectKey    string     `gorm:"column:object_key;size:512"`
	OriginalName string     `gorm:"column:original_name;size:255"`
	MIMEType     string     `gorm:"column:mime_type;size:64"`
	SizeBytes    int64      `gorm:"column:size_bytes"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	DeletedAt    *time.Time `gorm:"column:deleted_at"`
}

func (Attachment) TableName() string { return "attachments" }

type SkillSource struct {
	ID            int64          `gorm:"primaryKey;autoIncrement"`
	SourceType    string         `gorm:"column:source_type;size:32"`
	InstallMethod string         `gorm:"column:install_method;size:32"`
	SourceURL     string         `gorm:"column:source_url;type:text"`
	SourceRef     string         `gorm:"column:source_ref;size:128"`
	SourceSubdir  string         `gorm:"column:source_subdir;size:255"`
	SkillFilters  datatypes.JSON `gorm:"column:skill_filters_json;type:json"`
	Status        string         `gorm:"column:status;size:16"`
	Version       *string        `gorm:"column:version;size:64"`
	LastSyncAt    *time.Time     `gorm:"column:last_sync_at"`
	LastError     *string        `gorm:"column:last_error;type:text"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
}

func (SkillSource) TableName() string { return "skill_sources" }

type SkillRegistry struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	SourceID    int64     `gorm:"column:source_id"`
	Name        string    `gorm:"column:name;size:128"`
	Description string    `gorm:"column:description;type:text"`
	Version     *string   `gorm:"column:version;size:64"`
	SkillPath   string    `gorm:"column:skill_path;type:text"`
	ContentHash string    `gorm:"column:content_hash;size:64"`
	Status      string    `gorm:"column:status;size:16"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (SkillRegistry) TableName() string { return "skills_registry" }

// Guidelines stores the bot's behavioral guidelines and principles.
// The database table is named "constitutions" for backward compatibility.
type Guidelines struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Content   string    `gorm:"column:content;type:longtext"`
	Version   int       `gorm:"column:version"`
	IsActive  bool      `gorm:"column:is_active"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Guidelines) TableName() string { return "constitutions" }
