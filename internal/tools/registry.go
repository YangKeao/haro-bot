package tools

import (
	"context"
	"encoding/json"
	"sort"
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
	if r.tools == nil {
		r.tools = make(map[string]Tool)
	}
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	if r == nil || len(r.tools) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.tools))
	for k := range r.tools {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Tool, 0, len(keys))
	for _, k := range keys {
		out = append(out, r.tools[k])
	}
	return out
}
