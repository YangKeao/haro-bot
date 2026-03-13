package store

import (
	"context"
	"time"

	"github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/gorm"
)

// Store provides database operations for scheduler tasks.
type Store struct {
	db *gorm.DB
}

// NewStore creates a new scheduler store.
func NewStore(database *gorm.DB) *Store {
	return &Store{db: database}
}

// ListTasks returns all scheduler tasks.
func (s *Store) ListTasks(ctx context.Context) ([]db.SchedulerTask, error) {
	var tasks []db.SchedulerTask
	err := s.db.WithContext(ctx).Find(&tasks).Error
	return tasks, err
}

// ListEnabledTasks returns all enabled scheduler tasks.
func (s *Store) ListEnabledTasks(ctx context.Context) ([]db.SchedulerTask, error) {
	var tasks []db.SchedulerTask
	err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&tasks).Error
	return tasks, err
}

// GetTask returns a task by ID.
func (s *Store) GetTask(ctx context.Context, id int64) (*db.SchedulerTask, error) {
	var task db.SchedulerTask
	err := s.db.WithContext(ctx).First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTaskByName returns a task by name.
func (s *Store) GetTaskByName(ctx context.Context, name string) (*db.SchedulerTask, error) {
	var task db.SchedulerTask
	err := s.db.WithContext(ctx).Where("name = ?", name).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// CreateTask creates a new scheduler task.
func (s *Store) CreateTask(ctx context.Context, task *db.SchedulerTask) error {
	return s.db.WithContext(ctx).Create(task).Error
}

// UpdateTask updates an existing scheduler task.
func (s *Store) UpdateTask(ctx context.Context, task *db.SchedulerTask) error {
	return s.db.WithContext(ctx).Save(task).Error
}

// DeleteTask deletes a scheduler task by ID.
func (s *Store) DeleteTask(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&db.SchedulerTask{}, id).Error
}

// UpdateTaskRunStatus updates the task's run status and counters.
func (s *Store) UpdateTaskRunStatus(ctx context.Context, id int64, status string, errMsg string, nextRunAt *time.Time) error {
	updates := map[string]interface{}{
		"last_run_at":     time.Now(),
		"last_run_status": status,
		"last_run_error":  errMsg,
		"next_run_at":     nextRunAt,
	}
	if status == "success" {
		updates["successful_runs"] = gorm.Expr("successful_runs + 1")
	} else if status == "failed" {
		updates["failed_runs"] = gorm.Expr("failed_runs + 1")
	}
	return s.db.WithContext(ctx).Model(&db.SchedulerTask{}).Where("id = ?", id).Updates(updates).Error
}

// CreateExecution creates a new task execution record.
func (s *Store) CreateExecution(ctx context.Context, exec *db.SchedulerTaskExecution) error {
	return s.db.WithContext(ctx).Create(exec).Error
}

// UpdateExecution updates an execution record.
func (s *Store) UpdateExecution(ctx context.Context, exec *db.SchedulerTaskExecution) error {
	return s.db.WithContext(ctx).Save(exec).Error
}

// ListExecutions returns recent executions for a task.
func (s *Store) ListExecutions(ctx context.Context, taskID int64, limit int) ([]db.SchedulerTaskExecution, error) {
	var executions []db.SchedulerTaskExecution
	err := s.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("started_at DESC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}
