package memory

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

type Message struct {
	ID        int64
	SessionID int64
	Role      string
	Content   string
	CreatedAt time.Time
}

type Memory struct {
	ID         int64
	UserID     int64
	Type       string
	Content    string
	Importance int
	CreatedAt  time.Time
}

func (s *Store) GetOrCreateUserByTelegramID(ctx context.Context, telegramID int64) (int64, error) {
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

func (s *Store) GetOrCreateUserByExternalID(ctx context.Context, externalID string) (int64, error) {
	var user dbmodel.User
	tx := s.db.WithContext(ctx)
	if err := tx.Where("external_id = ?", externalID).First(&user).Error; err == nil {
		return user.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	user = dbmodel.User{ExternalID: &externalID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&user).Error; err != nil {
		return 0, err
	}
	if err := tx.Where("external_id = ?", externalID).First(&user).Error; err != nil {
		return 0, err
	}
	return user.ID, nil
}

func (s *Store) GetOrCreateSession(ctx context.Context, userID int64, channel string) (int64, error) {
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

func (s *Store) CreateSession(ctx context.Context, userID int64, channel string) (int64, error) {
	session := dbmodel.Session{
		UserID:  userID,
		Channel: channel,
		Status:  "active",
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return 0, err
	}
	return session.ID, nil
}

func (s *Store) AddMessage(ctx context.Context, sessionID int64, role, content string, metadata any) error {
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

func (s *Store) LoadRecentMessages(ctx context.Context, sessionID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	var records []dbmodel.Message
	if err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(records))
	for _, r := range records {
		msgs = append(msgs, Message{
			ID:        r.ID,
			SessionID: r.SessionID,
			Role:      r.Role,
			Content:   r.Content,
			CreatedAt: r.CreatedAt,
		})
	}
	return reverseMessages(msgs), nil
}

func (s *Store) LoadLongMemories(ctx context.Context, userID int64, limit int) ([]Memory, error) {
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

func (s *Store) RecordSkillCall(ctx context.Context, sessionID int64, skillName string, input any, output any, status string) error {
	var inputJSON []byte
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		inputJSON = b
	}
	var outputJSON []byte
	if output != nil {
		b, err := json.Marshal(output)
		if err != nil {
			return err
		}
		outputJSON = b
	}
	var inputData datatypes.JSON
	var outputData datatypes.JSON
	if inputJSON != nil {
		inputData = datatypes.JSON(inputJSON)
	}
	if outputJSON != nil {
		outputData = datatypes.JSON(outputJSON)
	}
	record := dbmodel.SkillCall{
		SkillName:  skillName,
		SessionID:  sessionID,
		InputJSON:  inputData,
		OutputJSON: outputData,
		Status:     status,
	}
	return s.db.WithContext(ctx).Create(&record).Error
}

func reverseMessages(in []Message) []Message {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}
