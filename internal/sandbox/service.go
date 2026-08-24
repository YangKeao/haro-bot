package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var ErrOperationInProgress = errors.New("another Sandbox lifecycle operation is already in progress")

type Service struct {
	db      *gorm.DB
	config  config.SandboxConfig
	box     *SecretBox
	control ControlPlane
	runtime Runtime
}

func NewService(db *gorm.DB, cfg config.SandboxConfig) (*Service, error) {
	if db == nil {
		return nil, errors.New("sandbox database is required")
	}
	service := &Service{db: db, config: cfg, runtime: NewHTTPRuntime()}
	if !cfg.Enabled {
		return service, nil
	}
	box, err := NewSecretBox(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	control, err := NewKubernetesControlPlane(cfg)
	if err != nil {
		return nil, err
	}
	service.box, service.control = box, control
	return service, nil
}

func NewServiceWithDependencies(db *gorm.DB, cfg config.SandboxConfig, box *SecretBox, control ControlPlane, runtime Runtime) *Service {
	return &Service{db: db, config: cfg, box: box, control: control, runtime: runtime}
}

func (s *Service) Enabled() bool {
	return s != nil && s.config.Enabled && s.box != nil && s.control != nil && s.runtime != nil
}

func (s *Service) Config() config.SandboxConfig { return s.config }

func (s *Service) List(ctx context.Context) ([]Profile, error) {
	var rows []dbmodel.Sandbox
	if err := s.db.WithContext(ctx).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]Profile, 0, len(rows))
	for _, row := range rows {
		profile, err := s.profileFromRow(ctx, row, true)
		if err != nil {
			return nil, err
		}
		result = append(result, profile)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id int64) (Profile, error) {
	var row dbmodel.Sandbox
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return Profile{}, err
	}
	return s.profileFromRow(ctx, row, true)
}

func (s *Service) Create(ctx context.Context, input Write) (Profile, error) {
	if !s.Enabled() {
		return Profile{}, errors.New("sandbox support is disabled")
	}
	normalized, err := s.normalizeWrite(input, nil)
	if err != nil {
		return Profile{}, err
	}
	var running int64
	if err := s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("desired_state = ?", StateRunning).Count(&running).Error; err != nil {
		return Profile{}, err
	}
	if int(running) >= s.config.MaxRunning {
		return Profile{}, fmt.Errorf("running sandbox limit (%d) reached", s.config.MaxRunning)
	}
	kubernetesName, err := newKubernetesName(normalized.Name)
	if err != nil {
		return Profile{}, err
	}
	serviceNames := []string{kubernetesName, kubernetesName + "." + s.config.Namespace, kubernetesName + "." + s.config.Namespace + ".svc", kubernetesName + "." + s.config.Namespace + ".svc.cluster.local"}
	credentials, err := GenerateRuntimeCredentials(serviceNames)
	if err != nil {
		return Profile{}, err
	}
	clientKey, err := s.box.Encrypt(string(credentials.ClientKeyPEM), "sandbox:"+kubernetesName+":client-key")
	if err != nil {
		return Profile{}, err
	}
	token, err := s.box.Encrypt(credentials.Token, "sandbox:"+kubernetesName+":token")
	if err != nil {
		return Profile{}, err
	}
	row := dbmodel.Sandbox{
		Name: normalized.Name, Description: normalized.Description, Image: normalized.Image,
		CPULimitMillis: normalized.CPULimitMillis, MemoryLimitMiB: normalized.MemoryLimitMiB,
		EphemeralStorageMiB: normalized.EphemeralStorageMiB, WorkspaceStorageMiB: normalized.WorkspaceStorageMiB,
		DesiredState: StateRunning, Revision: 1, AppliedRevision: 0, KubernetesName: kubernetesName,
		RuntimeCAPEM: string(credentials.CAPEM), RuntimeClientCertPEM: string(credentials.ClientCertPEM),
		RuntimeClientKeyCiphertext: clientKey, RuntimeTokenCiphertext: token,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return bindAgents(tx, row.ID, normalized.AgentIDs)
	}); err != nil {
		return Profile{}, err
	}
	profile, err := s.profileFromRow(ctx, row, false)
	if err != nil {
		return Profile{}, err
	}
	if err := s.control.Apply(ctx, profile, credentials); err != nil {
		s.setProvisionError(ctx, row.ID, err)
		return s.Get(ctx, row.ID)
	}
	if err := s.markApplied(ctx, row.ID, row.Revision); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, row.ID)
}

