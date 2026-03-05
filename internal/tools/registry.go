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

type ToolContext struct {
	SessionID int64
	UserID    int64
	BaseDir   string
	SkillName string
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
		out = append(out, t)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}
