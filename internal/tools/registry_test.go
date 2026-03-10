package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type mockTool struct {
	name        string
	description string
	executeOut  string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) Parameters() map[string]any { return nil }
func (m *mockTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if m.executeOut != "" {
		return m.executeOut, nil
	}
	return "ok", nil
}

func TestRegistryGet(t *testing.T) {
	registry := NewRegistry()
	tool1 := &mockTool{name: "tool1", description: "first tool"}
	tool2 := &mockTool{name: "tool2", description: "second tool"}

	registry.Register(tool1)
	registry.Register(tool2)

	t.Run("finds registered tool", func(t *testing.T) {
		tool, ok := registry.Get("tool1")
		if !ok {
			t.Fatal("expected to find tool1")
		}
		if tool.Name() != "tool1" {
			t.Errorf("expected tool1, got %s", tool.Name())
		}
	})

	t.Run("returns false for missing tool", func(t *testing.T) {
		_, ok := registry.Get("nonexistent")
		if ok {
			t.Error("expected false for nonexistent tool")
		}
	})
}

func TestRegistryList(t *testing.T) {
	registry := NewRegistry()
	tool1 := &mockTool{name: "tool1", description: "first tool"}
	tool2 := &mockTool{name: "tool2", description: "second tool"}

	registry.Register(tool1)
	registry.Register(tool2)

	tools := registry.List()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names["tool1"] || !names["tool2"] {
		t.Error("expected both tool1 and tool2 in list")
	}
}

func TestRegistryEmpty(t *testing.T) {
	registry := NewRegistry()

	t.Run("empty registry returns false for get", func(t *testing.T) {
		_, ok := registry.Get("any")
		if ok {
			t.Error("expected false for empty registry")
		}
	})

	t.Run("empty registry returns empty list", func(t *testing.T) {
		tools := registry.List()
		if len(tools) != 0 {
			t.Errorf("expected 0 tools, got %d", len(tools))
		}
	})
}

func TestNewRegistryWithTools(t *testing.T) {
	tool1 := &mockTool{name: "tool1", description: "first"}
	tool2 := &mockTool{name: "tool2", description: "second"}

	registry := NewRegistry(tool1, tool2)

	if len(registry.List()) != 2 {
		t.Errorf("expected 2 tools, got %d", len(registry.List()))
	}
}