func (s *Service) Update(ctx context.Context, id int64, input Write) (Profile, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	normalized, err := s.normalizeWrite(input, &current)
	if err != nil {
		return Profile{}, err
	}
	if normalized.WorkspaceStorageMiB != current.WorkspaceStorageMiB {
		return Profile{}, errors.New("workspace_storage_mib is immutable; create a new sandbox to use a different PVC size")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"name": normalized.Name, "description": normalized.Description, "image": normalized.Image,
			"cpu_limit_millis": normalized.CPULimitMillis, "memory_limit_mib": normalized.MemoryLimitMiB,
			"ephemeral_storage_mib": normalized.EphemeralStorageMiB, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now(),
		}
		if err := tx.Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		return bindAgents(tx, id, normalized.AgentIDs)
	})
	if err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Apply(ctx context.Context, id int64) (Profile, error) {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if profile.Revision == profile.AppliedRevision {
		return profile, nil
	}
	if err := s.beginOperation(ctx, profile, OperationApply); err != nil {
		return Profile{}, err
	}
	if err := s.control.Apply(ctx, profile, RuntimeCredentials{}); err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	if profile.DesiredState == StateRunning {
		if err := s.control.Restart(ctx, profile); err != nil {
			s.failOperation(ctx, id, err)
			return Profile{}, err
		}
	}
	if err := s.markApplied(ctx, id, profile.Revision); err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	if profile.DesiredState == StateSuspended {
		s.clearOperation(ctx, id)
	}
	return s.Get(ctx, id)
}

func (s *Service) Restart(ctx context.Context, id int64) (Profile, error) {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if profile.DesiredState != StateRunning {
		return Profile{}, errors.New("sandbox must be running before it can be restarted")
	}
	if err := s.beginOperation(ctx, profile, OperationRestart); err != nil {
		return Profile{}, err
	}
	if err := s.control.SyncRuntimeHelper(ctx, profile); err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	if err := s.control.Restart(ctx, profile); err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) SetOperatingMode(ctx context.Context, id int64, state string) (Profile, error) {
	if state != StateRunning && state != StateSuspended {
		return Profile{}, errors.New("state must be Running or Suspended")
	}
	profile, err := s.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if state == StateRunning && profile.DesiredState != StateRunning {
		var running int64
		if err := s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("desired_state = ? AND id <> ?", StateRunning, id).Count(&running).Error; err != nil {
			return Profile{}, err
		}
		if int(running) >= s.config.MaxRunning {
			return Profile{}, fmt.Errorf("running sandbox limit (%d) reached", s.config.MaxRunning)
		}
	}
	if state == profile.DesiredState {
		return profile, nil
	}
	operation := OperationStart
	if state == StateSuspended {
		operation = OperationPause
	}
	if err := s.beginOperation(ctx, profile, operation); err != nil {
		return Profile{}, err
	}
	if state == StateRunning {
		if err := s.control.SyncRuntimeHelper(ctx, profile); err != nil {
			s.failOperation(ctx, id, err)
			return Profile{}, err
		}
	}
	if err := s.control.SetOperatingMode(ctx, profile, state); err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	if err := s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"desired_state": state, "updated_at": time.Now()}).Error; err != nil {
		s.failOperation(ctx, id, err)
		return Profile{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.control.Delete(ctx, profile); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&dbmodel.Sandbox{}, id).Error
}

func (s *Service) ResetWorkspace(ctx context.Context, id int64) error {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if profile.Operation != "" || profile.RuntimeStatus != "Suspended" {
		return errors.New("sandbox must be fully suspended before resetting its workspace")
	}
	return s.control.ResetWorkspace(ctx, profile)
}

