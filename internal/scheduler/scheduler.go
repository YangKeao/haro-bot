package scheduler
 
 import (
 	"context"
 	"fmt"
 	"sync"
 	"time"
 
 	"github.com/YangKeao/haro-bot/internal/agent"
 	"github.com/YangKeao/haro-bot/internal/logging"
 	"go.uber.org/zap"
	"github.com/YangKeao/haro-bot/internal/config"
 )
 
 // Task defines a scheduled task that can trigger LLM interactions.
 type Task struct {
 	Name        string        // Human-readable name for the task
 	CronExpr    string        // Cron expression (e.g., "0 8 * * *" for 8 AM daily)
 	Prompt      string        // The prompt to send to the LLM
 	UserID      int64         // The user ID to associate with the task
 	Channel     string        // The channel to send the message to
 	Model       string        // Optional model override (empty for default)
 	Notify      bool          // Whether to notify the user of the result
 	MaxWait     time.Duration // Max time to wait if session is busy
 	SkipIfBusy  bool          // Skip execution if session is busy
 }
 
 // Scheduler manages scheduled tasks and their execution.
 type Scheduler struct {
 	tasks      []*Task
 	agent      *agent.Agent
 	statusFunc func(sessionID int64) *agent.SessionStatus
 	getSession func(ctx context.Context, userID int64, channel string) (int64, error)
 	mu         sync.RWMutex
 	running    bool
 	stopCh     chan struct{}
 }
 
 // New creates a new Scheduler.
 func New(ag *agent.Agent, statusFunc func(sessionID int64) *agent.SessionStatus, getSession func(ctx context.Context, userID int64, channel string) (int64, error)) *Scheduler {
 	return &Scheduler{
 		agent:      ag,
 		statusFunc: statusFunc,
 		getSession: getSession,
 		tasks:      make([]*Task, 0),
 		stopCh:     make(chan struct{}),
 	}
 }
 
 // AddTask adds a new task to the scheduler.
 func (s *Scheduler) AddTask(task *Task) {
 	s.mu.Lock()
 	defer s.mu.Unlock()
 	s.tasks = append(s.tasks, task)
 }
 
 // Start begins the scheduler loop.
 func (s *Scheduler) Start(ctx context.Context) {
 	s.mu.Lock()
 	if s.running {
 		s.mu.Unlock()
 		return
 	}
 	s.running = true
 	s.mu.Unlock()
 
 	log := logging.L().Named("scheduler")
 	log.Info("scheduler started", zap.Int("tasks", len(s.tasks)))
 
 	// Start a goroutine for each task
 	for _, task := range s.tasks {
 		go s.runTask(ctx, task)
 	}
 
 	// Wait for stop signal
 	<-s.stopCh
 	log.Info("scheduler stopped")
 }
 
 // Stop stops the scheduler.
 func (s *Scheduler) Stop() {
 	s.mu.Lock()
 	defer s.mu.Unlock()
 	if !s.running {
 		return
 	}
 	s.running = false
 	close(s.stopCh)
 }
 
 // runTask runs a single scheduled task.
 func (s *Scheduler) runTask(ctx context.Context, task *Task) {
 	log := logging.L().Named("scheduler").Named(task.Name)
 
 	schedule, err := ParseCron(task.CronExpr)
 	if err != nil {
 		log.Error("invalid cron expression", zap.String("expr", task.CronExpr), zap.Error(err))
 		return
 	}
 
 	for {
 		next := schedule.Next(time.Now())
 		log.Debug("next execution scheduled", zap.Time("next", next))
 
 		select {
 		case <-ctx.Done():
 			return
 		case <-s.stopCh:
 			return
 		case <-time.After(time.Until(next)):
 			s.executeTask(ctx, task, log)
 		}
 	}
 }
 
 // executeTask executes a single task instance.
 func (s *Scheduler) executeTask(ctx context.Context, task *Task, log *zap.Logger) {
 	log.Info("executing scheduled task")
 
 	// Get the session ID
 	sessionID, err := s.getSession(ctx, task.UserID, task.Channel)
 	if err != nil {
 		log.Error("failed to get session", zap.Error(err))
 		return
 	}
 
 	// Check if session is busy
 	status := s.statusFunc(sessionID)
 	if status != nil && status.State != agent.StateIdle {
 		if task.SkipIfBusy {
 			log.Debug("skipping task, session is busy", zap.String("state", string(status.State)))
 			return
 		}
 
 		// Wait for session to become idle
 		if task.MaxWait > 0 {
 			log.Debug("waiting for session to become idle", zap.Duration("max_wait", task.MaxWait))
 			if !s.waitForIdle(ctx, sessionID, task.MaxWait) {
 				log.Warn("session still busy after wait, skipping task")
 				return
 			}
 		}
 	}
 
 	// Execute the task via the agent
 	output, err := s.agent.HandleWithMiddleware(ctx, task.UserID, task.Channel, task.Prompt, task.Model, agent.MiddlewareSet{})
 	if err != nil {
 		log.Error("task execution failed", zap.Error(err))
 		return
 	}
 
 	log.Info("task completed", zap.Int("output_len", len(output)))
 
 	// If notify is enabled and we have output, we could send a notification here
 	// This would require integration with the IM layer
 	if task.Notify {
 		log.Debug("notification requested but not implemented")
 	}
 }
 
 // waitForIdle waits for the session to become idle.
 func (s *Scheduler) waitForIdle(ctx context.Context, sessionID int64, maxWait time.Duration) bool {
 	deadline := time.Now().Add(maxWait)
 	ticker := time.NewTicker(500 * time.Millisecond)
 	defer ticker.Stop()
 
 	for {
 		select {
 		case <-ctx.Done():
 			return false
 		case <-ticker.C:
 			status := s.statusFunc(sessionID)
 			if status == nil || status.State == agent.StateIdle {
 				return true
 			}
 			if time.Now().After(deadline) {
 				return false
 			}
 		}
 	}
 }
 
 // GetTasks returns a copy of all tasks.
 func (s *Scheduler) GetTasks() []*Task {
 	s.mu.RLock()
 	defer s.mu.RUnlock()
 	result := make([]*Task, len(s.tasks))
 	copy(result, s.tasks)
 	return result
 }
 
 // IsRunning returns whether the scheduler is running.
 func (s *Scheduler) IsRunning() bool {
 	s.mu.RLock()
 	defer s.mu.RUnlock()
 	return s.running
 }
 
 // ValidateTask validates a task configuration.
 func ValidateTask(task *Task) error {
 	if task.Name == "" {
 		return fmt.Errorf("task name is required")
 	}
 	if task.CronExpr == "" {
 		return fmt.Errorf("cron expression is required")
 	}
 	if task.Prompt == "" {
 		return fmt.Errorf("prompt is required")
 	}
 	if task.UserID == 0 {
 		return fmt.Errorf("user ID is required")
 	}
 	if _, err := ParseCron(task.CronExpr); err != nil {
 		return fmt.Errorf("invalid cron expression: %w", err)
 	}
 	return nil
 }
// ParseTaskConfig converts a config task into a scheduler Task.
func ParseTaskConfig(cfg config.SchedulerTaskConfig) (*Task, error) {
	task := &Task{
		Name:       cfg.Name,
		CronExpr:   cfg.CronExpr,
		Prompt:     cfg.Prompt,
		UserID:     cfg.UserID,
		Channel:    cfg.Channel,
		Model:      cfg.Model,
		Notify:     cfg.Notify,
		SkipIfBusy: cfg.SkipIfBusy,
	}
	if cfg.MaxWait != "" {
		d, err := time.ParseDuration(cfg.MaxWait)
		if err != nil {
			return nil, fmt.Errorf("invalid max_wait: %w", err)
		}
		task.MaxWait = d
	}
	return task, nil
}
