package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/go-co-op/gocron/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scheduler manages scheduled tasks using gocron.
type Scheduler struct {
	agent     *agent.Agent
	store     memory.StoreAPI
	db        *gorm.DB
	scheduler gocron.Scheduler
	mu        sync.RWMutex
	jobs      map[int64]gocron.Job // task ID -> job
}

// Config holds scheduler configuration.
type Config struct {
	SyncInterval time.Duration // How often to sync with database
}

// DefaultConfig returns default scheduler configuration.
func DefaultConfig() Config {
	return Config{
		SyncInterval: 30 * time.Second,
	}
}

// New creates a new scheduler.
func New(cfg Config, ag *agent.Agent, store memory.StoreAPI, database *gorm.DB) *Scheduler {
	s, _ := gocron.NewScheduler()
	return &Scheduler{
		agent:     ag,
		store:     store,
		db:        database,
		scheduler: s,
		jobs:      make(map[int64]gocron.Job),
	}
}

// Start begins the scheduler and starts syncing with database.
func (s *Scheduler) Start(ctx context.Context) {
	log := logging.L().Named("scheduler")
	log.Info("scheduler starting")

	// Load enabled tasks
	var tasks []db.SchedulerTask
	if err := s.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error("failed to load tasks", zap.Error(err))
		return
	}

	s.mu.Lock()
	for _, task := range tasks {
		if err := s.addJob(ctx, &task); err != nil {
			log.Error("failed to add job",
				zap.String("name", task.Name),
				zap.Error(err))
		}
	}
	s.mu.Unlock()

	s.scheduler.Start()
	log.Info("scheduler started", zap.Int("tasks", len(tasks)))

	// Sync loop
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				s.scheduler.Shutdown()
				log.Info("scheduler stopped")
				return
			case <-ticker.C:
				s.sync(ctx)
			}
		}
	}()
}

// addJob adds a new job to the scheduler.
func (s *Scheduler) addJob(ctx context.Context, task *db.SchedulerTask) error {
	log := logging.L().Named("scheduler")

	// Check if job already exists
	s.mu.RLock()
	_, exists := s.jobs[task.ID]
	s.mu.RUnlock()
	if exists {
		return fmt.Errorf("job already exists for task %d", task.ID)
	}

	// Capture task values for closure
	taskID := task.ID
	userID := task.UserID
	channel := task.Channel
	prompt := task.Prompt
	skipIfBusy := task.SkipIfBusy

	job, err := s.scheduler.NewJob(
		gocron.CronJob(task.CronExpr, false),
		gocron.NewTask(func() {
			s.execute(ctx, taskID, userID, channel, prompt, skipIfBusy)
		}),
		gocron.WithName(task.Name),
		gocron.WithTags(fmt.Sprintf("task:%d", task.ID)),
	)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	s.mu.Lock()
	s.jobs[task.ID] = job
	s.mu.Unlock()

	// Update next run time
	nextRun := job.NextRun()
	if nextRun != nil {
		s.db.Model(task).Update("next_run_at", nextRun)
	}

	log.Info("task added", zap.String("name", task.Name), zap.String("cron", task.CronExpr))
	return nil
}

// execute runs a scheduled task.
func (s *Scheduler) execute(ctx context.Context, taskID, userID int64, channel, prompt string, skipIfBusy bool) {
	log := logging.L().Named("scheduler")

	taskLogger := log.With(
		zap.Int64("task_id", taskID),
		zap.Int64("user_id", userID),
		zap.String("channel", channel),
	)

	taskLogger.Info("executing scheduled task")

	// Get or create session
	sessionID, err := s.store.GetOrCreateSession(ctx, userID, channel)
	if err != nil {
		taskLogger.Error("failed to get session", zap.Error(err))
		s.recordFailure(taskID, "session_error", err.Error())
		return
	}

	// Check if session is busy
	if skipIfBusy {
		status := s.agent.GetSessionStatus(sessionID)
		if status != nil && status.State != "idle" {
			taskLogger.Info("skipping task, session busy",
				zap.String("state", status.State))
			s.recordSkip(taskID)
			return
		}
	}

	// Execute with retry
	var lastErr error
	for retry := 0; retry <= 3; retry++ {
		if retry > 0 {
			taskLogger.Info("retrying task", zap.Int("retry", retry))
			time.Sleep(time.Duration(retry) * time.Second) // Exponential backoff simplified
		}

		_, err := s.agent.Handle(ctx, userID, channel, prompt)
		if err == nil {
			taskLogger.Info("task completed successfully")
			s.recordSuccess(taskID)
			return
		}
		lastErr = err
		taskLogger.Warn("task execution failed",
			zap.Int("retry", retry),
			zap.Error(err))
	}

	// All retries exhausted
	taskLogger.Error("task failed after retries", zap.Int("max_retries", 3), zap.Error(lastErr))
	s.recordFailure(taskID, "execution_error", lastErr.Error())
}

