package tools

import (
	"strings"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SchedulerTasksTool lists and manages scheduled tasks.
type SchedulerTasksTool struct {
	db *gorm.DB
}

func NewSchedulerTasksTool(db *gorm.DB) *SchedulerTasksTool {
	return &SchedulerTasksTool{db: db}
}

func (t *SchedulerTasksTool) Name() string { return "scheduler_tasks" }

func (t *SchedulerTasksTool) Description() string {
	return "List all scheduled tasks, or get details of a specific task by name or id."
}

func (t *SchedulerTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Task name to get details (optional, lists all if not provided)"},
			"id":   map[string]any{"type": "integer", "description": "Task ID to get details (optional)"},
		},
	}
}

func (t *SchedulerTasksTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args struct {
		Name string `json:"name,omitempty"`
		ID   *int64 `json:"id,omitempty"`
	}
	json.Unmarshal(argsJSON, &args)

	if args.Name != "" || args.ID != nil {
		return t.getTask(ctx, args.Name, args.ID)
	}
	return t.listTasks(ctx)
}

func (t *SchedulerTasksTool) listTasks(ctx context.Context) (string, error) {
	var tasks []db.SchedulerTask
	if err := t.db.WithContext(ctx).Find(&tasks).Error; err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "No scheduled tasks.", nil
	}

	result := fmt.Sprintf("Scheduled tasks (%d):\n", len(tasks))
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

func (t *SchedulerTasksTool) getTask(ctx context.Context, name string, id *int64) (string, error) {
	var task db.SchedulerTask
	if id != nil {
		if err := t.db.First(&task, *id).Error; err != nil {
			return "", fmt.Errorf("task not found: %w", err)
		}
	} else {
		if err := t.db.Where("name = ?", name).First(&task).Error; err != nil {
			return "", fmt.Errorf("task not found: %w", err)
		}
	}

	result := fmt.Sprintf("**%s** (ID: %d)\n", task.Name, task.ID)
	result += fmt.Sprintf("- Schedule: `%s`\n", task.CronExpr)
	result += fmt.Sprintf("- Status: %s\n", map[bool]string{true: "enabled", false: "disabled"}[task.Enabled])
	result += fmt.Sprintf("- User/Channel: %d/%s\n", task.UserID, task.Channel)
	result += fmt.Sprintf("- Skip if busy: %v\n", task.SkipIfBusy)
	result += fmt.Sprintf("- Runs: %d success, %d failed\n", task.SuccessfulRuns, task.FailedRuns)
	if task.LastRunAt != nil {
		result += fmt.Sprintf("- Last run: %s (%s)\n", task.LastRunAt.Format(time.RFC3339), task.LastRunStatus)
	}
	if task.NextRunAt != nil {
		result += fmt.Sprintf("- Next run: %s\n", task.NextRunAt.Format(time.RFC3339))
	}
	result += fmt.Sprintf("- Prompt:\n```\n%s\n```\n", task.Prompt)
	return result, nil
}

// SchedulerTaskTool creates, updates, or deletes a scheduled task.
type SchedulerTaskTool struct {
	db *gorm.DB
}

func NewSchedulerTaskTool(db *gorm.DB) *SchedulerTaskTool {
	return &SchedulerTaskTool{db: db}
}

func (t *SchedulerTaskTool) Name() string { return "scheduler_task" }

func (t *SchedulerTaskTool) Description() string {
	return "Create, update, or delete a scheduled task. Set action='delete' to remove, action='disable' to pause."
}

func (t *SchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action", "name"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "update", "delete", "disable", "enable"},
				"description": "Action to perform",
			},
			"name":        map[string]any{"type": "string", "description": "Task name"},
			"cron_expr":   map[string]any{"type": "string", "description": "Cron expression (e.g., '0 8 * * *')"},
			"prompt":      map[string]any{"type": "string", "description": "Prompt to send"},
			"user_id":     map[string]any{"type": "integer", "description": "User ID"},
			"channel":     map[string]any{"type": "string", "description": "Channel (default: telegram)"},
			"skip_if_busy": map[string]any{"type": "boolean", "description": "Skip if session busy"},
		},
	}
}

