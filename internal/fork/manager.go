package fork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/memory"
)

type Manager struct {
	agent *agent.Agent
	store *memory.Store

	mu   sync.Mutex
	runs map[int64]*run
}

const defaultInheritRecent = 8

type run struct {
	parentSessionID int64
	childSessionID  int64
	userID          int64
	channel         string
	model           string
	startedAt       time.Time

	cancel context.CancelFunc
	done   chan struct{}
	status string
	err    error

	mu sync.Mutex
}

func NewManager(agentSvc *agent.Agent, store *memory.Store) *Manager {
	return &Manager{
		agent: agentSvc,
		store: store,
		runs:  make(map[int64]*run),
	}
}

func (m *Manager) Start(ctx context.Context, parentSessionID, userID int64, input, model string, inheritRecent int) (int64, error) {
	if m.agent == nil || m.store == nil {
		return 0, errors.New("fork manager not configured")
	}
	channel, err := m.newChildChannel(parentSessionID)
	if err != nil {
		return 0, err
	}
	childID, err := m.store.CreateSession(context.Background(), userID, channel)
	if err != nil {
		return 0, err
	}
	if inheritRecent <= 0 {
		inheritRecent = defaultInheritRecent
	}
	if err := m.copyRecent(ctx, parentSessionID, childID, inheritRecent); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		parentSessionID: parentSessionID,
		childSessionID:  childID,
		userID:          userID,
		channel:         channel,
		model:           model,
		startedAt:       time.Now(),
		cancel:          cancel,
		done:            make(chan struct{}),
		status:          "running",
	}
	m.mu.Lock()
	m.runs[childID] = r
	m.mu.Unlock()

	go func() {
		_, err := m.agent.HandleWithModel(ctx, userID, channel, input, model)
		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			r.status = "error"
			r.err = err
		} else {
			r.status = "completed"
		}
		close(r.done)
	}()

	return childID, nil
}

func (m *Manager) Interrupt(ctx context.Context, parentSessionID, childSessionID int64, message string, storeInChild bool, modelOverride string) (string, error) {
	r, err := m.getRun(parentSessionID, childSessionID)
	if err != nil {
		return "", err
	}
	return m.agent.InterruptSession(ctx, r.childSessionID, r.userID, message, modelOverride, storeInChild)
}

func (m *Manager) copyRecent(ctx context.Context, parentSessionID, childSessionID int64, limit int) error {
	if limit <= 0 {
		return nil
	}
	msgs, err := m.store.LoadRecentMessages(ctx, parentSessionID, limit)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if err := m.store.AddMessage(ctx, childSessionID, msg.Role, msg.Content, map[string]any{
			"inherited_from_session": parentSessionID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Cancel(parentSessionID, childSessionID int64) error {
	r, err := m.getRun(parentSessionID, childSessionID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == "completed" || r.status == "error" {
		return nil
	}
	r.status = "cancelled"
	r.cancel()
	return nil
}

func (m *Manager) Status(parentSessionID, childSessionID int64) (string, error) {
	r, err := m.getRun(parentSessionID, childSessionID)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, nil
}

func (m *Manager) getRun(parentSessionID, childSessionID int64) (*run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[childSessionID]
	if !ok {
		return nil, errors.New("fork session not found")
	}
	if r.parentSessionID != parentSessionID {
		return nil, errors.New("fork session not owned by caller")
	}
	return r, nil
}

func (m *Manager) newChildChannel(parentSessionID int64) (string, error) {
	suffix, err := shortRand()
	if err != nil {
		return "", err
	}
	channel := fmt.Sprintf("f:%d:%s", parentSessionID, suffix)
	if len(channel) > 32 {
		channel = channel[:32]
	}
	return channel, nil
}

func shortRand() (string, error) {
	var buf [3]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