// recordSuccess records a successful task execution.
func (s *Scheduler) recordSuccess(taskID int64) {
	now := time.Now()
	s.db.Model(&db.SchedulerTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"last_run_at":     now,
		"last_run_status": "success",
		"retry_count":     0,
		"successful_runs": gorm.Expr("successful_runs + 1"),
		"last_run_error":  "",
	})
}

// recordFailure records a failed task execution.
func (s *Scheduler) recordFailure(taskID int64, status, errorMsg string) {
	now := time.Now()
	s.db.Model(&db.SchedulerTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"last_run_at":     now,
		"last_run_status": status,
		"retry_count":     gorm.Expr("retry_count + 1"),
		"failed_runs":     gorm.Expr("failed_runs + 1"),
		"last_run_error":  errorMsg,
	})
}

// recordSkip records a skipped task execution.
func (s *Scheduler) recordSkip(taskID int64) {
	now := time.Now()
	s.db.Model(&db.SchedulerTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"last_run_at":     now,
		"last_run_status": "skipped",
	})
}

// sync synchronizes tasks with database.
func (s *Scheduler) sync(ctx context.Context) {
	log := logging.L().Named("scheduler")

	var tasks []db.SchedulerTask
	if err := s.db.Find(&tasks).Error; err != nil {
		log.Error("failed to sync tasks", zap.Error(err))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build map of current tasks
	currentTasks := make(map[int64]bool)
	for _, task := range tasks {
		currentTasks[task.ID] = true

		// Add new or update existing tasks
		if task.Enabled {
			if _, exists := s.jobs[task.ID]; !exists {
				if err := s.addJob(ctx, &task); err != nil {
					log.Error("failed to add job during sync",
						zap.String("name", task.Name),
						zap.Error(err))
				}
			}
		}
	}

	// Remove jobs for deleted or disabled tasks
	for taskID, job := range s.jobs {
		if !currentTasks[taskID] {
			s.scheduler.RemoveJob(job)
			delete(s.jobs, taskID)
			log.Info("removed job for deleted task", zap.Int64("task_id", taskID))
		}
	}
}

// ScheduleTask creates or updates a scheduled task.
func (s *Scheduler) ScheduleTask(ctx context.Context, task *db.SchedulerTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Save to database
	if err := s.db.Save(task).Error; err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	// Add or update job
	if task.Enabled {
		// Remove existing job if any
		if job, exists := s.jobs[task.ID]; exists {
			s.scheduler.RemoveJob(job)
			delete(s.jobs, task.ID)
		}

		// Add new job
		if err := s.addJob(ctx, task); err != nil {
			return fmt.Errorf("failed to schedule task: %w", err)
		}
	}

	return nil
}

// CancelTask removes a scheduled task.
func (s *Scheduler) CancelTask(ctx context.Context, taskID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove job from scheduler
	if job, exists := s.jobs[taskID]; exists {
		s.scheduler.RemoveJob(job)
		delete(s.jobs, taskID)
	}

	// Delete from database
	if err := s.db.Delete(&db.SchedulerTask{}, taskID).Error; err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	return nil
}

// ListTasks returns all scheduled tasks.
func (s *Scheduler) ListTasks(ctx context.Context) ([]db.SchedulerTask, error) {
	var tasks []db.SchedulerTask
	if err := s.db.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
}

// GetTask returns a specific task.
func (s *Scheduler) GetTask(ctx context.Context, taskID int64) (*db.SchedulerTask, error) {
	var task db.SchedulerTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return &task, nil
}

// EnableTask enables or disables a task.
func (s *Scheduler) EnableTask(ctx context.Context, taskID int64, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var task db.SchedulerTask
	if err := s.db.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	task.Enabled = enabled
	if err := s.db.Save(&task).Error; err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	if enabled {
		// Add job
		if err := s.addJob(ctx, &task); err != nil {
			return fmt.Errorf("failed to enable task: %w", err)
		}
	} else {
		// Remove job
		if job, exists := s.jobs[taskID]; exists {
			s.scheduler.RemoveJob(job)
			delete(s.jobs, taskID)
		}
	}

	return nil
}
