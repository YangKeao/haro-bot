package fork

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"go.uber.org/zap"
)

type Manager struct {
	agent *agent.Agent
	store memory.StoreAPI

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
	originSessionID int64
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
	output string

	mu sync.Mutex
}

func NewManager(agentSvc *agent.Agent, store memory.StoreAPI) *Manager {
	return NewManagerWithOptions(agentSvc, store, ManagerOptions{CleanupAfter: defaultCleanupAfter})
}

func NewManagerWithOptions(agentSvc *agent.Agent, store memory.StoreAPI, opts ManagerOptions) *Manager {
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

func (m *Manager) Start(ctx context.Context, originSessionID, userID int64, input, model string, inheritRecent int) (int64, error) {
	log := logging.L().Named("fork")
	if m.agent == nil || m.store == nil {
		return 0, errors.New("fork manager not configured")
	}
	channel, err := m.newChildChannel(originSessionID)
	if err != nil {
		return 0, err
	}
	childID, err := m.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		log.Error("create child session failed", zap.Error(err))
		return 0, err
	}
	if inheritRecent <= 0 {
		inheritRecent = defaultInheritRecent
	}
	if err := m.copyRecent(ctx, originSessionID, childID, inheritRecent); err != nil {
		log.Warn("inherit context failed", zap.Error(err))
		return 0, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r := &run{
		originSessionID: originSessionID,
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
		output, err := m.agent.HandleWithModel(runCtx, userID, channel, input, model)
		r.mu.Lock()
		if output != "" {
			r.output = output
		}
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
		m.notifyOrigin(r)
		m.scheduleCleanup(childID)
		if r.err != nil {
			log.Warn("child session finished", zap.Int64("child_session_id", childID), zap.String("status", r.status), zap.Error(r.err))
		} else {
			log.Info("child session finished", zap.Int64("child_session_id", childID), zap.String("status", r.status))
		}
	}(childID)

	log.Info("child session started", zap.Int64("origin_session_id", originSessionID), zap.Int64("child_session_id", childID))
	return childID, nil
}

func (m *Manager) notifyOrigin(r *run) {
	if m == nil || m.agent == nil || r == nil {
		return
	}
	log := logging.L().Named("fork")
	r.mu.Lock()
	originID := r.originSessionID
	childID := r.childSessionID
	status := r.status
	output := r.output
	errText := ""
	if r.err != nil {
		errText = r.err.Error()
	}
	r.mu.Unlock()
	if originID == 0 {
		return
	}
	prompt := buildCompletionPrompt(childID, status, output, errText)
	meta := &memory.MessageMetadata{Status: "fork_status"}
	if _, err := m.agent.InterruptSession(context.Background(), originID, r.userID, prompt, "", true, meta); err != nil {
		log.Warn("notify origin failed", zap.Int64("origin_session_id", originID), zap.Int64("child_session_id", childID), zap.Error(err))
	}
}

func buildCompletionPrompt(childID int64, status, output, errText string) string {
	var b strings.Builder
	b.WriteString("You are notifying the user about a background task completion.\n")
	b.WriteString("Write a short, friendly update in 1-3 sentences. ")
	b.WriteString("Do not mention internal terms like \"child session\" or \"fork\". ")
	b.WriteString("If there is an error, state it clearly and suggest a next step. ")
	b.WriteString("If there is a result, summarize it briefly. ")
	b.WriteString("Only ask a question if user action is needed.\n\n")
	b.WriteString("Task ID: ")
	b.WriteString(fmt.Sprint(childID))
	b.WriteString("\nStatus: ")
	b.WriteString(status)
	if errText != "" {
		b.WriteString("\nError: ")
		b.WriteString(truncateRunes(errText, 1200))
	}
	if output != "" {
		b.WriteString("\nResult: ")
		b.WriteString(truncateRunes(output, 2000))
	}
	return b.String()
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 || text == "" {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

func (m *Manager) Interrupt(ctx context.Context, callerSessionID, childSessionID int64, message string, storeInChild bool, modelOverride string) (string, error) {
	log := logging.L().Named("fork")
	r, err := m.getRun(callerSessionID, childSessionID)
	if err != nil {
		return "", err
	}
	resp, err := m.agent.InterruptSession(ctx, r.childSessionID, r.userID, message, modelOverride, storeInChild, nil)
	if err != nil {
		log.Warn("interrupt failed", zap.Int64("child_session_id", childSessionID), zap.Error(err))
		return "", err
	}
	log.Info("interrupt completed", zap.Int64("child_session_id", childSessionID), zap.Bool("stored", storeInChild))
	return resp, nil
}

func (m *Manager) copyRecent(ctx context.Context, originSessionID, childSessionID int64, limit int) error {
	if limit <= 0 {
		return nil
	}
	msgs, _, err := m.store.LoadViewMessages(ctx, originSessionID, limit)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		parent := originSessionID
		if err := m.store.AddMessage(ctx, childSessionID, msg.Role, msg.Content, &memory.MessageMetadata{
			InheritedFromSession: &parent,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Cancel(callerSessionID, childSessionID int64) error {
	log := logging.L().Named("fork")
	r, err := m.getRun(callerSessionID, childSessionID)
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

func (m *Manager) Status(callerSessionID, childSessionID int64) (string, error) {
	status, err := m.StatusDetail(callerSessionID, childSessionID, 0)
	if err != nil {
		return "", err
	}
	return status.Status, nil
}

type StatusDetail struct {
	Status string
	Output string
	Error  string
}

func (m *Manager) StatusDetail(callerSessionID, childSessionID int64, wait time.Duration) (StatusDetail, error) {
	r, err := m.getRun(callerSessionID, childSessionID)
	if err != nil {
		return StatusDetail{}, err
	}
	r.mu.Lock()
	status := r.status
	done := r.done
	r.mu.Unlock()
	if status == "running" && wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	detail := StatusDetail{Status: r.status, Output: r.output}
	if r.err != nil {
		detail.Error = r.err.Error()
	}
	return detail, nil
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

func (m *Manager) getRun(callerSessionID, childSessionID int64) (*run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = callerSessionID
	r, ok := m.runs[childSessionID]
	if !ok {
		return nil, errors.New("fork session not found")
	}
	return r, nil
}

func (m *Manager) newChildChannel(originSessionID int64) (string, error) {
	suffix, err := shortRand()
	if err != nil {
		return "", err
	}
	channel := fmt.Sprintf("f:%d:%s", originSessionID, suffix)
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
