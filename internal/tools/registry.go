package tools

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error)
}

type HiddenTool interface {
	Hidden() bool
}

// AgentRestrictedTool marks workspace-management tools that must not be
// exposed to ordinary agent runtimes. Trusted application layers may still
// instantiate them directly.
type AgentRestrictedTool interface {
	AgentRestricted() bool
}

type ToolContext struct {
	SessionID int64
	BaseDir   string
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry(toolList ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range toolList {
		if t == nil {
			continue
		}
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) Register(t Tool) {
	if r == nil || t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = make(map[string]Tool)
	}
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	return r.list(false)
}

func (r *Registry) ListAll() []Tool {
	return r.list(true)
}

func (r *Registry) list(includeHidden bool) []Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	if len(r.tools) == 0 {
		r.mu.RUnlock()
		return nil
	}
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		if hidden, ok := t.(HiddenTool); ok && hidden.Hidden() && !includeHidden {
			continue
		}
		out = append(out, t)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}
