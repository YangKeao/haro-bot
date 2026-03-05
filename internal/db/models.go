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
}

func (Message) TableName() string { return "messages" }

type Memory struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	UserID     int64     `gorm:"column:user_id"`
	Type       string    `gorm:"column:type;size:32"`
	Content    string    `gorm:"column:content;type:text"`
	Importance int       `gorm:"column:importance"`
	Embedding  []byte    `gorm:"column:embedding;type:blob"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (Memory) TableName() string { return "memories" }

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
