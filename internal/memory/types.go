package memory

import "time"

// MemoryItem represents a long-term memory stored in the vector store.
type MemoryItem struct {
	ID        int64
	UserID    int64
	SessionID *int64
	Type      string
	Content   string
	Metadata  map[string]any
	Score     float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MemoryCandidate struct {
	Memory     string   `json:"memory"`
	Type       string   `json:"type"`
	Importance float64  `json:"importance"`
	Confidence float64  `json:"confidence"`
	Tags       []string `json:"tags"`
	Source     string   `json:"source"`
}

type MemoryAction struct {
	Action   string `json:"action"`
	TargetID int64  `json:"target_id"`
	Memory   string `json:"memory"`
	Type     string `json:"type"`
	Reason   string `json:"reason"`
}

const (
	MemoryActionAdd    = "ADD"
	MemoryActionUpdate = "UPDATE"
	MemoryActionDelete = "DELETE"
	MemoryActionNoop   = "NOOP"
)
