package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/scheduler/store"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scheduler manages scheduled tasks that trigger LLM interactions.
type Scheduler struct {
	agent      *agent.Agent
	statusFunc func(sessionID int64) *agent.SessionStatus
	getSession func(ctx context.Context, userID int64, channel string) (int64, error)
	store      *store.Store
	db         *gorm.DB

	mu      sync.RWMutex
	tasks   map[int64]*runningTask
	running bool
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
		store:      store.NewStore(database),
		db:         database,
		tasks:      make(map[int64]*runningTask),
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start(ctx context.Context) {
	log := logging.L().Named("scheduler")
	log.Info("scheduler starting")

	// Load enabled tasks from database
	s.loadTasks(ctx)

	// Start ticker to check for task updates
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
	tasks, err := s.store.ListEnabledTasks(ctx)
	if err != nil {
		logging.L().Named("scheduler").Error("failed to load tasks", zap.Error(err))
		return
	}

	for _, task := range tasks {
		s.startTask(&task)
	}
}

func (s *Scheduler) syncTasks(ctx context.Context) {
	log := logging.L().Named("scheduler")
	tasks, err := s.store.ListEnabledTasks(ctx)
	if err != nil {
		log.Error("failed to sync tasks", zap.Error(err))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build map of current tasks
	currentTasks := make(map[int64]bool)
	for _, task := range tasks {
		currentTasks[task.ID] = true
	}

	// Stop tasks that are no longer enabled or updated
	for id, rt := range s.tasks {
		if !currentTasks[id] {
			log.Info("stopping removed/disabled task", zap.String("name", rt.task.Name))
			rt.cancel()
			delete(s.tasks, id)
			continue
		}

		// Check if task was updated
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
	// Calculate next run time
	now := time.Now()
	nextRun := rt.schedule.Next(now)

	// Update next_run_at in database
	s.store.UpdateTaskRunStatus(ctx, rt.task.ID, rt.task.LastRunStatus, rt.task.LastRunError, &nextRun)

	for {
		// Calculate wait duration
		waitDuration := time.Until(nextRun)

		if waitDuration <= 0 {
			// Time to run
			s.executeTask(ctx, rt)
			nextRun = rt.schedule.Next(time.Now())
			s.store.UpdateTaskRunStatus(ctx, rt.task.ID, rt.task.LastRunStatus, rt.task.LastRunError, &nextRun)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(waitDuration):
			s.executeTask(ctx, rt)
			nextRun = rt.schedule.Next(time.Now())
			s.store.UpdateTaskRunStatus(ctx, rt.task.ID, rt.task.LastRunStatus, rt.task.LastRunError, &nextRun)
		}
	}
}

func (s *Scheduler) executeTask(ctx context.Context, rt *runningTask) {
	log := logging.L().Named("scheduler").With(zap.String("task", rt.task.Name))
	log.Info("executing scheduled task")

	// Create execution record
	exec := &db.SchedulerTaskExecution{
		TaskID:    rt.task.ID,
		StartedAt: time.Now(),
		Status:    "running",
	}
	if err := s.store.CreateExecution(ctx, exec); err != nil {
		log.Error("failed to create execution record", zap.Error(err))
	}

	// Check if session is busy
	sessionID, err := s.getSession(ctx, rt.task.UserID, rt.task.Channel)
	if err != nil {
		log.Error("failed to get session", zap.Error(err))
		s.recordExecutionResult(ctx, exec, "failed", "", "failed to get session: "+err.Error())
		s.store.UpdateTaskRunStatus(ctx, rt.task.ID, "failed", "failed to get session", nil)
		return
	}

	status := s.statusFunc(sessionID)
	if status != nil && status.State != "idle" {
		if rt.task.SkipIfBusy {
			log.Info("skipping task - session is busy")
			s.recordExecutionResult(ctx, exec, "skipped", "session busy", "")
			s.store.UpdateTaskRunStatus(ctx, rt.task.ID, "skipped", "", nil)
			return
		}

		// Wait for session to become idle
		maxWait := time.Duration(rt.task.MaxWaitSeconds) * time.Second
		if maxWait > 0 {
			if !s.waitForIdle(ctx, sessionID, maxWait) {
				log.Warn("session still busy after max wait, skipping")
				s.recordExecutionResult(ctx, exec, "skipped", "session busy after wait", "")
				s.store.UpdateTaskRunStatus(ctx, rt.task.ID, "skipped", "session busy", nil)
				return
			}
		}
	}

	// Execute the prompt
	var modelOverride string
	if rt.task.Model != "" {
		modelOverride = rt.task.Model
	}

	// Use empty middleware set for scheduled tasks
	result, err := s.agent.HandleWithMiddleware(ctx, rt.task.UserID, rt.task.Channel, rt.task.Prompt, modelOverride, agent.MiddlewareSet{})
	if err != nil {
		log.Error("task execution failed", zap.Error(err))
		s.recordExecutionResult(ctx, exec, "failed", "", err.Error())
		s.store.UpdateTaskRunStatus(ctx, rt.task.ID, "failed", err.Error(), nil)
		return
	}

	log.Info("task executed successfully", zap.Int("result_len", len(result)))
	s.recordExecutionResult(ctx, exec, "success", result, "")
	s.store.UpdateTaskRunStatus(ctx, rt.task.ID, "success", "", nil)
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

func (s *Scheduler) recordExecutionResult(ctx context.Context, exec *db.SchedulerTaskExecution, status, result, errMsg string) {
	now := time.Now()
	exec.CompletedAt = &now
	exec.Status = status
	exec.Result = result
	exec.ErrorMessage = errMsg
	if err := s.store.UpdateExecution(ctx, exec); err != nil {
		logging.L().Named("scheduler").Error("failed to update execution record", zap.Error(err))
	}
}

// ValidateTask validates a scheduler task configuration.
func ValidateTask(name, cronExpr, prompt string, userID int64) error {
	if name == "" {
		return fmt.Errorf("task name is required")
	}
	if cronExpr == "" {
		return fmt.Errorf("cron expression is required")
	}
	if _, err := ParseCron(cronExpr); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	if prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if userID == 0 {
		return fmt.Errorf("user ID is required")
	}
	return nil
}
