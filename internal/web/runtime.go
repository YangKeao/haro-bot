package web

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentdefaults "github.com/YangKeao/haro-bot/internal/agent/defaults"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	llmopenai "github.com/YangKeao/haro-bot/internal/llm/openai"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type runtimeEntry struct {
	updatedAt  time.Time
	providerID int64
	agent      *agent.Agent
}

type RuntimeRegistry struct {
	store        *Store
	conversation memory.StoreAPI
	skills       *skills.Manager
	tools        *tools.Registry
	objects      *ObjectStore
	downloader   *avatarDownloader
	guidelines   *guidelines.Manager
	sandboxes    *sandbox.Service
	baseDir      string
	maxToolTurns int
	httpDebug    bool

	mu      sync.Mutex
	entries map[int64]runtimeEntry
}

func NewRuntimeRegistry(store *Store, conversation memory.StoreAPI, skillsManager *skills.Manager, registry *tools.Registry, objects *ObjectStore, guidelinesManager *guidelines.Manager, sandboxes *sandbox.Service, baseDir string, maxToolTurns int, httpDebug bool) *RuntimeRegistry {
	return &RuntimeRegistry{
		store: store, conversation: conversation, skills: skillsManager, tools: registry, objects: objects, downloader: newAvatarDownloader(),
		guidelines: guidelinesManager, sandboxes: sandboxes, baseDir: baseDir, maxToolTurns: maxToolTurns,
		httpDebug: httpDebug, entries: make(map[int64]runtimeEntry),
	}
}

func (r *RuntimeRegistry) Get(ctx context.Context, id int64) (*agent.Agent, AgentProfile, error) {
	profile, err := r.store.GetAgent(ctx, id)
	if err != nil {
		return nil, AgentProfile{}, err
	}
	if profile.ArchivedAt != nil {
		return nil, AgentProfile{}, errors.New("agent is archived")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.entries[id]; ok && entry.updatedAt.Equal(profile.RuntimeRevision) {
		return entry.agent, profile, nil
	}
	client := llmopenai.New(profile.BaseURL, profile.APIKey, llmopenai.WithHTTPDebug(r.httpDebug))
	scopedTools := tools.NewRegistry(r.tools.ListAll()...)
	scopedTools.Register(&getOwnProfileTool{agentID: id, store: r.store})
	scopedTools.Register(&updateOwnProfileTool{
		agentID: id, store: r.store, skills: r.skills, objects: r.objects, downloader: r.downloader,
		invalidate: func() { r.Invalidate(id) },
	})
	readSkill := tools.NewReadSkillTool(r.skills, r.sandboxes, id, profile.SkillNames)
	scopedTools.Register(readSkill)
	scopedTools.Register(tools.NewActivateSkillCompatTool(readSkill))
	if profile.SandboxID != nil && r.sandboxes != nil && r.sandboxes.Enabled() {
		scopedTools.Register(tools.NewSandboxExecCommandTool(id, r.sandboxes))
		scopedTools.Register(tools.NewSandboxWriteStdinTool(id, r.sandboxes))
	}
	reasoningEffort := ""
	if profile.ReasoningEffortOverride != nil {
		reasoningEffort = *profile.ReasoningEffortOverride
	}
	runtime := agent.New(
		r.conversation, r.skills, scopedTools, r.baseDir, r.maxToolTurns,
		client, profile.Model, profile.PromptFormat,
		llm.ReasoningConfig{Enabled: reasoningEffort != "", Effort: reasoningEffort},
	)
	runtime.SetProfile(profile.Instructions, profile.SkillNames)
	runtime.SetMiddleware(agentdefaults.New(r.guidelines, r.conversation, client, llm.ContextConfig{
		WindowTokens: profile.ResolvedContextWindow, AutoCompactTokenLimit: profile.ResolvedAutoCompactTokenLimit,
		EffectiveContextWindowPercent: profile.EffectiveContextWindowPercent,
	}, runtime.SessionStatusWriter()))
	r.entries[id] = runtimeEntry{updatedAt: profile.RuntimeRevision, providerID: profile.ProviderID, agent: runtime}
	return runtime, profile, nil
}
func (r *RuntimeRegistry) Resolve(ctx context.Context, id int64) (*agent.Agent, error) {
	runtime, _, err := r.Get(ctx, id)
	return runtime, err
}

func (r *RuntimeRegistry) Invalidate(id int64) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

func (r *RuntimeRegistry) InvalidateProvider(providerID int64) {
	r.mu.Lock()
	for id, entry := range r.entries {
		if entry.providerID == providerID {
			delete(r.entries, id)
		}
	}
	r.mu.Unlock()
}
