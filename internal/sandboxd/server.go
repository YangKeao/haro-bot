package sandboxd

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skillbundle"
	"github.com/creack/pty"
)

const (
	maxLogBytes         = 1 << 20
	maxListOutputBytes  = 8 << 10
	maxActiveProcesses  = 64
	maxTrackedProcesses = 256
	maxSkillCacheBytes  = 512 << 20
	maxMCPClients       = 64
)

type Server struct {
	workspace string
	token     string

	mu         sync.RWMutex
	skillsMu   sync.Mutex
	processes  map[string]*managedProcess
	pending    map[string]struct{}
	mcpClients map[string]*managedMCPClient
}

type managedProcess struct {
	mu              sync.Mutex
	interactionMu   sync.Mutex
	info            sandbox.Process
	command         *exec.Cmd
	stdin           io.WriteCloser
	terminal        *os.File
	done            chan struct{}
	readers         sync.WaitGroup
	log             []byte
	logOffset       int64
	outputTruncated bool
	unread          []byte
	unreadDropped   int
}

func New(workspace, token string) (*Server, error) {
	workspace = filepath.Clean(strings.TrimSpace(workspace))
	if !filepath.IsAbs(workspace) {
		return nil, errors.New("workspace must be an absolute path")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("runtime token is required")
	}
	if err := os.MkdirAll(filepath.Join(workspace, "home"), 0700); err != nil {
		return nil, err
	}
	return &Server{
		workspace:  workspace,
		token:      strings.TrimSpace(token),
		processes:  make(map[string]*managedProcess),
		pending:    make(map[string]struct{}),
		mcpClients: make(map[string]*managedMCPClient),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/processes", s.handleList)
	mux.HandleFunc("POST /v1/processes", s.handleStart)
	mux.HandleFunc("GET /v1/processes/{processID}", s.handleGet)
	mux.HandleFunc("POST /v1/processes/{processID}/stdin", s.handleStdin)
	mux.HandleFunc("POST /v1/processes/{processID}/resize", s.handleResize)
	mux.HandleFunc("POST /v1/processes/{processID}/signal", s.handleSignal)
	mux.HandleFunc("PUT /v1/skills/{hash}", s.handleEnsureSkill)
	mux.HandleFunc("PUT /v1/files", s.handleWriteFile)
	mux.HandleFunc("POST /v1/mcp/tools", s.handleListMCPTools)
	mux.HandleFunc("POST /v1/mcp/call", s.handleCallMCPTool)
	mux.HandleFunc("DELETE /v1/mcp/sessions/{key}", s.handleCloseMCPSession)
	return s.authenticate(mux)
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	request := sandbox.FileWriteRequest{
		Path:      strings.TrimSpace(r.URL.Query().Get("path")),
		Overwrite: r.URL.Query().Get("overwrite") == "true",
		SHA256:    strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sha256"))),
	}
	if request.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if request.SHA256 != "" && (!isLowerHex(request.SHA256) || len(request.SHA256) != 64) {
		writeError(w, http.StatusBadRequest, "sha256 must be a 64-character lowercase hexadecimal digest")
		return
	}
	result, err := s.writeWorkspaceFile(r.Body, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) writeWorkspaceFile(reader io.Reader, request sandbox.FileWriteRequest) (result sandbox.FileWriteResult, err error) {
	target, err := s.fileDestination(request.Path)
	if err != nil {
		return result, err
	}
	parent := filepath.Dir(target)
	if err := s.ensureWorkspaceDirectories(parent); err != nil {
		return result, err
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return result, errors.New("destination must not be a symbolic link")
		}
		if info.IsDir() {
			return result, errors.New("destination is a directory")
		}
		if !request.Overwrite {
			return result, errors.New("destination already exists; set overwrite to true to replace it")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return result, statErr
	}
	temp, err := os.CreateTemp(parent, ".haro-attachment-")
	if err != nil {
		return result, err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), reader)
	if err != nil {
		return result, err
	}
	actualHash := fmt.Sprintf("%x", hash.Sum(nil))
	if request.SHA256 != "" && actualHash != request.SHA256 {
		return result, errors.New("attachment sha256 mismatch")
	}
	if err := temp.Sync(); err != nil {
		return result, err
	}
	if err := temp.Close(); err != nil {
		return result, err
	}
	if request.Overwrite {
		if err := os.Rename(tempName, target); err != nil {
			return result, err
		}
	} else {
		if err := os.Link(tempName, target); err != nil {
			if errors.Is(err, os.ErrExist) {
				return result, errors.New("destination already exists; set overwrite to true to replace it")
			}
			return result, err
		}
		if err := os.Remove(tempName); err != nil {
			return result, err
		}
	}
	return sandbox.FileWriteResult{Path: target, SizeBytes: size, SHA256: actualHash}, nil
}

func (s *Server) fileDestination(raw string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(raw))
	var relative string
	if filepath.IsAbs(clean) {
		var err error
		relative, err = filepath.Rel(s.workspace, clean)
		if err != nil {
			return "", err
		}
	} else {
		relative = clean
	}
	if relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("destination must be a file within /workspace")
	}
	return filepath.Join(s.workspace, relative), nil
}

