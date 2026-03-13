package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/scheduler/store"
)

// ListSchedulerTasksTool lists all scheduler tasks.
type ListSchedulerTasksTool struct {
	store *store.Store
}

func NewListSchedulerTasksTool(s *store.Store) *ListSchedulerTasksTool {
	return &ListSchedulerTasksTool{store: s}
}

func (t *ListSchedulerTasksTool) Name() string {
	return "list_scheduler_tasks"
}

func (t *ListSchedulerTasksTool) Description() string {
	return "List all scheduled tasks. Returns task names, schedules, and status."
}

func (t *ListSchedulerTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
}

func (t *ListSchedulerTasksTool) Execute(ctx context.Context, _ ToolContext, _ json.RawMessage) (string, error) {
	tasks, err := t.store.ListTasks(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(tasks) == 0 {
		return "No scheduled tasks found.", nil
	}

	result := fmt.Sprintf("Found %d scheduled tasks:\n\n", len(tasks))
	for _, task := range tasks {
		status := "enabled"
		if !task.Enabled {
			status = "disabled"
		}
		lastRun := "never"
		if task.LastRunAt != nil {
			lastRun = task.LastRunAt.Format(time.RFC3339)
		}
		nextRun := "not scheduled"
		if task.NextRunAt != nil {
			nextRun = task.NextRunAt.Format(time.RFC3339)
		}

		result += fmt.Sprintf("- **%s** (ID: %d)\n", task.Name, task.ID)
		result += fmt.Sprintf("  - Schedule: `%s`\n", task.CronExpr)
		result += fmt.Sprintf("  - Status: %s\n", status)
		result += fmt.Sprintf("  - Last run: %s (%s)\n", lastRun, task.LastRunStatus)
		result += fmt.Sprintf("  - Next run: %s\n", nextRun)
		result += fmt.Sprintf("  - Success/Fail: %d/%d\n\n", task.SuccessfulRuns, task.FailedRuns)
	}

	return result, nil
}

// CreateSchedulerTaskTool creates a new scheduler task.
type CreateSchedulerTaskTool struct {
	store *store.Store
}

func NewCreateSchedulerTaskTool(s *store.Store) *CreateSchedulerTaskTool {
	return &CreateSchedulerTaskTool{store: s}
}

func (t *CreateSchedulerTaskTool) Name() string {
	return "create_scheduler_task"
}

func (t *CreateSchedulerTaskTool) Description() string {
	return "Create a new scheduled task that triggers an LLM prompt at specified times."
}

func (t *CreateSchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique name for the task",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression (e.g., '0 8 * * *' for 8 AM daily)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The prompt to send to the LLM",
			},
			"user_id": map[string]any{
				"type":        "integer",
				"description": "User ID to associate with the task",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Channel to send messages to (e.g., 'telegram')",
				"default":     "telegram",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override",
			},
			"notify": map[string]any{
				"type":        "boolean",
				"description": "Whether to notify user of results",
				"default":     true,
			},
			"skip_if_busy": map[string]any{
				"type":        "boolean",
				"description": "Skip execution if session is busy",
				"default":     false,
			},
			"max_wait_seconds": map[string]any{
				"type":        "integer",
				"description": "Max seconds to wait if session is busy (0 = no wait)",
				"default":     0,
			},
		},
		"required": []string{"name", "cron_expr", "prompt", "user_id"},
	}
}

type createTaskArgs struct {
	Name           string `json:"name"`
	CronExpr       string `json:"cron_expr"`
	Prompt         string `json:"prompt"`
	UserID         int64  `json:"user_id"`
	Channel        string `json:"channel,omitempty"`
	Model          string `json:"model,omitempty"`
	Notify         *bool  `json:"notify,omitempty"`
	SkipIfBusy     *bool  `json:"skip_if_busy,omitempty"`
	MaxWaitSeconds *int   `json:"max_wait_seconds,omitempty"`
}

func (t *CreateSchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args createTaskArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	channel := "telegram"
	if args.Channel != "" {
		channel = args.Channel
	}

	// Validate
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
		Notify:     true,
		SkipIfBusy: false,
	}

	if args.Model != "" {
		task.Model = args.Model
	}
	if args.Notify != nil {
		task.Notify = *args.Notify
	}
	if args.SkipIfBusy != nil {
		task.SkipIfBusy = *args.SkipIfBusy
	}
	if args.MaxWaitSeconds != nil {
		task.MaxWaitSeconds = *args.MaxWaitSeconds
	}

	if err := t.store.CreateTask(ctx, task); err != nil {
		return "", fmt.Errorf("failed to create task: %w", err)
	}

	return fmt.Sprintf("Created scheduled task '%s' (ID: %d) with schedule `%s`", task.Name, task.ID, task.CronExpr), nil
}

