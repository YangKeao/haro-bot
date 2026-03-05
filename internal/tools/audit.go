package tools

import "context"

type AuditEntry struct {
	SessionID int64
	UserID    int64
	Tool      string
	Path      string
	Allowed   bool
	Status    string
	Reason    string
	Metadata  map[string]any
}

type AuditLogger interface {
	Record(ctx context.Context, entry AuditEntry) error
}