func (s *Server) ensureWorkspaceDirectories(parent string) error {
	relative, err := filepath.Rel(s.workspace, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("destination must be within /workspace")
	}
	current := s.workspace
	if relative == "." {
		return nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("destination path must not contain symbolic links")
		}
		if !info.IsDir() {
			return errors.New("destination parent is not a directory")
		}
	}
	return nil
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) handleEnsureSkill(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.PathValue("hash"))
	if !skillbundle.ValidHash(hash) {
		writeError(w, http.StatusBadRequest, "invalid skill bundle hash")
		return
	}
	s.skillsMu.Lock()
	defer s.skillsMu.Unlock()
	parent := filepath.Join(s.workspace, ".haro", "skills", "sha256")
	target := filepath.Join(parent, hash)
	if manifest, err := skillbundle.Scan(target); err == nil && manifest.Hash == hash {
		now := time.Now()
		_ = os.Chtimes(target, now, now)
		s.pruneSkillCache(parent, hash)
		writeJSON(w, http.StatusOK, sandbox.SkillMaterialization{SkillRoot: target, Reused: true})
		return
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	temp, err := os.MkdirTemp(parent, ".skill-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(temp)
	manifest, err := skillbundle.ExtractArchive(http.MaxBytesReader(w, r.Body, skillbundle.MaxArchiveSize), temp)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if manifest.Hash != hash {
		writeError(w, http.StatusBadRequest, "skill bundle hash mismatch")
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(temp, target); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.pruneSkillCache(parent, hash)
	writeJSON(w, http.StatusCreated, sandbox.SkillMaterialization{SkillRoot: target})
}

func (s *Server) pruneSkillCache(parent, keepHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) > 0 {
		return
	}
	for _, process := range s.processes {
		process.mu.Lock()
		status := process.info.Status
		process.mu.Unlock()
		if status == sandbox.RunStarting || status == sandbox.RunRunning {
			return
		}
	}
	type cachedSkill struct {
		path   string
		hash   string
		size   int64
		usedAt time.Time
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	var cached []cachedSkill
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || !skillbundle.ValidHash(entry.Name()) {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		manifest, err := skillbundle.Scan(path)
		if err != nil || manifest.Hash != entry.Name() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += manifest.TotalSize
		cached = append(cached, cachedSkill{path: path, hash: entry.Name(), size: manifest.TotalSize, usedAt: info.ModTime()})
	}
	if total <= maxSkillCacheBytes {
		return
	}
	sort.Slice(cached, func(i, j int) bool { return cached[i].usedAt.Before(cached[j].usedAt) })
	for _, item := range cached {
		if total <= maxSkillCacheBytes {
			break
		}
		if item.hash == keepHash {
			continue
		}
		if os.RemoveAll(item.path) == nil {
			total -= item.size
		}
	}
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var input sandbox.ExecRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	webTerminal := input.Kind == "web_terminal"
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Command) == "" || (!webTerminal && (input.AgentID <= 0 || input.SessionID <= 0)) {
		writeError(w, http.StatusBadRequest, "id, agent_id, session_id and command are required")
		return
	}
	if len(input.Command) > 1<<20 || len(input.Environment) > 100 {
		writeError(w, http.StatusBadRequest, "command or environment is too large")
		return
	}
	s.mu.Lock()
	if s.activeProcessCountLocked()+len(s.pending) >= maxActiveProcesses {
		s.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "sandbox active process limit reached")
		return
	}
	s.pruneCompletedLocked(maxTrackedProcesses - 1)
	_, running := s.processes[input.ID]
	_, pending := s.pending[input.ID]
	if running || pending {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "process id already exists")
		return
	}
	s.pending[input.ID] = struct{}{}
	s.mu.Unlock()

	process, err := s.start(input)
	s.mu.Lock()
	delete(s.pending, input.ID)
	s.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	canceled := false
	if !input.Background {
		yield := time.Duration(sandbox.ExecYieldTimeMS(input.YieldTimeMS)) * time.Millisecond
		select {
		case <-process.done:
		case <-time.After(yield):
		case <-r.Context().Done():
			canceled = true
		}
	}
	writeJSON(w, http.StatusCreated, process.interactionSnapshot(!canceled))
}

