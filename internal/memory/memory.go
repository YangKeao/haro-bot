package memory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/llm"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// store provides persistence for sessions, messages, anchors, and long-term memory.
type store struct {
	db *gorm.DB
}

// NewStore creates a StoreAPI backed by the provided database handle.
func NewStore(db *gorm.DB) StoreAPI {
	return &store{db: db}
}

// Message is a persisted chat message within a session.
type Message struct {
	ID        int64
	SessionID int64
	Role      string
	Content   string
	Metadata  *MessageMetadata
	CreatedAt time.Time
}

// MessageMetadata captures tool calls, tool outputs, and other message state.
type MessageMetadata struct {
	ToolCallID           string         `json:"tool_call_id,omitempty"`
	ToolCalls            []llm.ToolCall `json:"tool_calls,omitempty"`
	Status               string         `json:"status,omitempty"`
	InheritedFromSession *int64         `json:"inherited_from_session,omitempty"`
}

// Memory is a long-term memory record for a user.
type Memory struct {
	ID         int64
	UserID     int64
	Type       string
	Content    string
	Importance int
	CreatedAt  time.Time
}

// Anchor is a session summary snapshot used to compact the view window.
type Anchor struct {
	ID             int64
	SessionID      int64
	EntryID        int64
	Phase          string
	Summary        string
	State          map[string]any
	SourceEntryIDs []int64
	CreatedAt      time.Time
}

// GetOrCreateUserByTelegramID returns the internal user ID for a Telegram user,
// creating the user if it does not exist.
func (s *store) GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (int64, error) {
	var user dbmodel.User
	tx := s.db.WithContext(ctx)
	if err := tx.Where("telegram_id = ?", telegramID).First(&user).Error; err == nil {
		return user.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	user = dbmodel.User{TelegramID: &telegramID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&user).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		return 0, err
	}
	return user.ID, nil
}

// GetOrCreateSession returns the active session ID for a user/channel pair,
// creating a new session if none exists.
func (s *store) GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error) {
	var session dbmodel.Session
	tx := s.db.WithContext(ctx)
	if err := tx.Where("user_id = ? AND channel = ?", userID, channel).First(&session).Error; err == nil {
		return session.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	session = dbmodel.Session{UserID: userID, Channel: channel, Status: "active"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&session).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("user_id = ? AND channel = ?", userID, channel).First(&session).Error; err != nil {
		return 0, err
	}
	return session.ID, nil
}

// AddMessage appends a message to a session. Metadata captures tool calls/outputs and status.
func (s *store) AddMessage(ctx context.Context, sessionID int64, role, content string, metadata *MessageMetadata) error {
	var metaJSON []byte
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		metaJSON = b
	}
	var meta datatypes.JSON
	if metaJSON != nil {
		meta = datatypes.JSON(metaJSON)
	}
	msg := dbmodel.Message{
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Metadata:  meta,
	}
	return s.db.WithContext(ctx).Create(&msg).Error
}

// AppendAnchor stores a summary snapshot (anchor) for a session. If EntryID is 0,
// it anchors the latest message in the session.
func (s *store) AppendAnchor(ctx context.Context, sessionID int64, anchor Anchor) (int64, error) {
	if sessionID == 0 {
		return 0, errors.New("session id required")
	}
	entryID := anchor.EntryID
	if entryID == 0 {
		latest, err := s.latestMessageID(ctx, sessionID)
		if err != nil {
			return 0, err
		}
		entryID = latest
	}
	stateJSON, err := json.Marshal(anchor.State)
	if err != nil {
		return 0, err
	}
	sourceJSON, err := json.Marshal(anchor.SourceEntryIDs)
	if err != nil {
		return 0, err
	}
	record := dbmodel.SessionAnchor{
		SessionID:      sessionID,
		EntryID:        entryID,
		Phase:          anchor.Phase,
		Summary:        anchor.Summary,
		StateJSON:      datatypes.JSON(stateJSON),
		SourceEntryIDs: datatypes.JSON(sourceJSON),
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return 0, err
	}
	return record.ID, nil
}