func (t *SchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Name        string `json:"name"`
		CronExpr    string `json:"cron_expr,omitempty"`
		Prompt      string `json:"prompt,omitempty"`
		UserID      int64  `json:"user_id,omitempty"`
		Channel     string `json:"channel,omitempty"`
		SkipIfBusy  *bool  `json:"skip_if_busy,omitempty"`
	}
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	log := logging.L().Named("scheduler_tool")
	log.Info("executing", zap.String("action", args.Action), zap.String("name", args.Name))

	switch args.Action {
	case "create":
		return t.create(ctx, &args)
	case "update":
		return t.update(ctx, &args)
	case "delete":
		return t.delete(ctx, args.Name)
	case "disable":
		return t.setEnable(ctx, args.Name, false)
	case "enable":
		return t.setEnable(ctx, args.Name, true)
	default:
		return "", fmt.Errorf("unknown action: %s", args.Action)
	}
}

func (t *SchedulerTaskTool) create(ctx context.Context, args *struct {
	Action     string `json:"action"`
	Name       string `json:"name"`
	CronExpr   string `json:"cron_expr,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	UserID     int64  `json:"user_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	SkipIfBusy *bool  `json:"skip_if_busy,omitempty"`
}) (string, error) {
	channel := args.Channel
	if channel == "" {
		channel = "telegram"
	}
	if err := validateTask(args.Name, args.CronExpr, args.Prompt, args.UserID); err != nil {
		return "", err
	}

	task := &db.SchedulerTask{
		Name:       args.Name,
		CronExpr:   args.CronExpr,
		Prompt:     args.Prompt,
		UserID:     args.UserID,
		Channel:    channel,
		Enabled:    true,
		SkipIfBusy: args.SkipIfBusy != nil && *args.SkipIfBusy,
	}

	if err := t.db.Create(task).Error; err != nil {
		return "", fmt.Errorf("create failed: %w", err)
	}
	return fmt.Sprintf("Created task '%s' (ID: %d) with schedule `%s`", task.Name, task.ID, task.CronExpr), nil
}

func (t *SchedulerTaskTool) update(ctx context.Context, args *struct {
	Action     string `json:"action"`
	Name       string `json:"name"`
	CronExpr   string `json:"cron_expr,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	UserID     int64  `json:"user_id,omitempty"`
	Channel    string `json:"channel,omitempty"`
	SkipIfBusy *bool  `json:"skip_if_busy,omitempty"`
}) (string, error) {
	var task db.SchedulerTask
	if err := t.db.Where("name = ?", args.Name).First(&task).Error; err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	if args.CronExpr != "" {
		if err := validateTask(args.Name, args.CronExpr, task.Prompt, task.UserID); err != nil {
			return "", err
		}
		task.CronExpr = args.CronExpr
	}
	if args.Prompt != "" {
		task.Prompt = args.Prompt
	}
	if args.UserID != 0 {
		task.UserID = args.UserID
	}
	if args.Channel != "" {
		task.Channel = args.Channel
	}
	if args.SkipIfBusy != nil {
		task.SkipIfBusy = *args.SkipIfBusy
	}

	if err := t.db.Save(&task).Error; err != nil {
		return "", fmt.Errorf("update failed: %w", err)
	}
	return fmt.Sprintf("Updated task '%s'", task.Name), nil
}

func (t *SchedulerTaskTool) delete(ctx context.Context, name string) (string, error) {
	result := t.db.Where("name = ?", name).Delete(&db.SchedulerTask{})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("task not found: %s", name)
	}
	return fmt.Sprintf("Deleted task '%s'", name), nil
}

func (t *SchedulerTaskTool) setEnable(ctx context.Context, name string, enabled bool) (string, error) {
	result := t.db.Model(&db.SchedulerTask{}).Where("name = ?", name).Update("enabled", enabled)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("task not found: %s", name)
	}
	action := "Enabled"
	if !enabled {
		action = "Disabled"
	}
	return fmt.Sprintf("%s task '%s'", action, name), nil
}

func validateTask(name, cronExpr, prompt string, userID int64) error {
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