func (s *Server) start(input sandbox.ExecRequest) (*managedProcess, error) {
	workdir, err := s.resolveWorkdir(input.Workdir)
	if err != nil {
		return nil, err
	}
	shell := strings.TrimSpace(input.Shell)
	if shell == "" {
		shell = "/bin/sh"
	}
	login := true
	if input.Login != nil {
		login = *input.Login
	}
	flag := "-c"
	if login {
		flag = "-lc"
	}
	command := exec.Command(shell, flag, input.Command)
	command.Dir = workdir
	command.Env = buildEnvironment(input.Environment, s.workspace, input.TTY)
	tty := input.TTY
	process := &managedProcess{
		info:    sandbox.Process{ID: input.ID, Kind: input.Kind, AgentID: input.AgentID, SessionID: input.SessionID, Command: input.Command, TTY: &tty, Status: sandbox.RunStarting, StartedAt: time.Now().UTC()},
		command: command, done: make(chan struct{}),
	}
	if input.TTY {
		terminal, err := pty.Start(command)
		if err != nil {
			return nil, err
		}
		process.stdin = terminal
		process.terminal = terminal
		process.info.PID = int64(command.Process.Pid)
		process.info.Status = sandbox.RunRunning
		s.register(process)
		process.readers.Add(1)
		go process.read(terminal)
	} else {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout, err := command.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr, err := command.StderrPipe()
		if err != nil {
			return nil, err
		}
		stdin, err := command.StdinPipe()
		if err != nil {
			return nil, err
		}
		if err := command.Start(); err != nil {
			return nil, err
		}
		process.stdin = stdin
		process.info.PID = int64(command.Process.Pid)
		process.info.Status = sandbox.RunRunning
		s.register(process)
		process.readers.Add(2)
		go process.read(stdout)
		go process.read(stderr)
	}
	go process.wait()
	return process, nil
}

func (s *Server) handleResize(w http.ResponseWriter, r *http.Request) {
	process, ok := s.get(r.PathValue("processID"))
	if !ok {
		writeError(w, http.StatusNotFound, "process not found")
		return
	}
	var input sandbox.ResizeRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Columns < 2 || input.Rows < 2 || input.Columns > 1000 || input.Rows > 1000 {
		writeError(w, http.StatusBadRequest, "terminal columns and rows must be between 2 and 1000")
		return
	}
	process.mu.Lock()
	terminal := process.terminal
	status := process.info.Status
	process.mu.Unlock()
	if terminal == nil || (status != sandbox.RunStarting && status != sandbox.RunRunning) {
		writeError(w, http.StatusConflict, "process is not an active TTY")
		return
	}
	if err := pty.Setsize(terminal, &pty.Winsize{Cols: input.Columns, Rows: input.Rows}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) register(process *managedProcess) {
	s.mu.Lock()
	s.processes[process.info.ID] = process
	s.mu.Unlock()
}

func (s *Server) activeProcessCountLocked() int {
	count := 0
	for _, process := range s.processes {
		process.mu.Lock()
		status := process.info.Status
		process.mu.Unlock()
		if status == sandbox.RunStarting || status == sandbox.RunRunning {
			count++
		}
	}
	return count
}

func (s *Server) pruneCompletedLocked(target int) {
	for len(s.processes) > target {
		var oldestID string
		var oldest time.Time
		for id, process := range s.processes {
			process.mu.Lock()
			status := process.info.Status
			started := process.info.StartedAt
			process.mu.Unlock()
			if status == sandbox.RunStarting || status == sandbox.RunRunning {
				continue
			}
			if oldestID == "" || started.Before(oldest) {
				oldestID, oldest = id, started
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.processes, oldestID)
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	var sessionID int64
	if value := r.URL.Query().Get("session_id"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "session_id must be a positive integer")
			return
		}
		sessionID = parsed
	}
	s.mu.RLock()
	processes := make([]sandbox.Process, 0, len(s.processes))
	for _, process := range s.processes {
		info := process.summary()
		if sessionID == 0 || info.SessionID == sessionID {
			processes = append(processes, info)
		}
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"processes": processes})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	process, ok := s.get(r.PathValue("processID"))
	if !ok {
		writeError(w, http.StatusNotFound, "process not found")
		return
	}
	writeJSON(w, http.StatusOK, process.snapshot())
}

