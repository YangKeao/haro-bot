package sandbox

import "time"

const (
	StateRunning   = "Running"
	StateSuspended = "Suspended"

	RunStarting = "starting"
	RunRunning  = "running"
	RunExited   = "exited"
	RunFailed   = "failed"
	RunLost     = "lost"

	DefaultExecYieldTimeMS                = 10_000
	DefaultWriteYieldTimeMS               = 250
	MinYieldTimeMS                        = 250
	MaxYieldTimeMS                        = 30_000
	MinEmptyWriteYieldTimeMS              = 5_000
	DefaultBackgroundTerminalMaxTimeoutMS = 300_000
	DefaultMaxOutputTokens                = 10_000
)

func ExecYieldTimeMS(requested int) int {
	if requested <= 0 {
		requested = DefaultExecYieldTimeMS
	}
	return clamp(requested, MinYieldTimeMS, MaxYieldTimeMS)
}

func StdinYieldTimeMS(chars string, requested, backgroundTerminalMax int) int {
	if requested <= 0 {
		requested = DefaultWriteYieldTimeMS
	}
	if chars != "" {
		return clamp(requested, MinYieldTimeMS, MaxYieldTimeMS)
	}
	if backgroundTerminalMax <= 0 {
		backgroundTerminalMax = DefaultBackgroundTerminalMaxTimeoutMS
	}
	if backgroundTerminalMax < MinEmptyWriteYieldTimeMS {
		backgroundTerminalMax = MinEmptyWriteYieldTimeMS
	}
	return clamp(requested, MinEmptyWriteYieldTimeMS, backgroundTerminalMax)
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

type Profile struct {
	ID                         int64     `json:"id"`
	Name                       string    `json:"name"`
	Description                string    `json:"description"`
	Image                      string    `json:"image"`
	CPULimitMillis             int       `json:"cpu_limit_millis"`
	MemoryLimitMiB             int       `json:"memory_limit_mib"`
	EphemeralStorageMiB        int       `json:"ephemeral_storage_mib"`
	WorkspaceStorageMiB        int       `json:"workspace_storage_mib"`
	DesiredState               string    `json:"desired_state"`
	Revision                   int64     `json:"revision"`
	AppliedRevision            int64     `json:"applied_revision"`
	PendingRestart             bool      `json:"pending_restart"`
	KubernetesName             string    `json:"kubernetes_name"`
	RuntimeStatus              string    `json:"runtime_status"`
	LastError                  *string   `json:"last_error,omitempty"`
	AgentIDs                   []int64   `json:"agent_ids"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
	RuntimeCAPEM               string    `json:"-"`
	RuntimeClientCertPEM       string    `json:"-"`
	RuntimeClientKeyCiphertext string    `json:"-"`
	RuntimeTokenCiphertext     string    `json:"-"`
}

type Write struct {
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	Image               string  `json:"image"`
	CPULimitMillis      int     `json:"cpu_limit_millis"`
	MemoryLimitMiB      int     `json:"memory_limit_mib"`
	EphemeralStorageMiB int     `json:"ephemeral_storage_mib"`
	WorkspaceStorageMiB int     `json:"workspace_storage_mib"`
	AgentIDs            []int64 `json:"agent_ids"`
}

type EnvironmentVariable struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	Secret   bool   `json:"secret"`
	HasValue bool   `json:"has_value"`
}

type EnvironmentWrite struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Secret      bool   `json:"secret"`
	KeepCurrent bool   `json:"keep_current,omitempty"`
}

type Process struct {
	ID              string     `json:"id"`
	SandboxID       int64      `json:"sandbox_id"`
	AgentID         int64      `json:"agent_id"`
	SessionID       int64      `json:"session_id"`
	Command         string     `json:"command"`
	TTY             *bool      `json:"tty,omitempty"`
	Status          string     `json:"status"`
	PID             int64      `json:"pid,omitempty"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	DurationMillis  int64      `json:"duration_millis"`
	CPUPercent      float64    `json:"cpu_percent,omitempty"`
	MemoryBytes     int64      `json:"memory_bytes,omitempty"`
	Output          string     `json:"output,omitempty"`
	OutputOffset    int64      `json:"output_offset,omitempty"`
	OutputTruncated bool       `json:"output_truncated,omitempty"`

	// InteractionOutput is the unread output for a single exec/write_stdin
	// interaction. Output remains the bounded cumulative log used by the web UI.
	InteractionOutput          string `json:"interaction_output,omitempty"`
	InteractionOutputAvailable bool   `json:"interaction_output_available,omitempty"`
	InteractionOutputTruncated bool   `json:"interaction_output_truncated,omitempty"`
	InteractionOriginalBytes   int    `json:"interaction_original_bytes,omitempty"`
}

type ExecRequest struct {
	ID          string            `json:"id"`
	AgentID     int64             `json:"agent_id"`
	SessionID   int64             `json:"session_id"`
	Command     string            `json:"command"`
	Workdir     string            `json:"workdir,omitempty"`
	Shell       string            `json:"shell,omitempty"`
	Login       *bool             `json:"login,omitempty"`
	TTY         bool              `json:"tty,omitempty"`
	Background  bool              `json:"background,omitempty"`
	YieldTimeMS int               `json:"yield_time_ms,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type SignalRequest struct {
	Signal string `json:"signal"`
}

type StdinRequest struct {
	Chars          string `json:"chars"`
	YieldTimeMS    int    `json:"yield_time_ms,omitempty"`
	MaxYieldTimeMS int    `json:"max_yield_time_ms,omitempty"`
}