// UpdateSchedulerTaskTool updates an existing scheduler task.
type UpdateSchedulerTaskTool struct {
	store *store.Store
}

func NewUpdateSchedulerTaskTool(s *store.Store) *UpdateSchedulerTaskTool {
	return &UpdateSchedulerTaskTool{store: s}
}

func (t *UpdateSchedulerTaskTool) Name() string {
	return "update_scheduler_task"
}

func (t *UpdateSchedulerTaskTool) Description() string {
	return "Update an existing scheduled task. Provide task ID or name to identify the task."
}

func (t *UpdateSchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "integer",
				"description": "Task ID to update",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Task name (alternative to ID)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "New cron expression",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "New prompt",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable or disable the task",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model override (empty string to clear)",
			},
			"skip_if_busy": map[string]any{
				"type":        "boolean",
				"description": "Skip execution if session is busy",
			},
			"max_wait_seconds": map[string]any{
				"type":        "integer",
				"description": "Max seconds to wait if session is busy",
			},
		},
	}
}

type updateTaskArgs struct {
	ID             *int64  `json:"id,omitempty"`
	Name           *string `json:"name,omitempty"`
	CronExpr       *string `json:"cron_expr,omitempty"`
	Prompt         *string `json:"prompt,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
	Model          *string `json:"model,omitempty"`
	SkipIfBusy     *bool   `json:"skip_if_busy,omitempty"`
	MaxWaitSeconds *int    `json:"max_wait_seconds,omitempty"`
}

func (t *UpdateSchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args updateTaskArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var task *db.SchedulerTask
	var err error

	// Find task by ID or name
	if args.ID != nil {
		task, err = t.store.GetTask(ctx, *args.ID)
	} else if args.Name != nil {
		task, err = t.store.GetTaskByName(ctx, *args.Name)
	} else {
		return "", fmt.Errorf("either 'id' or 'name' is required")
	}

	if err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	// Update fields
	if args.CronExpr != nil {
		if err := validateCron(*args.CronExpr); err != nil {
			return "", err
		}
		task.CronExpr = *args.CronExpr
	}
	if args.Prompt != nil {
		task.Prompt = *args.Prompt
	}
	if args.Enabled != nil {
		task.Enabled = *args.Enabled
	}
	if args.Model != nil {
		task.Model = *args.Model
	}
	if args.SkipIfBusy != nil {
		task.SkipIfBusy = *args.SkipIfBusy
	}
	if args.MaxWaitSeconds != nil {
		task.MaxWaitSeconds = *args.MaxWaitSeconds
	}

	if err := t.store.UpdateTask(ctx, task); err != nil {
		return "", fmt.Errorf("failed to update task: %w", err)
	}

	return fmt.Sprintf("Updated task '%s' (ID: %d)", task.Name, task.ID), nil
}

// DeleteSchedulerTaskTool deletes a scheduler task.
type DeleteSchedulerTaskTool struct {
	store *store.Store
}

func NewDeleteSchedulerTaskTool(s *store.Store) *DeleteSchedulerTaskTool {
	return &DeleteSchedulerTaskTool{store: s}
}

func (t *DeleteSchedulerTaskTool) Name() string {
	return "delete_scheduler_task"
}

func (t *DeleteSchedulerTaskTool) Description() string {
	return "Delete a scheduled task. Provide task ID or name."
}

func (t *DeleteSchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "integer",
				"description": "Task ID to delete",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Task name to delete (alternative to ID)",
			},
		},
	}
}

type deleteTaskArgs struct {
	ID   *int64  `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`
}

func (t *DeleteSchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args deleteTaskArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var id int64

	if args.ID != nil {
		id = *args.ID
	} else if args.Name != nil {
		task, err := t.store.GetTaskByName(ctx, *args.Name)
		if err != nil {
			return "", fmt.Errorf("task not found: %w", err)
		}
		id = task.ID
	} else {
		return "", fmt.Errorf("either 'id' or 'name' is required")
	}

	// Get task name before deletion for response
	task, err := t.store.GetTask(ctx, id)
	if err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	if err := t.store.DeleteTask(ctx, id); err != nil {
		return "", fmt.Errorf("failed to delete task: %w", err)
	}

	return fmt.Sprintf("Deleted task '%s' (ID: %d)", task.Name, id), nil
}

// GetSchedulerTaskTool gets details of a specific task.
type GetSchedulerTaskTool struct {
	store *store.Store
}

func NewGetSchedulerTaskTool(s *store.Store) *GetSchedulerTaskTool {
	return &GetSchedulerTaskTool{store: s}
}

func (t *GetSchedulerTaskTool) Name() string {
	return "get_scheduler_task"
}

func (t *GetSchedulerTaskTool) Description() string {
	return "Get details of a specific scheduled task, including recent execution history."
}

func (t *GetSchedulerTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "integer",
				"description": "Task ID",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Task name (alternative to ID)",
			},
			"include_history": map[string]any{
				"type":        "boolean",
				"description": "Include recent execution history",
				"default":     true,
			},
		},
	}
}

type getTaskArgs struct {
	ID             *int64 `json:"id,omitempty"`
	Name           *string `json:"name,omitempty"`
	IncludeHistory *bool  `json:"include_history,omitempty"`
}

func (t *GetSchedulerTaskTool) Execute(ctx context.Context, _ ToolContext, argsJSON json.RawMessage) (string, error) {
	var args getTaskArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	var task *db.SchedulerTask
	var err error

	if args.ID != nil {
		task, err = t.store.GetTask(ctx, *args.ID)
	} else if args.Name != nil {
		task, err = t.store.GetTaskByName(ctx, *args.Name)
	} else {
		return "", fmt.Errorf("either 'id' or 'name' is required")
	}

	if err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	status := "enabled"
	if !task.Enabled {
		status = "disabled"
	}

	result := fmt.Sprintf("**Task: %s** (ID: %d)\n", task.Name, task.ID)
	result += fmt.Sprintf("- Schedule: `%s`\n", task.CronExpr)
	result += fmt.Sprintf("- Status: %s\n", status)
	result += fmt.Sprintf("- User ID: %d\n", task.UserID)
	result += fmt.Sprintf("- Channel: %s\n", task.Channel)
	if task.Model != "" {
		result += fmt.Sprintf("- Model: %s\n", task.Model)
	}
	result += fmt.Sprintf("- Notify: %v\n", task.Notify)
	result += fmt.Sprintf("- Skip if busy: %v\n", task.SkipIfBusy)
	if task.MaxWaitSeconds > 0 {
		result += fmt.Sprintf("- Max wait: %d seconds\n", task.MaxWaitSeconds)
	}
	result += fmt.Sprintf("- Prompt:\n```\n%s\n```\n\n", task.Prompt)

	lastRun := "never"
	if task.LastRunAt != nil {
		lastRun = task.LastRunAt.Format(time.RFC3339)
	}
	nextRun := "not scheduled"
	if task.NextRunAt != nil {
		nextRun = task.NextRunAt.Format(time.RFC3339)
	}

	result += fmt.Sprintf("- Last run: %s (%s)\n", lastRun, task.LastRunStatus)
	result += fmt.Sprintf("- Next run: %s\n", nextRun)
	result += fmt.Sprintf("- Success/Fail: %d/%d\n", task.SuccessfulRuns, task.FailedRuns)

	// Include history if requested
	includeHistory := true
	if args.IncludeHistory != nil {
		includeHistory = *args.IncludeHistory
	}

	if includeHistory {
		executions, err := t.store.ListExecutions(ctx, task.ID, 5)
		if err == nil && len(executions) > 0 {
			result += "\n**Recent Executions:**\n"
			for _, exec := range executions {
				result += fmt.Sprintf("- %s: %s", exec.StartedAt.Format(time.RFC3339), exec.Status)
				if exec.ErrorMessage != "" {
					result += fmt.Sprintf(" (%s)", exec.ErrorMessage)
				}
				result += "\n"
			}
		}
	}

	return result, nil
}

// Helper functions

func validateTask(name, cronExpr, prompt string, userID int64) error {
	if name == "" {
		return fmt.Errorf("task name is required")
	}
	if cronExpr == "" {
		return fmt.Errorf("cron expression is required")
	}
	if prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if userID == 0 {
		return fmt.Errorf("user ID is required")
	}
	return validateCron(cronExpr)
}

func validateCron(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("cron expression must have 5 fields (minute hour day-of-month month day-of-week)")
	}
	return nil
}
