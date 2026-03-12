package agent

import (
	"context"
	"sync"

	"github.com/YangKeao/haro-bot/internal/llm"
)

type Session struct {
	id   int64
	refs int
	deps *sessionDeps
	mu   sync.Mutex

	// For session interruption
	cancelMu   sync.Mutex
	cancelFunc context.CancelFunc
}

func (s *Session) setCancelFunc(cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancelFunc = cancel
}

func (s *Session) clearCancelFunc() {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancelFunc = nil
}

func (s *Session) cancel() bool {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.cancelFunc != nil {
		s.cancelFunc()
		s.cancelFunc = nil
		return true
	}
	return false
}

func (s *Session) toolsFor() []llm.Tool {
	var tools []llm.Tool
	for _, t := range s.deps.toolRegistry.List() {
		tools = append(tools, llm.Tool{
			Type: "function",
			Function: llm.FunctionSpec{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return tools
}

func (s *Session) estimatorForModel(model string) *llm.TokenEstimator {
	if model == "" || model == s.deps.model {
		if s.deps.tokenEstimator != nil {
			return s.deps.tokenEstimator
		}
		estimator, err := llm.NewTokenEstimator(s.deps.model)
		if err != nil {
			return nil
		}
		return estimator
	}
	estimator, err := llm.NewTokenEstimator(model)
	if err != nil {
		return s.deps.tokenEstimator
	}
	return estimator
}