func (s *Service) ListAgentEnvironment(ctx context.Context, agentID int64) ([]EnvironmentVariable, error) {
	var rows []dbmodel.AgentEnvironmentVariable
	if err := s.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]EnvironmentVariable, 0, len(rows))
	for _, row := range rows {
		item := EnvironmentVariable{Name: row.Name, Secret: row.IsSecret, HasValue: row.ValueCiphertext != ""}
		if !row.IsSecret {
			item.Value, _ = s.box.Decrypt(row.ValueCiphertext, envAAD(agentID, row.Name))
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) ReplaceAgentEnvironment(ctx context.Context, agentID int64, input []EnvironmentWrite) ([]EnvironmentVariable, error) {
	if !s.Enabled() {
		return nil, errors.New("sandbox support is disabled")
	}
	if len(input) > 100 {
		return nil, errors.New("an agent can have at most 100 environment variables")
	}
	var existing []dbmodel.AgentEnvironmentVariable
	if err := s.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&existing).Error; err != nil {
		return nil, err
	}
	existingByName := make(map[string]dbmodel.AgentEnvironmentVariable, len(existing))
	for _, row := range existing {
		existingByName[row.Name] = row
	}
	rows := make([]dbmodel.AgentEnvironmentVariable, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	totalBytes := 0
	for _, item := range input {
		name := strings.TrimSpace(item.Name)
		if err := validateEnvironmentName(name); err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate environment variable %s", name)
		}
		seen[name] = struct{}{}
		ciphertext := ""
		if item.KeepCurrent {
			old, ok := existingByName[name]
			if !ok {
				return nil, fmt.Errorf("cannot keep missing environment variable %s", name)
			}
			ciphertext = old.ValueCiphertext
		} else {
			if len(item.Value) > 64*1024 {
				return nil, fmt.Errorf("environment variable %s exceeds 64 KiB", name)
			}
			var err error
			ciphertext, err = s.box.Encrypt(item.Value, envAAD(agentID, name))
			if err != nil {
				return nil, err
			}
		}
		totalBytes += len(name) + len(item.Value)
		if totalBytes > 512*1024 {
			return nil, errors.New("agent environment exceeds the 512 KiB total limit")
		}
		rows = append(rows, dbmodel.AgentEnvironmentVariable{AgentID: agentID, Name: name, ValueCiphertext: ciphertext, IsSecret: item.Secret})
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&dbmodel.AgentEnvironmentVariable{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			return tx.Create(&rows).Error
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.ListAgentEnvironment(ctx, agentID)
}

func (s *Service) AgentEnvironmentValues(ctx context.Context, agentID int64) (map[string]string, error) {
	return s.agentEnvironmentValues(ctx, agentID, false)
}

func (s *Service) agentSecretValues(ctx context.Context, agentID int64) (map[string]string, error) {
	return s.agentEnvironmentValues(ctx, agentID, true)
}

func (s *Service) agentEnvironmentValues(ctx context.Context, agentID int64, onlySecrets bool) (map[string]string, error) {
	var rows []dbmodel.AgentEnvironmentVariable
	if err := s.db.WithContext(ctx).Where("agent_id = ?", agentID).Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		if onlySecrets && !row.IsSecret {
			continue
		}
		value, err := s.box.Decrypt(row.ValueCiphertext, envAAD(agentID, row.Name))
		if err != nil {
			return nil, fmt.Errorf("decrypt agent environment variable %s: %w", row.Name, err)
		}
		values[row.Name] = value
	}
	return values, nil
}

func (s *Service) StartProcess(ctx context.Context, agentID, sessionID int64, request ExecRequest) (Process, error) {
	profile, target, err := s.targetForAgent(ctx, agentID)
	if err != nil {
		return Process{}, err
	}
	environment, err := s.AgentEnvironmentValues(ctx, agentID)
	if err != nil {
		return Process{}, err
	}
	secrets, err := s.agentSecretValues(ctx, agentID)
	if err != nil {
		return Process{}, err
	}
	request.ID = uuid.NewString()
	request.Kind = ""
	request.AgentID, request.SessionID, request.Environment = agentID, sessionID, environment
	request.YieldTimeMS = ExecYieldTimeMS(request.YieldTimeMS)
	if strings.TrimSpace(request.Workdir) == "" {
		request.Workdir = "/workspace"
	}
	started := time.Now().UTC()
	redactedCommand := redact(request.Command, secrets)
	row := dbmodel.SandboxRun{ID: request.ID, SandboxID: profile.ID, AgentID: agentID, SessionID: sessionID, Command: redactedCommand, TTY: request.TTY, Status: RunStarting, StartedAt: started}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Process{}, err
	}
	process, err := s.runtime.Exec(ctx, target, request)
	if err != nil {
		reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if recovered, getErr := s.runtime.GetProcess(reconcileCtx, target, request.ID); getErr == nil {
			recovered.SandboxID, recovered.AgentID, recovered.SessionID = profile.ID, agentID, sessionID
			recovered.TTY = boolPointer(request.TTY)
			s.persistProcess(reconcileCtx, recovered, secrets)
		} else {
			now := time.Now().UTC()
			_ = s.db.WithContext(reconcileCtx).Model(&dbmodel.SandboxRun{}).Where("id = ?", request.ID).Updates(map[string]any{"status": RunFailed, "finished_at": now, "output_tail": redact(err.Error(), secrets)}).Error
		}
		return Process{}, err
	}
	process.SandboxID = profile.ID
	process.AgentID, process.SessionID = agentID, sessionID
	process.TTY = boolPointer(request.TTY)
	s.persistProcess(ctx, process, secrets)
	return redactProcess(process, secrets), nil
}

