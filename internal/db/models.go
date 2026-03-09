package db

import (
	"time"

	"gorm.io/datatypes"
)

type User struct {
	ID          int64          `gorm:"primaryKey;autoIncrement"`
	TelegramID  *int64         `gorm:"column:telegram_id"`
	ExternalID  *string        `gorm:"column:external_id"`
	ProfileJSON datatypes.JSON `gorm:"column:profile_json;type:json"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
}

func (User) TableName() string { return "users" }

type Session struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	UserID    int64     `gorm:"column:user_id"`
	Channel   string    `gorm:"column:channel;size:32"`
	Status    string    `gorm:"column:status;size:16"`
	Summary   string    `gorm:"column:summary;type:text"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
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
	ID             int64          `gorm:"primaryKey;autoIncrement"`
	SessionID      int64          `gorm:"column:session_id"`
	EntryID        int64          `gorm:"column:entry_id"`
	Phase          string         `gorm:"column:phase;size:64"`
	Summary        string         `gorm:"column:summary;type:text"`
	StateJSON      datatypes.JSON `gorm:"column:state_json;type:json"`
	SourceEntryIDs datatypes.JSON `gorm:"column:source_entry_ids;type:json"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
}

func (SessionSummary) TableName() string { return "session_summaries" }

type SkillSource struct {
	ID            int64      `gorm:"primaryKey;autoIncrement"`
	SourceType    string     `gorm:"column:source_type;size:32"`
	InstallMethod string     `gorm:"column:install_method;size:32"`
	SourceURL     string     `gorm:"column:source_url;type:text"`
	SourceRef     string     `gorm:"column:source_ref;size:128"`
	SourceSubdir  string     `gorm:"column:source_subdir;size:255"`
	Status        string     `gorm:"column:status;size:16"`
	Version       *string    `gorm:"column:version;size:64"`
	LastSyncAt    *time.Time `gorm:"column:last_sync_at"`
	LastError     *string    `gorm:"column:last_error;type:text"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
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

type ToolAudit struct {
	ID        int64          `gorm:"primaryKey;autoIncrement"`
	SessionID *int64         `gorm:"column:session_id"`
	UserID    *int64         `gorm:"column:user_id"`
	Tool      string         `gorm:"column:tool;size:32"`
	Path      string         `gorm:"column:path;type:text"`
	Allowed   bool           `gorm:"column:allowed"`
	Status    string         `gorm:"column:status;size:16"`
	Reason    string         `gorm:"column:reason;type:text"`
	Metadata  datatypes.JSON `gorm:"column:metadata_json;type:json"`
	CreatedAt time.Time      `gorm:"column:created_at"`
}

func (ToolAudit) TableName() string { return "tool_audit" }

type AppConfig struct {
	ID         int64          `gorm:"primaryKey;autoIncrement"`
	ConfigJSON datatypes.JSON `gorm:"column:config_json;type:json"`
	CreatedAt  time.Time      `gorm:"column:created_at"`
	UpdatedAt  time.Time      `gorm:"column:updated_at"`
}

func (AppConfig) TableName() string { return "app_config" }

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

// OAuthToken stores OAuth tokens for external services
type OAuthToken struct {
	ID           int64     `gorm:"primaryKey;autoIncrement"`
	Provider     string    `gorm:"column:provider;size:32;uniqueIndex"`
	AccessToken  string    `gorm:"column:access_token;type:text"`
	RefreshToken string    `gorm:"column:refresh_token;type:text"`
	ExpiresAt    *time.Time `gorm:"column:expires_at"`
	Email        string    `gorm:"column:email;size:255"`
	AccountID    string    `gorm:"column:account_id;size:128"`
	ExtraJSON    datatypes.JSON `gorm:"column:extra_json;type:json"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (OAuthToken) TableName() string { return "oauth_tokens" }
