package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SchedulerTasksTool lists all scheduled tasks.
type SchedulerTasksTool struct {
	db *gorm.DB
}

func NewSchedulerTasksTool(db *gorm.DB) *SchedulerTasksTool {
	return &SchedulerTasksTool{db: db}
}

func (t *SchedulerTasksTool) Name() string { return "scheduler_tasks" }

func (t *SchedulerTasksTool) Description() string {
	return `List all scheduled tasks with their status.

Returns a summary of all scheduled tasks including:
- Task name and schedule (cron expression)
- Enabled/disabled status
- Last run time and status
- Success/failure counts

Use scheduler_task tool to manage individual tasks.`
}

func (t *SchedulerTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"all": map[string]any{
				"type":        "boolean",
				"description": "Show all tasks including disabled ones (default: false, only shows enabled)",
			},
		},
	}
}

func (t *SchedulerTasksTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args struct {
		All bool `json:"all"`
	}
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var tasks []db.SchedulerTask
	query := t.db.WithContext(ctx).Order("name")
	if !args.All {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Find(&tasks).Error; err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(tasks) == 0 {
		return "No scheduled tasks.", nil
	}

	result := fmt.Sprintf("Scheduled tasks (%d):\n\n", len(tasks))
	for _, task := range tasks {
		status := "✅"
		if !task.Enabled {
			status = "⏸️"
		}
		
		lastRun := "never"
		if task.LastRunAt != nil {
			lastRun = task.LastRunAt.Format("2006-01-02 15:04")
		}
		
		result += fmt.Sprintf("%s **%s** `%s` (last: %s, %d✓/%d✗)\n",
			status, task.Name, task.CronExpr, lastRun, task.SuccessfulRuns, task.FailedRuns)
	}
	
	return result, nil
}

// SchedulerTaskTool manages individual scheduled tasks.
type SchedulerTaskTool struct {
	db *gorm.DB
}

func NewSchedulerTaskTool(db *gorm.DB) *SchedulerTaskTool {
	return &SchedulerTaskTool{db: db}
}

func (t *SchedulerTaskTool) Name() string { return "scheduler_task" }

func (t *SchedulerTaskTool) Description() string {
	return `Manage a scheduled task (create, update, delete, enable, disable).

Actions:
- create: Create a new task with name, cron_expr, prompt, user_id, channel, Optional: skip_if_busy (default: true)
- update: Update task fields (cron_expr, prompt, user_id, channel, skip_if_busy)
- delete: Delete a task permanently
- enable: Enable a disabled task
- disable: Temporarily disable a task without deleting it

Cron expression format: "minute hour day month weekday"
Example: "0 9 * * *" runs at 9:00 AM every day
Example: "*/15 * * * *" runs every 15 minutes
Example: "0 9 * * 1" runs at 9:00 AM every Monday`
}

func (t *SchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action", "name"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "update", "delete", "enable", "disable"},
				"description": "Action to perform",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Task name (unique identifier)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression (e.g., '0 9 * * *' for daily at 9 AM)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Prompt to send when task runs",
			},
			"user_id": map[string]any{
				"type":        "integer",
				"description": "User ID to send prompt to",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Channel (default: 'telegram')",
			},
			"skip_if_busy": map[string]any{
				"type":        "boolean",
				"description": "Skip if session is busy (default: true)",
			},
		},
	}
}

func (t *SchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Name        string `json:"name"`
		CronExpr    string `json:"cron_expr,omitempty"`
		Prompt      string `json:"prompt,omitempty"`
		UserID      *int64 `json:"user_id,omitempty"`
		Channel     string `json:"channel,omitempty"`
		SkipIfBusy  *bool  `json:"skip_if_busy,omitempty"`
	}
	
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	
	switch args.Action {
	case "create":
		return t.createTask(ctx, &args)
	case "update":
		return t.updateTask(ctx, &args)
	case "delete":
		return t.deleteTask(ctx, args.Name)
	case "enable":
		return t.enableTask(ctx, args.Name, true)
	case "disable":
		return t.enableTask(ctx, args.Name, false)
	default:
		return "", fmt.Errorf("unknown action: %s", args.Action)
	}
}

func (t *SchedulerTaskTool) createTask(ctx context.Context, args *struct {
	Action     string `json:"action"`
	Name       string `json:"name"`
	CronExpr   string `json:"cron_expr,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	UserID     *int64 `json:"user_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	SkipIfBusy *bool  `json:"skip_if_busy,omitempty"`
},
) (string, error) {
	if args.CronExpr == "" {
		return "", fmt.Errorf("cron_expr is required for create")
	}
	if args.Prompt == "" {
		return "", fmt.Errorf("prompt is required for create")
	}
	if args.UserID == nil {
		return "", fmt.Errorf("user_id is required for create")
	}
	
	channel := args.Channel
	if channel == "" {
		channel = "telegram"
	}
	
	skipIfBusy := true
	if args.SkipIfBusy != nil {
		skipIfBusy = *args.SkipIfBusy
	}
	
	task := db.SchedulerTask{
		Name:       args.Name,
		CronExpr:   args.CronExpr,
		Prompt:     args.Prompt,
		UserID:     *args.UserID,
		Channel:    channel,
		SkipIfBusy: skipIfBusy,
		Enabled:    true,
	}
	
	if err := t.db.WithContext(ctx).Create(&task).Error; err != nil {
		return "", fmt.Errorf("failed to create task: %w", err)
	}
	
	return fmt.Sprintf("Task '%s' created successfully (ID: %d)", task.Name, task.ID), nil
}

func (t *SchedulerTaskTool) updateTask(ctx context.Context, args *struct {
	Action     string `json:"action"`
	Name       string `json:"name"`
	CronExpr   string `json:"cron_expr,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	UserID     *int64 `json:"user_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	SkipIfBusy *bool  `json:"skip_if_busy,omitempty"`
},
) (string, error) {
	var task db.SchedulerTask
	if err := t.db.WithContext(ctx).Where("name = ?", args.Name).First(&task).Error; err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}
	
	if args.CronExpr != "" {
		task.CronExpr = args.CronExpr
	}
	if args.Prompt != "" {
		task.Prompt = args.Prompt
	}
	if args.UserID != nil {
		task.UserID = *args.UserID
	}
	if args.Channel != "" {
		task.Channel = args.Channel
	}
	if args.SkipIfBusy != nil {
		task.SkipIfBusy = *args.SkipIfBusy
	}
	
	if err := t.db.WithContext(ctx).Save(&task).Error; err != nil {
		return "", fmt.Errorf("failed to update task: %w", err)
	}
	
	return fmt.Sprintf("Task '%s' updated successfully", task.Name), nil
}

func (t *SchedulerTaskTool) deleteTask(ctx context.Context, name string) (string, error) {
	result := t.db.WithContext(ctx).Where("name = ?", name).Delete(&db.SchedulerTask{})
	if result.Error != nil {
		return "", fmt.Errorf("failed to delete task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("task not found: %s", name)
	}
	
	return fmt.Sprintf("Task '%s' deleted successfully", name), nil
}

func (t *SchedulerTaskTool) enableTask(ctx context.Context, name string, enabled bool) (string, error) {
	var task db.SchedulerTask
	if err := t.db.WithContext(ctx).Where("name = ?", name).First(&task).Error; err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}
	
	task.Enabled = enabled
	if err := t.db.WithContext(ctx).Save(&task).Error; err != nil {
		return "", fmt.Errorf("failed to update task: %w", err)
	}
	
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	return fmt.Sprintf("Task '%s' %s successfully", task.Name, action), nil
}
*** End Patch