func (s *Service) StartWebTerminal(ctx context.Context, sandboxID int64) (Process, error) {
	profile, err := s.Get(ctx, sandboxID)
	if err != nil {
		return Process{}, err
	}
	if profile.RuntimeStatus != "Ready" {
		return Process{}, fmt.Errorf("sandbox is %s; Web Terminal requires Ready", profile.RuntimeStatus)
	}
	profile, target, err := s.runtimeTarget(profile)
	if err != nil {
		return Process{}, err
	}
	request := ExecRequest{
		ID: uuid.NewString(), Kind: "web_terminal", Command: "exec /bin/sh -l", Workdir: "/workspace",
		TTY: true, Background: true, YieldTimeMS: MinYieldTimeMS,
	}
	process, err := s.runtime.Exec(ctx, target, request)
	if err != nil {
		return Process{}, err
	}
	process.SandboxID = profile.ID
	return process, nil
}

func (s *Service) WriteWebTerminal(ctx context.Context, sandboxID int64, processID, chars string) (Process, error) {
	_, target, process, err := s.webTerminalTarget(ctx, sandboxID, processID)
	if err != nil {
		return Process{}, err
	}
	input := StdinRequest{Chars: chars, MaxYieldTimeMS: 5_000}
	if chars == "" {
		input.YieldTimeMS = 5_000
	} else {
		input.YieldTimeMS = MinYieldTimeMS
	}
	updated, err := s.runtime.WriteStdin(ctx, target, process.ID, input)
	if err != nil {
		return Process{}, err
	}
	updated.SandboxID = sandboxID
	return updated, nil
}

func (s *Service) GetWebTerminal(ctx context.Context, sandboxID int64, processID string) (Process, error) {
	_, _, process, err := s.webTerminalTarget(ctx, sandboxID, processID)
	if err != nil {
		return Process{}, err
	}
	process.SandboxID = sandboxID
	return process, nil
}

func (s *Service) ResizeWebTerminal(ctx context.Context, sandboxID int64, processID string, input ResizeRequest) error {
	_, target, process, err := s.webTerminalTarget(ctx, sandboxID, processID)
	if err != nil {
		return err
	}
	return s.runtime.Resize(ctx, target, process.ID, input)
}

func (s *Service) StopWebTerminal(ctx context.Context, sandboxID int64, processID, signal string) (Process, error) {
	_, target, process, err := s.webTerminalTarget(ctx, sandboxID, processID)
	if err != nil {
		return Process{}, err
	}
	updated, err := s.runtime.Signal(ctx, target, process.ID, signal)
	if err != nil {
		return Process{}, err
	}
	updated.SandboxID = sandboxID
	return updated, nil
}

func (s *Service) webTerminalTarget(ctx context.Context, sandboxID int64, processID string) (Profile, RuntimeTarget, Process, error) {
	profile, target, err := s.targetForSandbox(ctx, sandboxID)
	if err != nil {
		return Profile{}, RuntimeTarget{}, Process{}, err
	}
	process, err := s.runtime.GetProcess(ctx, target, processID)
	if err != nil {
		return Profile{}, RuntimeTarget{}, Process{}, err
	}
	if process.Kind != "web_terminal" {
		return Profile{}, RuntimeTarget{}, Process{}, errors.New("process is not a Web Terminal")
	}
	return profile, target, process, nil
}

func (s *Service) ListProcessesForSession(ctx context.Context, sessionID int64) ([]Process, error) {
	var session dbmodel.Session
	if err := s.db.WithContext(ctx).First(&session, sessionID).Error; err != nil {
		return nil, err
	}
	if session.AgentID == nil {
		return []Process{}, nil
	}
	profile, target, err := s.targetForAgent(ctx, *session.AgentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "not associated") {
			return []Process{}, nil
		}
		return s.persistedProcesses(ctx, sessionID)
	}
	processes, err := s.runtime.ListProcesses(ctx, target, &sessionID)
	if err != nil {
		return s.persistedProcesses(ctx, sessionID)
	}
	secrets, _ := s.agentSecretValues(ctx, *session.AgentID)
	live := make(map[string]struct{}, len(processes))
	for i := range processes {
		live[processes[i].ID] = struct{}{}
		processes[i].SandboxID = profile.ID
		s.persistProcess(ctx, processes[i], secrets)
		processes[i] = redactProcess(processes[i], secrets)
	}
	s.reconcileLostProcesses(ctx, profile.ID, &sessionID, live)
	sort.Slice(processes, func(i, j int) bool { return processes[i].StartedAt.After(processes[j].StartedAt) })
	return processes, nil
}

