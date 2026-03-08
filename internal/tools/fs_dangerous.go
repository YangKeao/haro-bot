package tools

// NewDangerousFS creates a filesystem tool that skips all security checks.
// Use with caution - this disables all path restrictions, symlink checks,
// approval requests, and audit logging.
func NewDangerousFS() *FS {
	return &FS{
		dangerous:    true,
		allowedRoots: nil,
		audit:        nil,
		approver:     nil,
	}
}

// IsDangerous returns true if the filesystem is running in dangerous mode.
func (f *FS) IsDangerous() bool {
	return f != nil && f.dangerous
}
