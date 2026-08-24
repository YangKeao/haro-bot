package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

type AgentProfile struct {
	ID                             int64      `json:"id"`
	ProviderID                     int64      `json:"provider_id"`
	SandboxID                      *int64     `json:"sandbox_id"`
	ProviderName                   string     `json:"provider_name"`
	Name                           string     `json:"name"`
	Description                    string     `json:"description"`
	Icon                           string     `json:"icon"`
	Color                          string     `json:"color"`
	AvatarMode                     string     `json:"avatar_mode"`
	AvatarObjectKey                string     `json:"-"`
	AvatarMIMEType                 string     `json:"-"`
	AvatarURL                      string     `json:"avatar_url,omitempty"`
	Instructions                   string     `json:"instructions"`
	BaseURL                        string     `json:"-"`
	APIKey                         string     `json:"-"`
	Model                          string     `json:"model"`
	PromptFormat                   string     `json:"-"`
	ReasoningEffortOverride        *string    `json:"reasoning_effort_override"`
	ContextWindowOverride          *int       `json:"context_window_override"`
	AutoCompactTokenLimitOverride  *int       `json:"auto_compact_token_limit_override"`
	ResolvedContextWindow          int        `json:"resolved_context_window"`
	ResolvedAutoCompactTokenLimit  int        `json:"resolved_auto_compact_token_limit"`
	ProviderDefaultReasoningEffort string     `json:"provider_default_reasoning_effort,omitempty"`
	EffectiveContextWindowPercent  int        `json:"effective_context_window_percent"`
	SkillNames                     []string   `json:"skill_names"`
	MCPServerIDs                   []int64    `json:"mcp_server_ids"`
	ArchivedAt                     *time.Time `json:"archived_at,omitempty"`
	CreatedAt                      time.Time  `json:"created_at"`
	UpdatedAt                      time.Time  `json:"updated_at"`
	RuntimeRevision                time.Time  `json:"-"`
}

type AgentWrite struct {
	ProviderID                    int64
	SandboxID                     *int64
	Name                          string
	Description                   string
	Icon                          string
	Color                         string
	AvatarMode                    string
	AvatarObjectKey               string
	AvatarMIMEType                string
	Instructions                  string
	Model                         string
	ReasoningEffortOverride       *string
	ContextWindowOverride         *int
	AutoCompactTokenLimitOverride *int
	EffectiveContextWindowPercent int
	SkillNames                    []string
	MCPServerIDs                  []int64
}

func (s *Store) ListAgents(ctx context.Context, includeArchived bool) ([]AgentProfile, error) {
	query := s.db.WithContext(ctx).Model(&dbmodel.Agent{})
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var rows []dbmodel.Agent
	if err := query.Order("archived_at IS NOT NULL, updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	profiles := make([]AgentProfile, 0, len(rows))
	for _, row := range rows {
		profile, err := s.agentFromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *Store) GetAgent(ctx context.Context, id int64) (AgentProfile, error) {
	var row dbmodel.Agent
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return AgentProfile{}, err
	}
	return s.agentFromRow(ctx, row)
}

func (s *Store) CreateAgent(ctx context.Context, input AgentWrite) (AgentProfile, error) {
	provider, err := s.GetProvider(ctx, input.ProviderID)
	if err != nil {
		return AgentProfile{}, err
	}
	if provider.ArchivedAt != nil {
		return AgentProfile{}, ErrProviderUnavailable
	}
	if input.AvatarMode == "" {
		input.AvatarMode = "icon"
	}
	if input.Color == "" {
		input.Color = "#2563EB"
	}
	row := dbmodel.Agent{
		ProviderID: input.ProviderID, SandboxID: input.SandboxID,
		Name: input.Name, Description: input.Description, Icon: input.Icon, Color: input.Color,
		AvatarMode: input.AvatarMode, AvatarObjectKey: input.AvatarObjectKey, AvatarMIMEType: input.AvatarMIMEType,
		Instructions: input.Instructions, Model: input.Model,
		ReasoningEffortOverride: input.ReasoningEffortOverride,
		ContextWindowOverride:   input.ContextWindowOverride, AutoCompactTokenLimitOverride: input.AutoCompactTokenLimitOverride,
		EffectiveContextWindowPercent: input.EffectiveContextWindowPercent,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := replaceAgentSkills(tx, row.ID, input.SkillNames); err != nil {
			return err
		}
		return replaceAgentMCPServers(tx, row.ID, input.MCPServerIDs)
	})
	if err != nil {
		return AgentProfile{}, err
	}
	return s.GetAgent(ctx, row.ID)
}