func (s *Server) handleStdin(w http.ResponseWriter, r *http.Request) {
	process, ok := s.get(r.PathValue("processID"))
	if !ok {
		writeError(w, http.StatusNotFound, "process not found")
		return
	}
	var input sandbox.StdinRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	process.interactionMu.Lock()
	defer process.interactionMu.Unlock()
	process.mu.Lock()
	stdin, status, ttyValue, pid := process.stdin, process.info.Status, process.info.TTY, process.info.PID
	process.mu.Unlock()
	tty := ttyValue != nil && *ttyValue
	if input.Chars != "" {
		if status != sandbox.RunRunning || stdin == nil {
			writeError(w, http.StatusConflict, "process is not running")
			return
		}
		var err error
		if !tty && input.Chars == "\x03" {
			err = syscall.Kill(-int(pid), syscall.SIGINT)
			if errors.Is(err, syscall.ESRCH) {
				err = nil
			}
		} else if !tty {
			writeError(w, http.StatusConflict, "stdin is only available for TTY processes; send \\u0003 to interrupt")
			return
		} else {
			_, err = io.WriteString(stdin, input.Chars)
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if process.info.Kind == "web_terminal" {
			writeJSON(w, http.StatusOK, process.snapshot())
			return
		}
	}
	canceled := false
	yield := sandbox.StdinYieldTimeMS(input.Chars, input.YieldTimeMS, input.MaxYieldTimeMS)
	select {
	case <-process.done:
	case <-time.After(time.Duration(yield) * time.Millisecond):
	case <-r.Context().Done():
		canceled = true
	}
	writeJSON(w, http.StatusOK, process.interactionSnapshot(!canceled))
}

func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	process, ok := s.get(r.PathValue("processID"))
	if !ok {
		writeError(w, http.StatusNotFound, "process not found")
		return
	}
	var input sandbox.SignalRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	var signal syscall.Signal
	switch strings.ToUpper(strings.TrimSpace(input.Signal)) {
	case "TERM":
		signal = syscall.SIGTERM
	case "KILL":
		signal = syscall.SIGKILL
	default:
		writeError(w, http.StatusBadRequest, "signal must be TERM or KILL")
		return
	}
	process.mu.Lock()
	pid := process.info.PID
	running := process.info.Status == sandbox.RunRunning
	process.mu.Unlock()
	if running && pid > 0 {
		if err := syscall.Kill(-int(pid), signal); err != nil {
			if err := syscall.Kill(int(pid), signal); err != nil && !errors.Is(err, syscall.ESRCH) {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, process.snapshot())
}

func (s *Server) get(id string) (*managedProcess, bool) {
	s.mu.RLock()
	process, ok := s.processes[id]
	s.mu.RUnlock()
	return process, ok
}

func (s *Server) resolveWorkdir(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return s.workspace, nil
	}
	target := value
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.workspace, target)
	}
	target = filepath.Clean(target)
	relative, err := filepath.Rel(s.workspace, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("workdir must be inside /workspace")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workdir is not a directory")
	}
	return target, nil
}

