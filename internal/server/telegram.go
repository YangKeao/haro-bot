package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type TelegramClient struct {
	token string
	http  *http.Client
}

func NewTelegramClient(token string) *TelegramClient {
	return &TelegramClient{
		token: token,
		http:  &http.Client{},
	}
}

func (c *TelegramClient) SendMessage(chatID int64, text string) error {
	url := "https://api.telegram.org/bot" + c.token + "/sendMessage"
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

type telegramUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []telegramUpdate `json:"result"`
}

type telegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
}

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	From      struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

func (c *TelegramClient) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegramUpdate, error) {
	if timeoutSec <= 0 {
		timeoutSec = 20
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=%d", c.token, timeoutSec)
	if offset > 0 {
		url += fmt.Sprintf("&offset=%d", offset)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload telegramUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if !payload.OK {
		return nil, fmt.Errorf("telegram getUpdates failed")
	}
	return payload.Result, nil
}

func (s *Server) StartTelegramPolling(ctx context.Context) {
	if s.telegram == nil {
		return
	}
	go s.pollTelegram(ctx)
}

func (s *Server) pollTelegram(ctx context.Context) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		updates, err := s.telegram.GetUpdates(ctx, offset, 20)
		if err != nil {
			log.Printf("telegram poll error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if update.Message == nil || update.Message.Text == "" {
				continue
			}
			uid, err := s.store.GetOrCreateUserByTelegramID(ctx, update.Message.From.ID)
			if err != nil {
				log.Printf("telegram user error: %v", err)
				continue
			}
			output, err := s.agent.Handle(ctx, uid, "telegram", update.Message.Text)
			if err != nil {
				log.Printf("telegram agent error: %v", err)
				continue
			}
			if err := s.telegram.SendMessage(update.Message.Chat.ID, output); err != nil {
				log.Printf("telegram send error: %v", err)
			}
		}
	}
}