func (s *Store) UpdateAgent(ctx context.Context, id int64, input AgentWrite) (AgentProfile, error) {
	provider, err := s.GetProvider(ctx, input.ProviderID)
	if err != nil {
		return AgentProfile{}, err
	}
	if provider.ArchivedAt != nil {
		return AgentProfile{}, ErrProviderUnavailable
	}
	if input.AvatarMode == "" {
		input.AvatarMode = "icon"
	}
	updates := map[string]any{
		"provider_id": input.ProviderID, "sandbox_id": input.SandboxID, "name": input.Name, "description": input.Description, "icon": input.Icon, "color": input.Color,
		"avatar_mode": input.AvatarMode, "avatar_object_key": input.AvatarObjectKey, "avatar_mime_type": input.AvatarMIMEType,
		"instructions": input.Instructions, "model": input.Model,
		"reasoning_effort_override": input.ReasoningEffortOverride,
		"context_window_override":   input.ContextWindowOverride, "auto_compact_token_limit_override": input.AutoCompactTokenLimitOverride,
		"effective_context_window_percent": input.EffectiveContextWindowPercent, "updated_at": time.Now(),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&dbmodel.Agent{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := replaceAgentSkills(tx, id, input.SkillNames); err != nil {
			return err
		}
		return replaceAgentMCPServers(tx, id, input.MCPServerIDs)
	})
	if err != nil {
		return AgentProfile{}, err
	}
	return s.GetAgent(ctx, id)
}

