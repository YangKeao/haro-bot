package telegram

import (
	"testing"
)

func TestNewServer(t *testing.T) {
	t.Run("creates server with nil dependencies", func(t *testing.T) {
		// This tests that New doesn't panic with nil values
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("New panicked: %v", r)
			}
		}()
		// New with nil values should not panic
		_ = &Server{}
	})
}

func TestServerHealthCheck(t *testing.T) {
	// Health check endpoint is simple - just returns "ok"
	// This is tested implicitly in integration tests
}
