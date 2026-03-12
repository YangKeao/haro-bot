package telegram

import "sync"

type telegramSessionDestination struct {
	chatID               int64
	threadID             int
	directTopicID        int
	businessConnectionID string
}

type telegramSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[int64]telegramSessionDestination
}

func newTelegramSessionRegistry() *telegramSessionRegistry {
	return &telegramSessionRegistry{sessions: make(map[int64]telegramSessionDestination)}
}

func (r *telegramSessionRegistry) Set(sessionID int64, dest telegramSessionDestination) {
	if r == nil || sessionID == 0 {
		return
	}
	r.mu.Lock()
	r.sessions[sessionID] = dest
	r.mu.Unlock()
}

func (r *telegramSessionRegistry) Get(sessionID int64) (telegramSessionDestination, bool) {
	if r == nil || sessionID == 0 {
		return telegramSessionDestination{}, false
	}
	r.mu.RLock()
	dest, ok := r.sessions[sessionID]
	r.mu.RUnlock()
	return dest, ok
}