func (s *Store) SetAgentArchived(ctx context.Context, id int64, archived bool) error {
	if archived {
		bound, err := s.GetTelegramAgentID(ctx)
		if err != nil {
			return err
		}
		if bound != nil && *bound == id {
			return ErrAgentTelegramBound
		}
	} else {
		agent, err := s.GetAgent(ctx, id)
		if err != nil {
			return err
		}
		provider, err := s.GetProvider(ctx, agent.ProviderID)
		if err != nil {
			return err
		}
		if provider.ArchivedAt != nil {
			return ErrProviderUnavailable
		}
	}
	var value any
	if archived {
		value = time.Now()
	}
	result := s.db.WithContext(ctx).Model(&dbmodel.Agent{}).Where("id = ?", id).
		Updates(map[string]any{"archived_at": value, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Store) agentFromRow(ctx context.Context, row dbmodel.Agent) (AgentProfile, error) {
	provider, err := s.GetProvider(ctx, row.ProviderID)
	if err != nil {
		return AgentProfile{}, err
	}
	var skillRows []dbmodel.AgentSkill
	if err := s.db.WithContext(ctx).Where("agent_id = ?", row.ID).Order("skill_name ASC").Find(&skillRows).Error; err != nil {
		return AgentProfile{}, err
	}
	skills := make([]string, 0, len(skillRows))
	for _, skill := range skillRows {
		skills = append(skills, skill.SkillName)
	}
	var mcpRows []dbmodel.AgentMCPServer
	if err := s.db.WithContext(ctx).Where("agent_id = ? AND enabled = ?", row.ID, true).Order("server_id ASC").Find(&mcpRows).Error; err != nil {
		return AgentProfile{}, err
	}
	mcpServerIDs := make([]int64, 0, len(mcpRows))
	for _, binding := range mcpRows {
		mcpServerIDs = append(mcpServerIDs, binding.ServerID)
		if binding.UpdatedAt.After(row.UpdatedAt) {
			row.UpdatedAt = binding.UpdatedAt
		}
	}
	avatarURL := ""
	if row.AvatarObjectKey != "" {
		avatarURL = fmt.Sprintf("/api/v1/agents/%d/avatar?v=%d", row.ID, row.UpdatedAt.UnixNano())
	}
	resolvedContext := 0
	resolvedAutoCompact := 0
	providerDefaultReasoning := ""
	if model := findModelCapability(provider.Models, row.Model); model != nil {
		resolvedContext = model.ContextWindow
		if resolvedContext == 0 {
			resolvedContext = model.MaxContextWindow
		}
		resolvedAutoCompact = model.AutoCompactTokenLimit
		providerDefaultReasoning = model.DefaultReasoningEffort
	}
	if row.ContextWindowOverride != nil {
		resolvedContext = *row.ContextWindowOverride
	}
	if row.AutoCompactTokenLimitOverride != nil {
		resolvedAutoCompact = *row.AutoCompactTokenLimitOverride
	}
	runtimeRevision := row.UpdatedAt
	if provider.UpdatedAt.After(runtimeRevision) {
		runtimeRevision = provider.UpdatedAt
	}
	if provider.ModelsFetchedAt != nil && provider.ModelsFetchedAt.After(runtimeRevision) {
		runtimeRevision = *provider.ModelsFetchedAt
	}
	return AgentProfile{
		ID: row.ID, ProviderID: row.ProviderID, SandboxID: row.SandboxID, ProviderName: provider.Name,
		Name: row.Name, Description: row.Description, Icon: row.Icon, Color: row.Color,
		AvatarMode: row.AvatarMode, AvatarObjectKey: row.AvatarObjectKey, AvatarMIMEType: row.AvatarMIMEType, AvatarURL: avatarURL,
		Instructions: row.Instructions, BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		Model: row.Model, PromptFormat: provider.PromptFormat,
		ReasoningEffortOverride: row.ReasoningEffortOverride,
		ContextWindowOverride:   row.ContextWindowOverride, AutoCompactTokenLimitOverride: row.AutoCompactTokenLimitOverride,
		ResolvedContextWindow: resolvedContext, ResolvedAutoCompactTokenLimit: resolvedAutoCompact,
		ProviderDefaultReasoningEffort: providerDefaultReasoning,
		EffectiveContextWindowPercent:  row.EffectiveContextWindowPercent,
		SkillNames:                     skills, MCPServerIDs: mcpServerIDs, ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		RuntimeRevision: runtimeRevision,
	}, nil
}

func replaceAgentMCPServers(tx *gorm.DB, agentID int64, ids []int64) error {
	if err := tx.Where("agent_id = ?", agentID).Delete(&dbmodel.AgentMCPServer{}).Error; err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := tx.Create(&dbmodel.AgentMCPServer{AgentID: agentID, ServerID: id, Enabled: true}).Error; err != nil {
			return err
		}
	}
	return nil
}

func replaceAgentSkills(tx *gorm.DB, agentID int64, names []string) error {
	if err := tx.Where("agent_id = ?", agentID).Delete(&dbmodel.AgentSkill{}).Error; err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := tx.Create(&dbmodel.AgentSkill{AgentID: agentID, SkillName: name}).Error; err != nil {
			return err
		}
	}
	return nil
}

type SessionRecord struct {
	ID         int64      `json:"id"`
	AgentID    int64      `json:"agent_id"`
	Title      string     `json:"title"`
	Channel    string     `json:"-"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type RecentSessionAgent struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	Color      string `json:"color"`
	AvatarMode string `json:"avatar_mode"`
	AvatarURL  string `json:"avatar_url,omitempty"`
}

type RecentSessionRecord struct {
	ID        int64              `json:"id"`
	AgentID   int64              `json:"agent_id"`
	Title     string             `json:"title"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Agent     RecentSessionAgent `json:"agent"`
}

func (s *Store) CreateSession(ctx context.Context, userID, agentID int64, title string) (SessionRecord, error) {
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return SessionRecord{}, err
	}
	if agent.ArchivedAt != nil {
		return SessionRecord{}, errors.New("agent is archived")
	}
	channel, err := randomID("web:", 12)
	if err != nil {
		return SessionRecord{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = "New session"
	}
	row := dbmodel.Session{UserID: userID, AgentID: &agentID, Channel: channel, Title: title}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return SessionRecord{}, err
	}
	return sessionFromRow(row), nil
}

func (s *Store) ListSessions(ctx context.Context, userID, agentID int64, archived bool) ([]SessionRecord, error) {
	query := s.db.WithContext(ctx).Where("user_id = ? AND agent_id = ?", userID, agentID)
	if archived {
		query = query.Where("archived_at IS NOT NULL")
	} else {
		query = query.Where("archived_at IS NULL")
	}
	var rows []dbmodel.Session
	if err := query.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SessionRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionFromRow(row))
	}
	return out, nil
}

