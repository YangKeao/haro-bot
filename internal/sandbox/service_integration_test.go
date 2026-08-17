//go:build integration

package sandbox

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/testutil"
)

type fakeControlPlane struct{ applied []Profile }

func (f *fakeControlPlane) Apply(_ context.Context, profile Profile, credentials RuntimeCredentials) error {
	f.applied = append(f.applied, profile)
	return nil
}
func (*fakeControlPlane) SetOperatingMode(context.Context, Profile, string) error { return nil }
func (*fakeControlPlane) Delete(context.Context, Profile) error                   { return nil }
func (*fakeControlPlane) ResetWorkspace(context.Context, Profile) error           { return nil }
func (*fakeControlPlane) Status(context.Context, Profile) (string, *string, error) {
	return "Ready", nil, nil
}

type fakeRuntime struct {
	input      ExecRequest
	stdinInput StdinRequest
}

func (f *fakeRuntime) Exec(_ context.Context, _ RuntimeTarget, input ExecRequest) (Process, error) {
	f.input = input
	exit := 0
	finished := time.Now().UTC()
	return Process{ID: input.ID, AgentID: input.AgentID, SessionID: input.SessionID, Command: input.Command, Status: RunExited, ExitCode: &exit, StartedAt: finished.Add(-time.Second), FinishedAt: &finished, Output: "connected with " + input.Environment["MYSQL_PASSWORD"]}, nil
}
func (*fakeRuntime) ListProcesses(context.Context, RuntimeTarget, *int64) ([]Process, error) {
	return nil, nil
}
func (*fakeRuntime) GetProcess(context.Context, RuntimeTarget, string) (Process, error) {
	return Process{}, nil
}
func (f *fakeRuntime) WriteStdin(_ context.Context, _ RuntimeTarget, id string, input StdinRequest) (Process, error) {
	f.stdinInput = input
	return Process{ID: id, TTY: boolPointer(true), Status: RunRunning, StartedAt: time.Now().UTC()}, nil
}
func (*fakeRuntime) Signal(context.Context, RuntimeTarget, string, string) (Process, error) {
	return Process{}, nil
}

func TestServiceInjectsEncryptedAgentEnvironmentAndRedactsSecrets(t *testing.T) {
	database, cleanup := testutil.NewTestDBWithMigrations(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	provider := dbmodel.Provider{Name: "test", BaseURL: "https://example.test/v1", PromptFormat: "openai"}
	if err := database.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	agent := dbmodel.Agent{ProviderID: provider.ID, Name: "DB agent", Model: "test", Icon: "bot", Color: "#000000", AvatarMode: "icon", EffectiveContextWindowPercent: 95}
	if err := database.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	user := dbmodel.User{ExternalID: stringPointer("sandbox-test")}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	session := dbmodel.Session{UserID: user.ID, AgentID: &agent.ID, Channel: "web:test", Title: "test"}
	if err := database.Create(&session).Error; err != nil {
		t.Fatal(err)
	}

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	control := &fakeControlPlane{}
	runtime := &fakeRuntime{}
	cfg := config.SandboxConfig{Enabled: true, Namespace: "test", DefaultImage: "sandbox:test", DefaultCPULimitMillis: 1000, DefaultMemoryLimitMiB: 1024, DefaultEphemeralStorageMiB: 1024, DefaultWorkspaceStorageMiB: 2048, MaxCPULimitMillis: 4000, MaxMemoryLimitMiB: 4096, MaxEphemeralStorageMiB: 4096, MaxWorkspaceStorageMiB: 8192, MaxRunning: 2, RuntimePort: 8888, BackgroundTerminalMaxTimeoutMS: 300000}
	service := NewServiceWithDependencies(database, cfg, box, control, runtime)
	profile, err := service.Create(ctx, Write{Name: "shared", AgentIDs: []int64{agent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(control.applied) != 1 || len(profile.AgentIDs) != 1 || profile.AgentIDs[0] != agent.ID {
		t.Fatalf("unexpected provision result: %#v", profile)
	}
	variables, err := service.ReplaceAgentEnvironment(ctx, agent.ID, []EnvironmentWrite{{Name: "MYSQL_PASSWORD", Value: "highly-secret", Secret: true}, {Name: "MYSQL_HOST", Value: "database", Secret: false}})
	if err != nil {
		t.Fatal(err)
	}
	if variables[0].Name != "MYSQL_HOST" || variables[0].Value != "database" || variables[1].Name != "MYSQL_PASSWORD" || variables[1].Value != "" || !variables[1].HasValue {
		t.Fatalf("secret exposure or non-secret loss: %#v", variables)
	}
	var stored dbmodel.AgentEnvironmentVariable
	if err := database.First(&stored, "agent_id = ? AND name = ?", agent.ID, "MYSQL_PASSWORD").Error; err != nil {
		t.Fatal(err)
	}
	if stored.ValueCiphertext == "highly-secret" {
		t.Fatal("secret was stored in plaintext")
	}
	process, err := service.StartProcess(ctx, agent.ID, session.ID, ExecRequest{Command: "mysql --password=highly-secret", TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.input.Environment["MYSQL_PASSWORD"] != "highly-secret" || runtime.input.Environment["MYSQL_HOST"] != "database" {
		t.Fatalf("runtime environment mismatch: %#v", runtime.input.Environment)
	}
	if runtime.input.YieldTimeMS != DefaultExecYieldTimeMS {
		t.Fatalf("exec yield = %d, want %d", runtime.input.YieldTimeMS, DefaultExecYieldTimeMS)
	}
	if process.Command != "mysql --password=[REDACTED]" || process.Output != "connected with [REDACTED]" {
		t.Fatalf("secret was not redacted: %#v", process)
	}
	if _, err := service.WriteProcessStdin(ctx, agent.ID, process.ID, StdinRequest{YieldTimeMS: 1000}); err != nil {
		t.Fatal(err)
	}
	if runtime.stdinInput.YieldTimeMS != MinEmptyWriteYieldTimeMS || runtime.stdinInput.MaxYieldTimeMS != 300000 {
		t.Fatalf("unexpected empty poll bounds: %#v", runtime.stdinInput)
	}
	if _, err := service.ListProcessesForSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	var run dbmodel.SandboxRun
	if err := database.First(&run, "id = ?", process.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != RunLost || !run.TTY {
		t.Fatalf("stale runtime process was not reconciled: %#v", run)
	}
}

func stringPointer(value string) *string { return &value }
