package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scheduler manages scheduled tasks.
type Scheduler struct {
	agent      *agent.Agent
	statusFunc func(sessionID int64) *agent.SessionStatus
	getSession func(ctx context.Context, userID int64, channel string) (int64, error)
	db         *gorm.DB

	mu    sync.RWMutex
	tasks map[int64]*runningTask
}

type runningTask struct {
	task     *db.SchedulerTask
	schedule *Schedule
	cancel   context.CancelFunc
}

// New creates a new scheduler.
func New(
	ag *agent.Agent,
	statusFunc func(sessionID int64) *agent.SessionStatus,
	getSession func(ctx context.Context, userID int64, channel string) (int64, error),
	database *gorm.DB,
) *Scheduler {
	return &Scheduler{
		agent:      ag,
		statusFunc: statusFunc,
		getSession: getSession,
		db:         database,
		tasks:      make(map[int64]*runningTask),
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) {
	log := logging.L().Named("scheduler")
	log.Info("scheduler starting")
	s.loadTasks(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("scheduler stopping")
			s.stopAllTasks()
			return
		case <-ticker.C:
			s.syncTasks(ctx)
		}
	}
}

func (s *Scheduler) loadTasks(ctx context.Context) {
	var tasks []db.SchedulerTask
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		logging.L().Named("scheduler").Error("failed to load tasks", zap.Error(err))
		return
	}
	for _, task := range tasks {
		s.startTask(&task)
	}
}

func (s *Scheduler) syncTasks(ctx context.Context) {
	log := logging.L().Named("scheduler")
	var tasks []db.SchedulerTask
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error("failed to sync tasks", zap.Error(err))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	currentTasks := make(map[int64]bool)
	for _, task := range tasks {
		currentTasks[task.ID] = true
	}

	// Stop removed/updated tasks
	for id, rt := range s.tasks {
		if !currentTasks[id] {
			log.Info("stopping task", zap.String("name", rt.task.Name))
			rt.cancel()
			delete(s.tasks, id)
			continue
		}
		var dbTask db.SchedulerTask
		if err := s.db.First(&dbTask, id).Error; err == nil {
			if dbTask.UpdatedAt.After(rt.task.UpdatedAt) {
				log.Info("restarting updated task", zap.String("name", rt.task.Name))
				rt.cancel()
				delete(s.tasks, id)
				s.startTask(&dbTask)
			}
		}
	}

	// Start new tasks
	for _, task := range tasks {
		if _, exists := s.tasks[task.ID]; !exists {
			s.startTask(&task)
		}
	}
}

func (s *Scheduler) startTask(task *db.SchedulerTask) {
	log := logging.L().Named("scheduler")

	schedule, err := ParseCron(task.CronExpr)
	if err != nil {
		log.Error("invalid cron expression", zap.String("name", task.Name), zap.Error(err))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := &runningTask{
		task:     task,
		schedule: schedule,
		cancel:   cancel,
	}

	s.mu.Lock()
	s.tasks[task.ID] = rt
	s.mu.Unlock()

	go s.runTaskLoop(ctx, rt)
	log.Info("task started", zap.String("name", task.Name), zap.String("cron", task.CronExpr))
}

func (s *Scheduler) stopAllTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rt := range s.tasks {
		rt.cancel()
	}
	s.tasks = make(map[int64]*runningTask)
}

func (s *Scheduler) runTaskLoop(ctx context.Context, rt *runningTask) {
	nextRun := rt.schedule.Next(time.Now())
	s.updateNextRun(ctx, rt.task.ID, &nextRun)

	for {
		waitDuration := time.Until(nextRun)
		if waitDuration <= 0 {
			s.executeTask(ctx, rt)
			nextRun = rt.schedule.Next(time.Now())
			s.updateNextRun(ctx, rt.task.ID, &nextRun)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(waitDuration):
			s.executeTask(ctx, rt)
			nextRun = rt.schedule.Next(time.Now())
			s.updateNextRun(ctx, rt.task.ID, &nextRun)
		}
	}
}

func (s *Scheduler) executeTask(ctx context.Context, rt *runningTask) {
	log := logging.L().Named("scheduler").With(zap.String("task", rt.task.Name))
	log.Info("executing task")

	sessionID, err := s.getSession(ctx, rt.task.UserID, rt.task.Channel)
	if err != nil {
		log.Error("failed to get session", zap.Error(err))
		s.recordResult(ctx, rt.task.ID, "failed", nil)
		return
	}

	status := s.statusFunc(sessionID)
	if status != nil && status.State != "idle" {
		if rt.task.SkipIfBusy {
			log.Info("skipping - session busy")
			s.recordResult(ctx, rt.task.ID, "skipped", nil)
			return
		}
		// Wait up to 30 seconds for idle
		if !s.waitForIdle(ctx, sessionID, 30*time.Second) {
			log.Warn("session still busy, skipping")
			s.recordResult(ctx, rt.task.ID, "skipped", nil)
			return
		}
	}

	result, err := s.agent.HandleWithMiddleware(ctx, rt.task.UserID, rt.task.Channel, rt.task.Prompt, "", agent.MiddlewareSet{})
	if err != nil {
		log.Error("task failed", zap.Error(err))
		s.recordResult(ctx, rt.task.ID, "failed", nil)
		return
	}

	log.Info("task success", zap.Int("result_len", len(result)))
	s.recordResult(ctx, rt.task.ID, "success", nil)
}

func (s *Scheduler) waitForIdle(ctx context.Context, sessionID int64, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			status := s.statusFunc(sessionID)
			if status == nil || status.State == "idle" {
				return true
			}
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

func (s *Scheduler) recordResult(ctx context.Context, taskID int64, status string, nextRun *time.Time) {
	now := time.Now()
	updates := map[string]interface{}{
		"last_run_at":     now,
		"last_run_status": status,
	}
	if nextRun != nil {
		updates["next_run_at"] = nextRun
	}
	if status == "success" {
		updates["successful_runs"] = gorm.Expr("successful_runs + 1")
	} else if status == "failed" {
		updates["failed_runs"] = gorm.Expr("failed_runs + 1")
	}
	s.db.WithContext(ctx).Model(&db.SchedulerTask{}).Where("id = ?", taskID).Updates(updates)
}

func (s *Scheduler) updateNextRun(ctx context.Context, taskID int64, nextRun *time.Time) {
	s.db.WithContext(ctx).Model(&db.SchedulerTask{}).Where("id = ?", taskID).Update("next_run_at", nextRun)
}

// ValidateTask validates a scheduler task.
func ValidateTask(name, cronExpr, prompt string, userID int64) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	if cronExpr == "" {
		return fmt.Errorf("cron_expr required")
	}
	if len(strings.Fields(cronExpr)) != 5 {
		return fmt.Errorf("cron must have 5 fields")
	}
	if prompt == "" {
		return fmt.Errorf("prompt required")
	}
	if userID == 0 {
		return fmt.Errorf("user_id required")
	}
	return nil
}
