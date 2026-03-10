package fork

import (
	"testing"
	"time"
)

func TestManagerOptions(t *testing.T) {
	opts := ManagerOptions{
		CleanupAfter: 5 * time.Minute,
	}
	if opts.CleanupAfter != 5*time.Minute {
		t.Errorf("expected CleanupAfter 5m, got %v", opts.CleanupAfter)
	}
}

func TestNewManager(t *testing.T) {
	// NewManager with nil values should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewManager panicked: %v", r)
		}
	}()
	mgr := NewManager(nil, nil)
	if mgr == nil {
		t.Error("expected non-nil manager")
	}
}

func TestNewManagerWithOptions(t *testing.T) {
	opts := ManagerOptions{CleanupAfter: time.Hour}
	mgr := NewManagerWithOptions(nil, nil, opts)
	if mgr == nil {
		t.Error("expected non-nil manager")
	}
}