func (p *managedProcess) read(reader io.Reader) {
	defer p.readers.Done()
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			p.appendLog(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *managedProcess) appendLog(value []byte) {
	p.mu.Lock()
	p.log = append(p.log, value...)
	p.unread = append(p.unread, value...)
	if len(p.log) > maxLogBytes {
		drop := len(p.log) - maxLogBytes
		p.log = append([]byte(nil), p.log[drop:]...)
		p.logOffset += int64(drop)
		p.outputTruncated = true
	}
	if len(p.unread) > maxLogBytes {
		drop := len(p.unread) - maxLogBytes
		p.unread = append([]byte(nil), p.unread[drop:]...)
		p.unreadDropped += drop
	}
	p.mu.Unlock()
}

func (p *managedProcess) wait() {
	err := p.command.Wait()
	p.readers.Wait()
	now := time.Now().UTC()
	p.mu.Lock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
			p.info.Status = sandbox.RunFailed
		}
	}
	if p.info.Status != sandbox.RunFailed {
		p.info.Status = sandbox.RunExited
	}
	p.info.ExitCode = &exitCode
	p.info.FinishedAt = &now
	p.mu.Unlock()
	close(p.done)
}

func (p *managedProcess) snapshot() sandbox.Process {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := p.info
	result.Output = string(append([]byte(nil), p.log...))
	result.OutputOffset = p.logOffset
	result.OutputTruncated = p.outputTruncated
	result.DurationMillis = time.Since(result.StartedAt).Milliseconds()
	if result.FinishedAt != nil {
		result.DurationMillis = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	}
	result.MemoryBytes = processMemoryBytes(result.PID)
	result.CPUPercent = processCPUPercent(result.PID, result.DurationMillis)
	return result
}

func (p *managedProcess) interactionSnapshot(drain bool) sandbox.Process {
	result := p.snapshot()
	p.mu.Lock()
	result.InteractionOutput = string(append([]byte(nil), p.unread...))
	result.InteractionOutputAvailable = true
	result.InteractionOutputTruncated = p.unreadDropped > 0
	result.InteractionOriginalBytes = len(p.unread) + p.unreadDropped
	if drain {
		p.unread = nil
		p.unreadDropped = 0
	}
	p.mu.Unlock()
	return result
}

func (p *managedProcess) summary() sandbox.Process {
	result := p.snapshot()
	if len(result.Output) > maxListOutputBytes {
		dropped := len(result.Output) - maxListOutputBytes
		result.Output = result.Output[len(result.Output)-maxListOutputBytes:]
		result.OutputTruncated = true
		result.OutputOffset += int64(dropped)
	}
	return result
}

func processCPUPercent(pid, durationMillis int64) float64 {
	if pid <= 0 || durationMillis <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 || closeParen+2 >= len(data) {
		return 0
	}
	fields := strings.Fields(string(data[closeParen+2:]))
	if len(fields) < 13 {
		return 0
	}
	userTicks, errUser := strconv.ParseInt(fields[11], 10, 64)
	systemTicks, errSystem := strconv.ParseInt(fields[12], 10, 64)
	if errUser != nil || errSystem != nil {
		return 0
	}
	// Linux exposes USER_HZ as 100 on the architectures supported by the
	// published image. This is cumulative CPU across the process lifetime.
	return (float64(userTicks+systemTicks) / 100) / (float64(durationMillis) / 1000) * 100
}

func processMemoryBytes(pid int64) int64 {
	if pid <= 0 {
		return 0
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			value, _ := strconv.ParseInt(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func buildEnvironment(overrides map[string]string, workspace string, tty bool) []string {
	values := make(map[string]string)
	for _, pair := range os.Environ() {
		name, value, ok := strings.Cut(pair, "=")
		if ok && !strings.HasPrefix(strings.ToUpper(name), "HARO_") && !strings.HasPrefix(strings.ToUpper(name), "KUBERNETES_") {
			values[name] = value
		}
	}
	values["HOME"] = filepath.Join(workspace, "home")
	values["LANG"] = "C.UTF-8"
	if tty {
		values["TERM"] = "xterm-256color"
	}
	for name, value := range overrides {
		values[name] = value
	}
	result := make([]string, 0, len(values))
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func decodeJSON(w http.ResponseWriter, r *http.Request, output any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
