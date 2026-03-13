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
	memopenai "github.com/YangKeao/haro-bot/internal/memory/embedder/openai"
	memtidb "github.com/YangKeao/haro-bot/internal/memory/vectorstore/tidb"
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
	fsTools := tools.NewFS(auditStore)
	registry := tools.NewRegistry(
		tools.NewListDirTool(fsTools),
		tools.NewReadFileTool(fsTools),
	)

	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, skillsMgr, registry, rootDir, 12, client, model, "openai", llm.ReasoningConfig{})
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

func TestE2EAgentApplyPatchToolFlow(t *testing.T) {
	testutil.EnsureIntegrationEnv(t)
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	rootDir := t.TempDir()
	token := fmt.Sprintf("PATCH-TOKEN-%d", time.Now().UnixNano())
	filePath := filepath.Join(rootDir, "notes.txt")
	original := "title=demo\nstatus=draft\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("write notes file: %v", err)
	}

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	auditStore := tools.NewAuditStore(gdb)
	fsTools := tools.NewFS(auditStore)
	registry := tools.NewRegistry(
		tools.NewReadFileTool(fsTools),
		tools.NewApplyPatchTool(fsTools),
	)

	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, skillsMgr, registry, rootDir, 12, client, model, "openai", llm.ReasoningConfig{})
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, nil, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "9103")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	prompt := fmt.Sprintf(
		"Use the apply_patch tool to edit %s. Replace the exact line `status=draft` with `status=%s`. After the tool succeeds, reply with only `status=%s`.",
		filePath,
		token,
		token,
	)
	resp, err := handleWithRetry(ctx, agentSvc, userID, "e2e-apply-patch", prompt, 2)
	if err != nil {
		t.Fatalf("handle prompt: %v", err)
	}
	if !containsFold(resp, "status="+token) {
		t.Fatalf("expected response to contain updated status %q, got: %s", "status="+token, resp)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	wantContent := "title=demo\nstatus=" + token + "\n"
	if string(content) != wantContent {
		t.Fatalf("expected file content %q, got %q", wantContent, string(content))
	}

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-apply-patch")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		t.Fatalf("load view messages: %v", err)
	}
	toolFound := false
	for _, msg := range msgs {
		if msg.Role == "tool" && containsFold(msg.Content, "Updated file:") {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Fatalf("expected tool output to include apply_patch success message")
	}

	var audits []dbmodel.ToolAudit
	if err := gdb.Where("session_id = ? AND tool = ? AND status = ?", sessionID, "apply_patch", "ok").Find(&audits).Error; err != nil {
		t.Fatalf("query tool audit: %v", err)
	}
	if len(audits) == 0 {
		t.Fatalf("expected apply_patch audit records for session %d", sessionID)
	}
}

