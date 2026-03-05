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
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

type Manager struct {
	agent *agent.Agent
	store *memory.Store

	mu   sync.Mutex
	runs map[int64]*run

	cleanupAfter time.Duration
}

const defaultInheritRecent = 8
const defaultCleanupAfter = 10 * time.Minute

type ManagerOptions struct {
	CleanupAfter time.Duration
}

type run struct {
	parentSessionID int64
	childSessionID  int64
	userID          int64
	channel         string
	model           string
	startedAt       time.Time
	finishedAt      time.Time

	cancel context.CancelFunc
	done   chan struct{}
	status string
	err    error

	mu sync.Mutex
}

func NewManager(agentSvc *agent.Agent, store *memory.Store) *Manager {
	return NewManagerWithOptions(agentSvc, store, ManagerOptions{CleanupAfter: defaultCleanupAfter})
}

func NewManagerWithOptions(agentSvc *agent.Agent, store *memory.Store, opts ManagerOptions) *Manager {
	cleanupAfter := opts.CleanupAfter
	if cleanupAfter < 0 {
		cleanupAfter = 0
	}
	return &Manager{
		agent:        agentSvc,
		store:        store,
		runs:         make(map[int64]*run),
		cleanupAfter: cleanupAfter,
	}
}

func (m *Manager) Start(ctx context.Context, parentSessionID, userID int64, input, model string, inheritRecent int) (int64, error) {
	log := logging.L().Named("fork")
	if m.agent == nil || m.store == nil {
		return 0, errors.New("fork manager not configured")
	}
	channel, err := m.newChildChannel(parentSessionID)
	if err != nil {
		return 0, err
	}
	childID, err := m.store.CreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("create child session failed", zap.Error(err))
		return 0, err
	}
	if inheritRecent <= 0 {
		inheritRecent = defaultInheritRecent
	}
	if err := m.copyRecent(ctx, parentSessionID, childID, inheritRecent); err != nil {
		log.Warn("inherit context failed", zap.Error(err))
		return 0, err
	}
	runCtx, cancel := context.WithCancel(ctx)
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

	go func(childID int64) {
		_, err := m.agent.HandleWithModel(runCtx, userID, channel, input, model)
		r.mu.Lock()
		if r.status == "cancelled" {
			if r.err == nil && runCtx.Err() != nil {
				r.err = runCtx.Err()
			}
			r.finishedAt = time.Now()
		} else if err == nil {
			if runCtx.Err() != nil {
				r.status = "cancelled"
				r.err = runCtx.Err()
			} else {
				r.status = "completed"
			}
			r.finishedAt = time.Now()
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || runCtx.Err() != nil {
			r.status = "cancelled"
			r.err = err
			r.finishedAt = time.Now()
		} else {
			r.status = "error"
			r.err = err
			r.finishedAt = time.Now()
		}
		r.mu.Unlock()
		close(r.done)
		m.scheduleCleanup(childID)
		if r.err != nil {
			log.Warn("child session finished", zap.Int64("child_session_id", childID), zap.String("status", r.status), zap.Error(r.err))
		} else {
			log.Info("child session finished", zap.Int64("child_session_id", childID), zap.String("status", r.status))
		}
	}(childID)

	log.Info("child session started", zap.Int64("parent_session_id", parentSessionID), zap.Int64("child_session_id", childID))
	return childID, nil
}

func (m *Manager) Interrupt(ctx context.Context, parentSessionID, childSessionID int64, message string, storeInChild bool, modelOverride string) (string, error) {
	log := logging.L().Named("fork")
	r, err := m.getRun(parentSessionID, childSessionID)
	if err != nil {
		return "", err
	}
	resp, err := m.agent.InterruptSession(ctx, r.childSessionID, r.userID, message, modelOverride, storeInChild)
	if err != nil {
		log.Warn("interrupt failed", zap.Int64("child_session_id", childSessionID), zap.Error(err))
		return "", err
	}
	log.Info("interrupt completed", zap.Int64("child_session_id", childSessionID), zap.Bool("stored", storeInChild))
	return resp, nil
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
		parent := parentSessionID
		if err := m.store.AddMessage(ctx, childSessionID, msg.Role, msg.Content, &memory.MessageMetadata{
			InheritedFromSession: &parent,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Cancel(parentSessionID, childSessionID int64) error {
	log := logging.L().Named("fork")
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
	log.Info("child session cancelled", zap.Int64("child_session_id", childSessionID))
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

func (m *Manager) scheduleCleanup(childID int64) {
	if m.cleanupAfter <= 0 {
		return
	}
	after := m.cleanupAfter
	time.AfterFunc(after, func() {
		m.mu.Lock()
		delete(m.runs, childID)
		m.mu.Unlock()
	})
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