func (s *Store) ListRecentSessions(ctx context.Context, userID int64, limit int) ([]RecentSessionRecord, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	type recentSessionRow struct {
		ID              int64     `gorm:"column:id"`
		AgentID         int64     `gorm:"column:agent_id"`
		Title           string    `gorm:"column:title"`
		CreatedAt       time.Time `gorm:"column:created_at"`
		UpdatedAt       time.Time `gorm:"column:updated_at"`
		AgentName       string    `gorm:"column:agent_name"`
		AgentIcon       string    `gorm:"column:agent_icon"`
		AgentColor      string    `gorm:"column:agent_color"`
		AgentAvatarMode string    `gorm:"column:agent_avatar_mode"`
		AgentAvatarKey  string    `gorm:"column:agent_avatar_key"`
		AgentUpdatedAt  time.Time `gorm:"column:agent_updated_at"`
	}
	var rows []recentSessionRow
	err := s.db.WithContext(ctx).Table("sessions").
		Select(`sessions.id, sessions.agent_id, sessions.title, sessions.created_at, sessions.updated_at,
agents.name AS agent_name, agents.icon AS agent_icon, agents.color AS agent_color,
agents.avatar_mode AS agent_avatar_mode, agents.avatar_object_key AS agent_avatar_key,
agents.updated_at AS agent_updated_at`).
		Joins("JOIN agents ON agents.id = sessions.agent_id").
		Where("sessions.user_id = ? AND sessions.archived_at IS NULL AND agents.archived_at IS NULL", userID).
		Order("sessions.updated_at DESC, sessions.id DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RecentSessionRecord, 0, len(rows))
	for _, row := range rows {
		avatarURL := ""
		if row.AgentAvatarKey != "" {
			avatarURL = fmt.Sprintf("/api/v1/agents/%d/avatar?v=%d", row.AgentID, row.AgentUpdatedAt.UnixNano())
		}
		out = append(out, RecentSessionRecord{
			ID: row.ID, AgentID: row.AgentID, Title: row.Title, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			Agent: RecentSessionAgent{
				ID: row.AgentID, Name: row.AgentName, Icon: row.AgentIcon, Color: row.AgentColor,
				AvatarMode: row.AgentAvatarMode, AvatarURL: avatarURL,
			},
		})
	}
	return out, nil
}

func (s *Store) GetSession(ctx context.Context, userID, id int64) (SessionRecord, error) {
	var row dbmodel.Session
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND agent_id IS NOT NULL", id, userID).First(&row).Error; err != nil {
		return SessionRecord{}, err
	}
	return sessionFromRow(row), nil
}

func (s *Store) RenameSession(ctx context.Context, userID, id int64, title string) error {
	result := s.db.WithContext(ctx).Model(&dbmodel.Session{}).Where("id = ? AND user_id = ? AND agent_id IS NOT NULL", id, userID).
		Updates(map[string]any{"title": title, "updated_at": time.Now()})
	return affected(result)
}

func (s *Store) AutoTitleSession(ctx context.Context, userID, id int64, content string) error {
	title := autoSessionTitle(content)
	return s.db.WithContext(ctx).Model(&dbmodel.Session{}).
		Where("id = ? AND user_id = ? AND title = ?", id, userID, "New session").
		Updates(map[string]any{"title": title, "updated_at": time.Now()}).Error
}

func autoSessionTitle(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	if len([]rune(title)) > 56 {
		title = string([]rune(title)[:56]) + "…"
	}
	if title == "" {
		return "Image conversation"
	}
	return title
}

func (s *Store) SetSessionArchived(ctx context.Context, userID, id int64, archived bool) error {
	var value any
	if archived {
		value = time.Now()
	}
	result := s.db.WithContext(ctx).Model(&dbmodel.Session{}).
		Where("id = ? AND user_id = ? AND agent_id IS NOT NULL", id, userID).
		Updates(map[string]any{"archived_at": value, "updated_at": time.Now()})
	return affected(result)
}

func (s *Store) TouchSession(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Model(&dbmodel.Session{}).Where("id = ?", id).UpdateColumn("updated_at", time.Now()).Error
}