func (s *Service) ListProcessesForAgent(ctx context.Context, agentID int64) ([]Process, error) {
	profile, target, err := s.targetForAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	processes, err := s.runtime.ListProcesses(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(processes))
	visible := make([]Process, 0, len(processes))
	for i := range processes {
		if processes[i].Kind == "web_terminal" {
			continue
		}
		live[processes[i].ID] = struct{}{}
		processes[i].SandboxID = profile.ID
		secrets, _ := s.agentSecretValues(ctx, processes[i].AgentID)
		s.persistProcess(ctx, processes[i], secrets)
		visible = append(visible, redactProcess(processes[i], secrets))
	}
	s.reconcileLostProcesses(ctx, profile.ID, nil, live)
	sort.Slice(visible, func(i, j int) bool { return visible[i].StartedAt.After(visible[j].StartedAt) })
	return visible, nil
}

func (s *Service) WriteProcessStdin(ctx context.Context, actorAgentID int64, processID string, input StdinRequest) (Process, error) {
	target, environment, err := s.targetForProcess(ctx, actorAgentID, processID)
	if err != nil {
		return Process{}, err
	}
	input.MaxYieldTimeMS = s.config.BackgroundTerminalMaxTimeoutMS
	input.YieldTimeMS = StdinYieldTimeMS(input.Chars, input.YieldTimeMS, input.MaxYieldTimeMS)
	process, err := s.runtime.WriteStdin(ctx, target, processID, input)
	if err != nil {
		return Process{}, err
	}
	s.persistProcess(ctx, process, environment)
	return redactProcess(process, environment), nil
}

func (s *Service) SignalProcess(ctx context.Context, actorAgentID int64, processID, signal string) (Process, error) {
	signal = strings.ToUpper(strings.TrimSpace(signal))
	if signal != "TERM" && signal != "KILL" {
		return Process{}, errors.New("signal must be TERM or KILL")
	}
	target, environment, err := s.targetForProcess(ctx, actorAgentID, processID)
	if err != nil {
		return Process{}, err
	}
	process, err := s.runtime.Signal(ctx, target, processID, signal)
	if err != nil {
		return Process{}, err
	}
	s.persistProcess(ctx, process, environment)
	return redactProcess(process, environment), nil
}

func (s *Service) GetProcess(ctx context.Context, actorAgentID int64, processID string) (Process, error) {
	target, environment, err := s.targetForProcess(ctx, actorAgentID, processID)
	if err != nil {
		return Process{}, err
	}
	process, err := s.runtime.GetProcess(ctx, target, processID)
	if err != nil {
		return Process{}, err
	}
	s.persistProcess(ctx, process, environment)
	return redactProcess(process, environment), nil
}

func (s *Service) ProcessAgentID(ctx context.Context, processID string) (int64, error) {
	var run dbmodel.SandboxRun
	if err := s.db.WithContext(ctx).Select("agent_id").First(&run, "id = ?", processID).Error; err != nil {
		return 0, err
	}
	return run.AgentID, nil
}

func (s *Service) targetForAgent(ctx context.Context, agentID int64) (Profile, RuntimeTarget, error) {
	if !s.Enabled() {
		return Profile{}, RuntimeTarget{}, errors.New("sandbox support is disabled")
	}
	var agent dbmodel.Agent
	if err := s.db.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	if agent.SandboxID == nil {
		return Profile{}, RuntimeTarget{}, errors.New("agent is not associated with a sandbox")
	}
	var row dbmodel.Sandbox
	if err := s.db.WithContext(ctx).First(&row, *agent.SandboxID).Error; err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	profile, err := s.profileFromRow(ctx, row, false)
	if err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	return s.runtimeTarget(profile)
}

func (s *Service) targetForSandbox(ctx context.Context, sandboxID int64) (Profile, RuntimeTarget, error) {
	if !s.Enabled() {
		return Profile{}, RuntimeTarget{}, errors.New("sandbox support is disabled")
	}
	var row dbmodel.Sandbox
	if err := s.db.WithContext(ctx).First(&row, sandboxID).Error; err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	profile, err := s.profileFromRow(ctx, row, false)
	if err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	return s.runtimeTarget(profile)
}

