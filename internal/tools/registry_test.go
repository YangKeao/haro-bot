package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

type dummyTool struct {
	name string
}

func (d dummyTool) Name() string        { return d.name }
func (d dummyTool) Description() string { return "dummy" }
func (d dummyTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (d dummyTool) Execute(context.Context, ToolContext, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			reg.Register(dummyTool{name: "tool-" + strconv.Itoa(i)})
			runtime.Gosched()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = reg.List()
			runtime.Gosched()
		}
	}()
	wg.Wait()
}
