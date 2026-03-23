// Package scheduler provides cron-based task scheduling for the haro-bot.
//
// The scheduler allows users to schedule LLM prompts to be executed at specific times
// using cron expressions. Tasks are stored in the database and can be managed via tools.
//
// Features:
//   - Cron-based scheduling using standard cron expressions
//   - Automatic retry with exponential backoff
//   - Skip-if-busy option to avoid interrupting active sessions
//   - Database persistence with automatic sync
//   - Task management via tools (create, update, delete, enable, disable)
//
// Usage:
//
//	sched := scheduler.New(scheduler.DefaultConfig(), agent, store, db)
//	go sched.Start(ctx)
//
//	// Schedule a task
//	task := &db.SchedulerTask{
//	    Name:      "daily_summary",
//	    CronExpr:  "0 9 * * *",
//	    Prompt:    "Generate a summary of yesterday's activity",
//	    UserID:    12345,
//	    Channel:   "telegram",
//	}
//	err := sched.ScheduleTask(ctx, task)
package scheduler
*** End Patch