// LoadLatestAnchor returns the most recent anchor for a session, or nil if none exists.
func (s *store) LoadLatestAnchor(ctx context.Context, sessionID int64) (*Anchor, error) {
	if sessionID == 0 {
		return nil, errors.New("session id required")
	}
	var record dbmodel.SessionAnchor
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id DESC").
		Limit(1).
		Find(&record).Error; err != nil {
		return nil, err
	}
	if record.ID == 0 {
		return nil, nil
	}
	var state map[string]any
	if len(record.StateJSON) > 0 {
		if err := json.Unmarshal(record.StateJSON, &state); err != nil {
			return nil, err
		}
	}
	var sourceIDs []int64
	if len(record.SourceEntryIDs) > 0 {
		if err := json.Unmarshal(record.SourceEntryIDs, &sourceIDs); err != nil {
			return nil, err
		}
	}
	return &Anchor{
		ID:             record.ID,
		SessionID:      record.SessionID,
		EntryID:        record.EntryID,
		Phase:          record.Phase,
		Summary:        record.Summary,
		State:          state,
		SourceEntryIDs: sourceIDs,
		CreatedAt:      record.CreatedAt,
	}, nil
}

// LoadViewMessages returns messages after the latest anchor (if any) and the anchor itself.
// This is the canonical "current view" for LLM context. If limit <= 0, all messages
// after the anchor are returned. Invalid tool call/output pairs may be soft-deleted.
func (s *store) LoadViewMessages(ctx context.Context, sessionID int64, limit int) ([]Message, *Anchor, error) {
	anchor, err := s.LoadLatestAnchor(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	entryID := int64(0)
	if anchor != nil {
		entryID = anchor.EntryID
	}
	msgs, err := s.loadMessagesAfter(ctx, sessionID, entryID, limit)
	if err != nil {
		return nil, nil, err
	}
	filtered, toDelete := filterInvalidToolOutputs(msgs)
	if len(toDelete) > 0 {
		if err := s.softDeleteMessages(ctx, toDelete); err != nil {
			return nil, nil, err
		}
	}
	return filtered, anchor, nil
}

// LoadLongMemories returns the user's long-term memories ordered by importance.
// If limit <= 0, a default limit is used.
func (s *store) LoadLongMemories(ctx context.Context, userID int64, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	var records []dbmodel.Memory
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("importance DESC, id DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	memories := make([]Memory, 0, len(records))
	for _, r := range records {
		memories = append(memories, Memory{
			ID:         r.ID,
			UserID:     r.UserID,
			Type:       r.Type,
			Content:    r.Content,
			Importance: r.Importance,
			CreatedAt:  r.CreatedAt,
		})
	}
	return memories, nil
}

func reverseMessages(in []Message) []Message {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}

func filterInvalidToolOutputs(msgs []Message) ([]Message, []int64) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	seenCalls := make(map[string]struct{})
	out := make([]Message, 0, len(msgs))
	var invalidIDs []int64
	for _, msg := range msgs {
		if msg.Role == "assistant" && msg.Metadata != nil {
			for _, call := range msg.Metadata.ToolCalls {
				if call.ID == "" {
					continue
				}
				seenCalls[call.ID] = struct{}{}
			}
		}
		if msg.Role == "tool" {
			callID := ""
			if msg.Metadata != nil {
				callID = msg.Metadata.ToolCallID
			}
			if callID == "" {
				invalidIDs = append(invalidIDs, msg.ID)
				continue
			}
			if _, ok := seenCalls[callID]; !ok {
				invalidIDs = append(invalidIDs, msg.ID)
				continue
			}
		}
		out = append(out, msg)
	}
	return out, invalidIDs
}

func (s *store) softDeleteMessages(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&dbmodel.Message{}).
		Where("id IN ? AND deleted_at IS NULL", ids).
		Update("deleted_at", now).Error
}

func (s *store) loadMessagesAfter(ctx context.Context, sessionID, afterID int64, limit int) ([]Message, error) {
	query := s.db.WithContext(ctx).
		Where("session_id = ? AND deleted_at IS NULL", sessionID)
	if afterID > 0 {
		query = query.Where("id > ?", afterID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var records []dbmodel.Message
	if err := query.Order("id DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(records))
	for _, r := range records {
		var meta *MessageMetadata
		if len(r.Metadata) > 0 {
			var parsed MessageMetadata
			if err := json.Unmarshal(r.Metadata, &parsed); err != nil {
				return nil, err
			}
			meta = &parsed
		}
		msgs = append(msgs, Message{
			ID:        r.ID,
			SessionID: r.SessionID,
			Role:      r.Role,
			Content:   r.Content,
			Metadata:  meta,
			CreatedAt: r.CreatedAt,
		})
	}
	return reverseMessages(msgs), nil
}

func (s *store) latestMessageID(ctx context.Context, sessionID int64) (int64, error) {
	var record dbmodel.Message
	if err := s.db.WithContext(ctx).
		Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Order("id DESC").
		Limit(1).
		Find(&record).Error; err != nil {
		return 0, err
	}
	return record.ID, nil
}