func (s *Service) runtimeTarget(profile Profile) (Profile, RuntimeTarget, error) {
	if profile.DesiredState != StateRunning {
		return Profile{}, RuntimeTarget{}, errors.New("sandbox is suspended")
	}
	clientKey, err := s.box.Decrypt(profile.RuntimeClientKeyCiphertext, "sandbox:"+profile.KubernetesName+":client-key")
	if err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	token, err := s.box.Decrypt(profile.RuntimeTokenCiphertext, "sandbox:"+profile.KubernetesName+":token")
	if err != nil {
		return Profile{}, RuntimeTarget{}, err
	}
	host := profile.KubernetesName + "." + s.config.Namespace + ".svc"
	target := RuntimeTarget{
		BaseURL: "https://" + host + ":" + fmt.Sprintf("%d", s.config.RuntimePort), ServerName: host,
		CAPEM: profile.RuntimeCAPEM, ClientCertPEM: profile.RuntimeClientCertPEM, ClientKeyPEM: clientKey, Token: token,
	}
	return profile, target, nil
}

func (s *Service) targetForProcess(ctx context.Context, actorAgentID int64, processID string) (RuntimeTarget, map[string]string, error) {
	var run dbmodel.SandboxRun
	if err := s.db.WithContext(ctx).First(&run, "id = ?", processID).Error; err != nil {
		return RuntimeTarget{}, nil, err
	}
	actorProfile, target, err := s.targetForAgent(ctx, actorAgentID)
	if err != nil {
		return RuntimeTarget{}, nil, err
	}
	if actorProfile.ID != run.SandboxID {
		return RuntimeTarget{}, nil, errors.New("process belongs to a different sandbox")
	}
	secrets, err := s.agentSecretValues(ctx, run.AgentID)
	return target, secrets, err
}

func (s *Service) profileFromRow(ctx context.Context, row dbmodel.Sandbox, resolveStatus bool) (Profile, error) {
	var agents []dbmodel.Agent
	if err := s.db.WithContext(ctx).Where("sandbox_id = ?", row.ID).Order("id ASC").Find(&agents).Error; err != nil {
		return Profile{}, err
	}
	agentIDs := make([]int64, 0, len(agents))
	for _, agent := range agents {
		agentIDs = append(agentIDs, agent.ID)
	}
	profile := Profile{
		ID: row.ID, Name: row.Name, Description: row.Description, Image: row.Image,
		CPULimitMillis: row.CPULimitMillis, MemoryLimitMiB: row.MemoryLimitMiB, EphemeralStorageMiB: row.EphemeralStorageMiB,
		WorkspaceStorageMiB: row.WorkspaceStorageMiB, DesiredState: row.DesiredState, Revision: row.Revision,
		AppliedRevision: row.AppliedRevision, PendingRestart: row.AppliedRevision != row.Revision, KubernetesName: row.KubernetesName,
		LastError: row.LastError, AgentIDs: agentIDs, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		RuntimeCAPEM: row.RuntimeCAPEM, RuntimeClientCertPEM: row.RuntimeClientCertPEM,
		RuntimeClientKeyCiphertext: row.RuntimeClientKeyCiphertext, RuntimeTokenCiphertext: row.RuntimeTokenCiphertext,
		Operation: row.RuntimeOperation, OperationStartedAt: row.RuntimeOperationStartedAt, OperationPreviousPodUID: row.RuntimeOperationPodUID,
	}
	if resolveStatus && s.Enabled() {
		details, err := s.control.Status(ctx, profile)
		if err == nil {
			if profile.Operation != "" && details.State == "Error" {
				message := details.Message
				if message == "" {
					message = fmt.Sprintf("Sandbox %s operation failed", profile.Operation)
				}
				s.failOperation(ctx, profile.ID, errors.New(message))
				profile.Operation = ""
				profile.OperationStartedAt = nil
				details.Operation = ""
				details.OperationStartedAt = nil
				profile.LastError = &message
			} else if operationCompleted(profile, details) {
				s.clearOperation(ctx, profile.ID)
				profile.Operation = ""
				profile.OperationStartedAt = nil
				details.Operation = ""
				details.OperationStartedAt = nil
			}
			if profile.OperationStartedAt != nil && time.Since(*profile.OperationStartedAt) > 5*time.Minute && details.State != "Ready" && details.State != "Suspended" {
				message := fmt.Sprintf("Sandbox %s operation did not complete within five minutes", profile.Operation)
				s.failOperation(ctx, profile.ID, errors.New(message))
				profile.Operation = ""
				profile.OperationStartedAt = nil
				details.Operation = ""
				details.OperationStartedAt = nil
				details.State = "Error"
				details.Message = message
				profile.LastError = &message
			}
			profile.RuntimeStatus = details.State
			profile.RuntimeDetails = &details
			if details.State == "Error" && details.Message != "" {
				profile.LastError = stringPtr(details.Message)
			}
		} else {
			profile.RuntimeStatus = "Unavailable"
			message := err.Error()
			profile.LastError = &message
			profile.RuntimeDetails = &RuntimeDetails{State: "Unavailable", Message: message, ObservedAt: time.Now().UTC(), Operation: profile.Operation, OperationStartedAt: profile.OperationStartedAt}
		}
	} else if !s.Enabled() {
		profile.RuntimeStatus = "Disabled"
		profile.RuntimeDetails = &RuntimeDetails{State: "Disabled", ObservedAt: time.Now().UTC()}
	}
	return profile, nil
}

