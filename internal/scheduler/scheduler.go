package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-co-op/gocron/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scheduler manages scheduled tasks using gocron.
type Scheduler struct {
	agent      *agent.Agent
	statusFunc func(sessionID int64) *agent.SessionStatus
	getSession func(ctx context.Context, userID int64, channel string) (int64, error)
	db         *gorm.DB
	scheduler  gocron.Scheduler
}

// New creates a new scheduler.
func New(
	ag *agent.Agent,
	statusFunc func(sessionID int64) *agent.SessionStatus,
	getSession func(ctx context.Context, userID int64, channel string) (int64, error),
	database *gorm.DB,
) *Scheduler {
	s, _ := gocron.NewScheduler()
	return &Scheduler{
		agent:      ag,
		statusFunc: statusFunc,
		getSession: getSession,
		db:         database,
		scheduler:  s,
	}
}

// Start begins the scheduler.
func (s *Scheduler) Start(ctx context.Context) {
	log := logging.L().Named("scheduler")
	log.Info("scheduler starting")

	// Load enabled tasks
	var tasks []db.SchedulerTask
	if err := s.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error("failed to load tasks", zap.Error(err))
		return
	}

	for _, task := range tasks {
		s.addJob(ctx, &task)
	}

	s.scheduler.Start()
	log.Info("scheduler started", zap.Int("tasks", len(tasks)))

	// Sync loop
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.scheduler.Shutdown()
				return
			case <-ticker.C:
				s.sync(ctx)
			}
		}
	}()
}

func (s *Scheduler) addJob(ctx context.Context, task *db.SchedulerTask) {
	log := logging.L().Named("scheduler")

	// Capture task values for closure
	taskID := task.ID
	userID := task.UserID
	channel := task.Channel
	prompt := task.Prompt
	skipIfBusy := task.SkipIfBusy

	_, err := s.scheduler.NewJob(
		gocron.CronJob(task.CronExpr, false),
		gocron.NewTask(func() {
			s.execute(ctx, taskID, userID, channel, prompt, skipIfBusy)
		}),
		gocron.WithName(task.Name),
	)
	if err != nil {
		log.Error("failed to add job", zap.String("name", task.Name), zap.Error(err))
		return
	}
	log.Info("task added", zap.String("name", task.Name), zap.String("cron", task.CronExpr))
}

func (s *Scheduler) sync(ctx context.Context) {
	log := logging.L().Named("scheduler")

	// Get current jobs
	jobs := s.scheduler.Jobs()
	jobNames := make(map[string]bool)
	for _, j := range jobs {
		jobNames[j.Name()] = true
	}

	// Get tasks from DB
	var tasks []db.SchedulerTask
	if err := s.db.Where("enabled = ?", true).Find(&tasks).Error; err != nil {
		log.Error("failed to sync", zap.Error(err))
		return
	}

	// Add new/updated tasks
	for _, task := range tasks {
		if !jobNames[task.Name] {
			s.addJob(ctx, &task)
		}
	}

	// Remove deleted/disabled tasks
	for _, j := range jobs {
		var count int64
		s.db.Model(&db.SchedulerTask{}).Where("name = ? AND enabled = ?", j.Name(), true).Count(&count)
		if count == 0 {
			s.scheduler.RemoveJob(j.ID())
			log.Info("task removed", zap.String("name", j.Name()))
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, taskID, userID int64, channel, prompt string, skipIfBusy bool) {
	log := logging.L().Named("scheduler").With(zap.Int64("task_id", taskID))
	log.Info("executing")

	sessionID, err := s.getSession(ctx, userID, channel)
	if err != nil {
		log.Error("failed to get session", zap.Error(err))
		s.recordResult(taskID, "failed")
		return
	}

	// Check if busy
	status := s.statusFunc(sessionID)
	if status != nil && status.State != "idle" {
		if skipIfBusy {
			log.Info("skipping - busy")
			s.recordResult(taskID, "skipped")
			return
		}
		if !s.waitForIdle(ctx, sessionID, 30*time.Second) {
			log.Warn("still busy, skipping")
			s.recordResult(taskID, "skipped")
			return
		}
	}

	// Execute
	_, err = s.agent.HandleWithMiddleware(ctx, userID, channel, prompt, "", agent.MiddlewareSet{})
	if err != nil {
		log.Error("failed", zap.Error(err))
		s.recordResult(taskID, "failed")
		return
	}

	log.Info("success")
	s.recordResult(taskID, "success")
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

func (s *Scheduler) recordResult(taskID int64, status string) {
	now := time.Now()
	updates := map[string]any{
		"last_run_at":     now,
		"last_run_status": status,
	}
	if status == "success" {
		updates["successful_runs"] = gorm.Expr("successful_runs + 1")
	} else if status == "failed" {
		updates["failed_runs"] = gorm.Expr("failed_runs + 1")
	}
	s.db.Model(&db.SchedulerTask{}).Where("id = ?", taskID).Updates(updates)
}

// ValidateTask validates task parameters.
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
