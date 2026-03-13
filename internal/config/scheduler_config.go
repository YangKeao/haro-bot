package config

// SchedulerTaskConfig configures a single scheduled task.
type SchedulerTaskConfig struct {
	Name       string `toml:"name"`
	CronExpr   string `toml:"cron_expr"`
	Prompt     string `toml:"prompt"`
	UserID     int64  `toml:"user_id"`
	Channel    string `toml:"channel"`
	Model      string `toml:"model"`
	Notify     bool   `toml:"notify"`
	MaxWait    string `toml:"max_wait"`
	SkipIfBusy bool   `toml:"skip_if_busy"`
}

// SchedulerConfig configures the scheduler.
type SchedulerConfig struct {
	Enabled bool                  `toml:"enabled"`
	Tasks   []SchedulerTaskConfig `toml:"tasks"`
}