func (s *Service) normalizeWrite(input Write, current *Profile) (Write, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Image = strings.TrimSpace(input.Image)
	if input.Name == "" || len([]rune(input.Name)) > 128 {
		return Write{}, errors.New("name is required and must be at most 128 characters")
	}
	if len([]rune(input.Description)) > 4096 {
		return Write{}, errors.New("description must be at most 4096 characters")
	}
	if input.Image == "" {
		input.Image = s.config.DefaultImage
	}
	if len(input.Image) > 1024 || strings.ContainsAny(input.Image, " \t\r\n") {
		return Write{}, errors.New("image must be a valid OCI image reference without whitespace")
	}
	if input.CPULimitMillis <= 0 {
		input.CPULimitMillis = s.config.DefaultCPULimitMillis
	}
	if input.MemoryLimitMiB <= 0 {
		input.MemoryLimitMiB = s.config.DefaultMemoryLimitMiB
	}
	if input.EphemeralStorageMiB <= 0 {
		input.EphemeralStorageMiB = s.config.DefaultEphemeralStorageMiB
	}
	if input.WorkspaceStorageMiB <= 0 {
		if current != nil {
			input.WorkspaceStorageMiB = current.WorkspaceStorageMiB
		} else {
			input.WorkspaceStorageMiB = s.config.DefaultWorkspaceStorageMiB
		}
	}
	if input.CPULimitMillis > s.config.MaxCPULimitMillis || input.MemoryLimitMiB > s.config.MaxMemoryLimitMiB || input.EphemeralStorageMiB > s.config.MaxEphemeralStorageMiB || input.WorkspaceStorageMiB > s.config.MaxWorkspaceStorageMiB {
		return Write{}, errors.New("sandbox resource request exceeds the configured maximum")
	}
	return input, nil
}

func bindAgents(tx *gorm.DB, sandboxID int64, agentIDs []int64) error {
	if err := tx.Model(&dbmodel.Agent{}).Where("sandbox_id = ?", sandboxID).Update("sandbox_id", nil).Error; err != nil {
		return err
	}
	seen := make(map[int64]struct{}, len(agentIDs))
	for _, id := range agentIDs {
		if id <= 0 {
			return errors.New("agent_ids must contain positive IDs")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result := tx.Model(&dbmodel.Agent{}).Where("id = ? AND archived_at IS NULL", id).Update("sandbox_id", sandboxID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("agent %d does not exist or is archived", id)
		}
	}
	return nil
}

func (s *Service) markApplied(ctx context.Context, id, revision int64) error {
	return s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"applied_revision": revision, "last_error": nil, "updated_at": time.Now()}).Error
}

func (s *Service) beginOperation(ctx context.Context, profile Profile, operation string) error {
	podUID := ""
	if profile.RuntimeDetails != nil && profile.RuntimeDetails.Pod != nil {
		podUID = profile.RuntimeDetails.Pod.UID
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).
		Where("id = ? AND runtime_operation = ''", profile.ID).
		Updates(map[string]any{"runtime_operation": operation, "runtime_operation_started_at": now, "runtime_operation_pod_uid": podUID, "last_error": nil, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOperationInProgress
	}
	profile.Operation = operation
	profile.OperationStartedAt = &now
	profile.OperationPreviousPodUID = podUID
	return nil
}

func (s *Service) clearOperation(ctx context.Context, id int64) {
	_ = s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"runtime_operation": "", "runtime_operation_started_at": nil, "runtime_operation_pod_uid": "", "updated_at": time.Now(),
	}).Error
}

func (s *Service) failOperation(ctx context.Context, id int64, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	_ = s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"runtime_operation": "", "runtime_operation_started_at": nil, "runtime_operation_pod_uid": "", "last_error": message, "updated_at": time.Now(),
	}).Error
}