func sessionFromRow(row dbmodel.Session) SessionRecord {
	agentID := int64(0)
	if row.AgentID != nil {
		agentID = *row.AgentID
	}
	return SessionRecord{ID: row.ID, AgentID: agentID, Title: row.Title, Channel: row.Channel, ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

type AttachmentRecord struct {
	ID           string    `json:"id"`
	SessionID    int64     `json:"session_id"`
	MessageID    *int64    `json:"message_id,omitempty"`
	ObjectKey    string    `json:"-"`
	OriginalName string    `json:"name"`
	MIMEType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) CreateAttachment(ctx context.Context, userID, sessionID int64, name, mime, objectKey string, size int64) (AttachmentRecord, error) {
	if _, err := s.GetSession(ctx, userID, sessionID); err != nil {
		return AttachmentRecord{}, err
	}
	id, err := randomID("", 16)
	if err != nil {
		return AttachmentRecord{}, err
	}
	row := dbmodel.Attachment{ID: id, SessionID: sessionID, ObjectKey: objectKey, OriginalName: name, MIMEType: mime, SizeBytes: size}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return AttachmentRecord{}, err
	}
	return attachmentFromRow(row), nil
}

func (s *Store) GetAttachment(ctx context.Context, userID int64, id string) (AttachmentRecord, error) {
	var row dbmodel.Attachment
	err := s.db.WithContext(ctx).Table("attachments").
		Joins("JOIN sessions ON sessions.id = attachments.session_id").
		Where("attachments.id = ? AND attachments.deleted_at IS NULL AND sessions.user_id = ?", id, userID).
		Select("attachments.*").First(&row).Error
	if err != nil {
		return AttachmentRecord{}, err
	}
	return attachmentFromRow(row), nil
}

func (s *Store) DeletePendingAttachment(ctx context.Context, userID int64, id string) (AttachmentRecord, error) {
	attachment, err := s.GetAttachment(ctx, userID, id)
	if err != nil {
		return AttachmentRecord{}, err
	}
	if attachment.MessageID != nil {
		return AttachmentRecord{}, errors.New("attachment is already part of a message")
	}
	if err := s.db.WithContext(ctx).Model(&dbmodel.Attachment{}).Where("id = ? AND message_id IS NULL").Update("deleted_at", time.Now()).Error; err != nil {
		return AttachmentRecord{}, err
	}
	return attachment, nil
}

func (s *Store) OrphanAttachments(ctx context.Context, before time.Time) ([]AttachmentRecord, error) {
	var rows []dbmodel.Attachment
	if err := s.db.WithContext(ctx).Where("message_id IS NULL AND deleted_at IS NULL AND created_at < ?", before).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AttachmentRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, attachmentFromRow(row))
	}
	return out, nil
}

func attachmentFromRow(row dbmodel.Attachment) AttachmentRecord {
	return AttachmentRecord{ID: row.ID, SessionID: row.SessionID, MessageID: row.MessageID, ObjectKey: row.ObjectKey, OriginalName: row.OriginalName, MIMEType: row.MIMEType, SizeBytes: row.SizeBytes, CreatedAt: row.CreatedAt}
}

type MessageRecord struct {
	ID          int64              `json:"id"`
	SessionID   int64              `json:"session_id"`
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	Metadata    json.RawMessage    `json:"metadata,omitempty"`
	Attachments []AttachmentRecord `json:"attachments,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
}

func (s *Store) ListMessages(ctx context.Context, userID, sessionID, beforeID int64, limit int) ([]MessageRecord, error) {
	if _, err := s.GetSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := s.db.WithContext(ctx).Where("session_id = ? AND deleted_at IS NULL", sessionID)
	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	var rows []dbmodel.Message
	if err := query.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MessageRecord, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		var attachments []dbmodel.Attachment
		if err := s.db.WithContext(ctx).Where("message_id = ? AND deleted_at IS NULL", row.ID).Order("created_at ASC").Find(&attachments).Error; err != nil {
			return nil, err
		}
		item := MessageRecord{ID: row.ID, SessionID: row.SessionID, Role: row.Role, Content: row.Content, Metadata: json.RawMessage(row.Metadata), CreatedAt: row.CreatedAt}
		for _, attachment := range attachments {
			item.Attachments = append(item.Attachments, attachmentFromRow(attachment))
		}
		out = append(out, item)
	}
	return out, nil
}

func randomID(prefix string, bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}

func affected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
