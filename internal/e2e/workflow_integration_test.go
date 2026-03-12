//go:build integration

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	agentdefaults "github.com/YangKeao/haro-bot/internal/agent/defaults"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/testutil"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func TestE2EAgentReadFileToolFlow(t *testing.T) {
	testutil.EnsureIntegrationEnv(t)
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	rootDir := t.TempDir()
	token := fmt.Sprintf("READ-TOKEN-%d", time.Now().UnixNano())
	filePath := filepath.Join(rootDir, "facts.txt")
	if err := os.WriteFile(filePath, []byte("token="+token+"\n"), 0o644); err != nil {
		t.Fatalf("write facts file: %v", err)
	}

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	auditStore := tools.NewAuditStore(gdb)
	fsTools := tools.NewFS([]string{rootDir}, auditStore, false)
	registry := tools.NewRegistry(
		tools.NewListDirTool(fsTools),
		tools.NewReadFileTool(fsTools),
	)

	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, nil, skillsMgr, registry, rootDir, 12, client, model, "openai", llm.ReasoningConfig{}, llm.ContextConfig{})
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, nil, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "9101")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	prompt := fmt.Sprintf(
		"Use tools to read file %s. Then reply with only the token value after 'token='.",
		filePath,
	)
	resp, err := agentSvc.Handle(ctx, userID, "e2e-read-file", prompt)
	if err != nil {
		t.Fatalf("handle prompt: %v", err)
	}
	if !containsFold(resp, token) {
		t.Fatalf("expected response to contain token %q, got: %s", token, resp)
	}

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-read-file")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		t.Fatalf("load view messages: %v", err)
	}
	toolFound := false
	for _, msg := range msgs {
		if msg.Role == "tool" && containsFold(msg.Content, token) {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Fatalf("expected tool output to contain token %q", token)
	}

	var audits []dbmodel.ToolAudit
	if err := gdb.Where("session_id = ? AND tool = ? AND status = ?", sessionID, "read_file", "ok").Find(&audits).Error; err != nil {
		t.Fatalf("query tool audit: %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("expected read_file audit records for session %d", sessionID)
	}
}

func TestE2EMemoryEngineCrossSessionRecall(t *testing.T) {
	cfg := testutil.EnsureIntegrationEnv(t)
	if strings.TrimSpace(cfg.Memory.Embedder.Model) == "" || cfg.Memory.Embedder.Dimensions <= 0 {
		t.Skip("memory embedder config is required in config.toml for this e2e test")
	}
	if strings.TrimSpace(cfg.Memory.Embedder.BaseURL) == "" {
		cfg.Memory.Embedder.BaseURL = os.Getenv("LLM_BASE_URL")
	}
	if strings.TrimSpace(cfg.Memory.Embedder.APIKey) == "" {
		cfg.Memory.Embedder.APIKey = os.Getenv("LLM_API_KEY")
	}
	if strings.TrimSpace(cfg.Memory.Embedder.APIKey) == "" {
		t.Skip("memory embedder api key missing in config and environment")
	}

	gdb, cleanup := testutil.NewTestDBWithMigrationsConfig(t, cfg.Memory)
	t.Cleanup(cleanup)

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	registry := tools.NewRegistry()
	client, model := testutil.NewLLMClientFromEnv(t)

	memEngine, err := memory.NewEngine(gdb, store, client, model, cfg.Memory)
	if err != nil {
		t.Fatalf("init memory engine: %v", err)
	}

	promptFormat := "openai"
	if v := strings.TrimSpace(string(cfg.LLMPromptFormat)); v != "" {
		promptFormat = v
	}
	agentSvc := agent.New(
		store,
		memEngine,
		skillsMgr,
		registry, t.TempDir(),
		6,
		client,
		model,
		promptFormat,
		llm.ReasoningConfig{Enabled: cfg.LLMReasoningEnabled, Effort: cfg.LLMReasoningEffort},
		llm.ContextConfig{},
	)
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, memEngine, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "9102")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token := fmt.Sprintf("CITY-E2E-%d", time.Now().UnixNano())
	seedText := fmt.Sprintf("User preference: favorite city keyword is %s.", token)
	embedder, err := memory.NewOpenAIEmbedder(cfg.Memory.Embedder)
	if err != nil {
		t.Fatalf("init embedder: %v", err)
	}
	vector, err := embedder.Embed(ctx, seedText)
	if err != nil {
		t.Fatalf("embed seed memory: %v", err)
	}
	vectorStore := memory.NewTiDBVectorStore(gdb, cfg.Memory.Vector.Distance)
	if _, err := vectorStore.Insert(ctx, memory.MemoryItem{
		UserID:  userID,
		Type:    "preference",
		Content: seedText,
	}, vector); err != nil {
		t.Fatalf("insert seed memory: %v", err)
	}

	recallPrompt := "What is my favorite city keyword? Reply with the exact keyword."
	resp, err := agentSvc.Handle(ctx, userID, "e2e-memory-recall", recallPrompt)
	if err != nil {
		t.Fatalf("recall conversation: %v", err)
	}
	if !containsFold(resp, token) {
		t.Fatalf("expected recall response to contain %q, got: %s", token, resp)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