func operationCompleted(profile Profile, details RuntimeDetails) bool {
	if profile.Operation == "" {
		return false
	}
	switch profile.Operation {
	case OperationPause:
		return details.State == "Suspended"
	case OperationStart:
		return details.State == "Ready"
	case OperationApply, OperationRestart:
		return details.State == "Ready" && details.Pod != nil && (profile.OperationPreviousPodUID == "" || details.Pod.UID != profile.OperationPreviousPodUID)
	default:
		return true
	}
}

func (s *Service) setProvisionError(ctx context.Context, id int64, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	_ = s.db.WithContext(ctx).Model(&dbmodel.Sandbox{}).Where("id = ?", id).Updates(map[string]any{"last_error": message, "updated_at": time.Now()}).Error
}

func (s *Service) persistProcess(ctx context.Context, process Process, environment map[string]string) {
	if process.ID == "" {
		return
	}
	status := process.Status
	if status == "" {
		status = RunRunning
	}
	output := redact(process.Output, environment)
	if len(output) > 256*1024 {
		output = output[len(output)-256*1024:]
		process.OutputTruncated = true
	}
	updates := map[string]any{
		"status": status, "pid": nullablePID(process.PID), "exit_code": process.ExitCode, "finished_at": process.FinishedAt,
		"output_tail": output, "output_truncated": process.OutputTruncated, "updated_at": time.Now(),
	}
	if process.TTY != nil {
		updates["tty"] = *process.TTY
	}
	_ = s.db.WithContext(ctx).Model(&dbmodel.SandboxRun{}).Where("id = ?", process.ID).Updates(updates).Error
}

func (s *Service) persistedProcesses(ctx context.Context, sessionID int64) ([]Process, error) {
	var rows []dbmodel.SandboxRun
	if err := s.db.WithContext(ctx).Where("session_id = ? AND status IN ?", sessionID, []string{RunStarting, RunRunning}).Order("started_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]Process, 0, len(rows))
	for _, row := range rows {
		pid := int64(0)
		if row.PID != nil {
			pid = *row.PID
		}
		result = append(result, Process{ID: row.ID, SandboxID: row.SandboxID, AgentID: row.AgentID, SessionID: row.SessionID, Command: row.Command, TTY: boolPointer(row.TTY), Status: row.Status, PID: pid, ExitCode: row.ExitCode, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, DurationMillis: time.Since(row.StartedAt).Milliseconds(), Output: row.OutputTail, OutputTruncated: row.OutputTruncated})
	}
	return result, nil
}

func (s *Service) reconcileLostProcesses(ctx context.Context, sandboxID int64, sessionID *int64, live map[string]struct{}) {
	query := s.db.WithContext(ctx).Where("sandbox_id = ? AND status IN ?", sandboxID, []string{RunStarting, RunRunning})
	if sessionID != nil {
		query = query.Where("session_id = ?", *sessionID)
	}
	var rows []dbmodel.SandboxRun
	if err := query.Find(&rows).Error; err != nil {
		return
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if _, ok := live[row.ID]; ok {
			continue
		}
		_ = s.db.WithContext(ctx).Model(&dbmodel.SandboxRun{}).Where("id = ? AND status IN ?", row.ID, []string{RunStarting, RunRunning}).Updates(map[string]any{
			"status": RunLost, "finished_at": now, "updated_at": now,
		}).Error
	}
}

func validateEnvironmentName(name string) error {
	if !envNamePattern.MatchString(name) || len(name) > 128 {
		return fmt.Errorf("environment variable %q has an invalid name", name)
	}
	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "HARO_") || strings.HasPrefix(upper, "KUBERNETES_") {
		return fmt.Errorf("environment variable %s uses a reserved prefix", name)
	}
	return nil
}

func envAAD(agentID int64, name string) string { return fmt.Sprintf("agent:%d:env:%s", agentID, name) }

func redact(value string, environment map[string]string) string {
	for _, secret := range environment {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func redactProcess(process Process, environment map[string]string) Process {
	process.Command = redact(process.Command, environment)
	process.Output = redact(process.Output, environment)
	process.InteractionOutput = redact(process.InteractionOutput, environment)
	return process
}

func nullablePID(pid int64) any {
	if pid <= 0 {
		return nil
	}
	return pid
}

func boolPointer(value bool) *bool { return &value }

func newKubernetesName(name string) (string, error) {
	var builder strings.Builder
	for _, char := range strings.ToLower(name) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		} else if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-") {
			builder.WriteByte('-')
		}
	}
	base := strings.Trim(builder.String(), "-")
	if base == "" {
		base = "sandbox"
	}
	if len(base) > 48 {
		base = strings.TrimRight(base[:48], "-")
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "haro-" + base + "-" + hex.EncodeToString(random), nil
}