func TestE2EAgentApplyPatchToolFlowComplexFile(t *testing.T) {
	testutil.EnsureIntegrationEnv(t)
	gdb, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)

	rootDir := t.TempDir()
	token := fmt.Sprintf("PATCH-COMPLEX-%d", time.Now().UnixNano())
	filePath := filepath.Join(rootDir, "project_notes.txt")
	draftDir := filepath.Join(rootDir, "drafts")
	if err := os.MkdirAll(draftDir, 0o755); err != nil {
		t.Fatalf("create drafts dir: %v", err)
	}
	releaseDraftPath := filepath.Join(draftDir, "release_plan.txt")
	obsoletePath := filepath.Join(draftDir, "obsolete.txt")
	originalLines := []string{
		"# Project Notes",
		"",
		"owner=team-alpha",
		"status=draft",
		"priority=medium",
		"",
		"[milestones]",
		"m1=design",
		"m2=implementation",
		"m3=review",
		"",
		"[risks]",
		"risk1=timeline",
		"risk2=scope",
		"risk3=handoff",
		"",
		"[daily-log]",
		"day01=created project note",
		"day02=aligned on requirements",
		"day03=prepared draft plan",
		"day04=scheduled review meeting",
		"day05=confirmed staffing",
		"day06=updated backlog",
		"day07=waiting for approval",
		"day08=still draft",
		"day09=no changes",
		"day10=collecting feedback",
		"day11=feedback incorporated",
		"day12=ready for launch prep",
		"",
		"[appendix]",
		"notes=line-a",
		"notes=line-b",
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(originalLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write complex notes file: %v", err)
	}
	releaseDraft := strings.Join([]string{
		"title=release-plan",
		"owner=team-alpha",
		"status=pending",
		"summary=ship after final review",
		"",
	}, "\n")
	if err := os.WriteFile(releaseDraftPath, []byte(releaseDraft), 0o644); err != nil {
		t.Fatalf("write release draft file: %v", err)
	}
	if err := os.WriteFile(obsoletePath, []byte("remove me\n"), 0o644); err != nil {
		t.Fatalf("write obsolete file: %v", err)
	}
	releasePlanPath := filepath.Join(rootDir, "release", "final_plan.txt")
	checklistPath := filepath.Join(rootDir, "release", "checklist.txt")

	store := memory.NewStore(gdb)
	skillsStore := skills.NewStore(gdb)
	skillsMgr := skills.NewManager(skillsStore, t.TempDir(), nil)
	guidelinesMgr := guidelines.NewManager(gdb)
	auditStore := tools.NewAuditStore(gdb)
	fsTools := tools.NewFS(auditStore)
	registry := tools.NewRegistry(
		tools.NewReadFileTool(fsTools),
		tools.NewApplyPatchTool(fsTools),
	)

	client, model := testutil.NewLLMClientFromEnv(t)
	agentSvc := agent.New(store, skillsMgr, registry, rootDir, 16, client, model, "openai", llm.ReasoningConfig{})
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, nil, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "9104")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	prompt := fmt.Sprintf(
		"First use the read_file tool to inspect %s and %s. Then use the apply_patch tool to make all required file changes. "+
			"Use apply_patch for every file edit, keep unrelated lines unchanged, and prefer doing the work in one patch. "+
			"Required changes: "+
			"replace the exact line `status=draft` with `status=ready-%s`; "+
			"replace the exact line `m2=implementation` with `m2=implementation-complete`; "+
			"replace the exact line `day08=still draft` with `day08=approved for rollout`; "+
			"replace the exact line `notes=line-b` with `notes=line-b-updated`; "+
			"append a new section at the end containing exactly `[release]` on one line and `tag=%s` on the next line to %s; "+
			"move %s to %s and in the moved file replace `status=pending` with `status=approved` and replace `summary=ship after final review` with `summary=ship with tag %s`; "+
			"create %s with exactly these lines: `check-owner=team-alpha`, `check-status=approved`, `check-tag=%s`; "+
			"delete %s. "+
			"After the patch succeeds, reply with only `tag=%s`.",
		filePath,
		releaseDraftPath,
		token,
		token,
		filePath,
		releaseDraftPath,
		releasePlanPath,
		token,
		checklistPath,
		token,
		obsoletePath,
		token,
	)
	resp, err := handleWithRetry(ctx, agentSvc, userID, "e2e-apply-patch-complex", prompt, 2)
	if err != nil {
		t.Fatalf("handle prompt: %v", err)
	}
	if !containsFold(resp, "tag="+token) {
		t.Fatalf("expected response to contain release tag %q, got: %s", "tag="+token, resp)
	}
	missing := collectComplexApplyPatchProblems(filePath, releaseDraftPath, releasePlanPath, checklistPath, obsoletePath, token)
	if len(missing) > 0 {
		followup := fmt.Sprintf(
			"The previous apply_patch work is incomplete. Continue editing files until all of these conditions are true: %s. Reply with only `tag=%s` when everything is done.",
			strings.Join(missing, "; "),
			token,
		)
		resp, err = handleWithRetry(ctx, agentSvc, userID, "e2e-apply-patch-complex", followup, 2)
		if err != nil {
			t.Fatalf("handle follow-up prompt: %v", err)
		}
		if !containsFold(resp, "tag="+token) {
			t.Fatalf("expected follow-up response to contain release tag %q, got: %s", "tag="+token, resp)
		}
	}
	missing = collectComplexApplyPatchProblems(filePath, releaseDraftPath, releasePlanPath, checklistPath, obsoletePath, token)
	if len(missing) > 0 {
		t.Fatalf("complex apply_patch scenario incomplete: %s", strings.Join(missing, "; "))
	}

	sessionID, err := store.GetOrCreateSession(ctx, userID, "e2e-apply-patch-complex")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	msgs, _, err := store.LoadViewMessages(ctx, sessionID, 0)
	if err != nil {
		t.Fatalf("load view messages: %v", err)
	}
	toolFound := false
	for _, msg := range msgs {
		if msg.Role == "tool" &&
			containsFold(msg.Content, "Updated file:") &&
			containsFold(msg.Content, "Created file:") &&
			containsFold(msg.Content, "Deleted file:") {
			toolFound = true
			break
		}
	}
	if !toolFound {
		t.Fatalf("expected tool output to include update/create/delete messages for complex scenario")
	}

	var audits []dbmodel.ToolAudit
	if err := gdb.Where("session_id = ? AND tool = ? AND status = ?", sessionID, "apply_patch", "ok").Find(&audits).Error; err != nil {
		t.Fatalf("query tool audit: %v", err)
	}
	if len(audits) < 4 {
		t.Fatalf("expected at least 4 apply_patch audit records for session %d, got %d", sessionID, len(audits))
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

	embedder, err := memopenai.New(cfg.Memory.Embedder)
	if err != nil {
		t.Fatalf("init embedder: %v", err)
	}
	vectorStore := memtidb.New(gdb, cfg.Memory.Vector.Distance)
	memEngine, err := memory.NewEngine(store, client, model, embedder, vectorStore, cfg.Memory)
	if err != nil {
		t.Fatalf("init memory engine: %v", err)
	}

	promptFormat := "openai"
	if v := strings.TrimSpace(string(cfg.LLMPromptFormat)); v != "" {
		promptFormat = v
	}
	agentSvc := agent.New(
		store,
		skillsMgr,
		registry, t.TempDir(),
		6,
		client,
		model,
		promptFormat,
		llm.ReasoningConfig{Enabled: cfg.LLMReasoningEnabled, Effort: cfg.LLMReasoningEffort},
	)
	agentSvc.SetMiddleware(agentdefaults.New(guidelinesMgr, store, memEngine, client, llm.ContextConfig{}, agentSvc.SessionStatusWriter()))

	ctx := context.Background()
	userID, err := store.GetOrCreateUserByExternalID(ctx, "telegram", "9102")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token := fmt.Sprintf("CITY-E2E-%d", time.Now().UnixNano())
	seedText := fmt.Sprintf("User preference: favorite city keyword is %s.", token)
	embedder, err = memopenai.New(cfg.Memory.Embedder)
	if err != nil {
		t.Fatalf("init embedder: %v", err)
	}
	vector, err := embedder.Embed(ctx, seedText)
	if err != nil {
		t.Fatalf("embed seed memory: %v", err)
	}
	vectorStore = memtidb.New(gdb, cfg.Memory.Vector.Distance)
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

func handleWithRetry(ctx context.Context, agentSvc *agent.Agent, userID int64, channel, prompt string, attempts int) (string, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		resp, err := agentSvc.Handle(ctx, userID, channel, prompt)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func collectComplexApplyPatchProblems(filePath, releaseDraftPath, releasePlanPath, checklistPath, obsoletePath, token string) []string {
	var problems []string

	content, err := os.ReadFile(filePath)
	if err != nil {
		return []string{"project_notes.txt is missing or unreadable"}
	}
	gotContent := string(content)
	for _, needle := range []string{
		"status=ready-" + token,
		"m2=implementation-complete",
		"day08=approved for rollout",
		"notes=line-b-updated",
		"[release]\ntag=" + token,
	} {
		if !strings.Contains(gotContent, needle) {
			problems = append(problems, "project_notes.txt must contain "+needle)
		}
	}
	for _, needle := range []string{
		"status=draft",
		"m2=implementation\n",
		"day08=still draft",
		"notes=line-b\n",
	} {
		if strings.Contains(gotContent, needle) {
			problems = append(problems, "project_notes.txt must no longer contain "+strings.TrimSpace(needle))
		}
	}
	if !strings.HasSuffix(gotContent, "tag="+token+"\n") {
		problems = append(problems, "project_notes.txt must end with tag="+token)
	}
	if _, err := os.Stat(releaseDraftPath); !os.IsNotExist(err) {
		problems = append(problems, "the old draft file must be moved away from "+releaseDraftPath)
	}

	releasePlanContent, err := os.ReadFile(releasePlanPath)
	if err != nil {
		problems = append(problems, "the moved release plan must exist at "+releasePlanPath)
	} else {
		releasePlanText := string(releasePlanContent)
		for _, needle := range []string{
			"title=release-plan",
			"owner=team-alpha",
			"status=approved",
			"summary=ship with tag " + token,
		} {
			if !strings.Contains(releasePlanText, needle) {
				problems = append(problems, "the moved release plan must contain "+needle)
			}
		}
	}

	checklistContent, err := os.ReadFile(checklistPath)
	if err != nil {
		problems = append(problems, "checklist file must exist at "+checklistPath)
	} else {
		wantChecklist := strings.Join([]string{
			"check-owner=team-alpha",
			"check-status=approved",
			"check-tag=" + token,
		}, "\n")
		if string(checklistContent) != wantChecklist {
			problems = append(problems, "checklist file must exactly match the requested three lines")
		}
	}

	if _, err := os.Stat(obsoletePath); !os.IsNotExist(err) {
		problems = append(problems, "obsolete file must be deleted at "+obsoletePath)
	}

	return problems
}
