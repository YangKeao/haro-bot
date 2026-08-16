package sandboxd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/creack/pty"
)

const (
	maxLogBytes         = 1 << 20
	maxListOutputBytes  = 8 << 10
	maxTrackedProcesses = 256
)

type Server struct {
	workspace string
	token     string

	mu        sync.RWMutex
	processes map[string]*managedProcess
	pending   map[string]struct{}
}

type managedProcess struct {
	mu              sync.Mutex
	info            sandbox.Process
	command         *exec.Cmd
	stdin           io.WriteCloser
	done            chan struct{}
	readers         sync.WaitGroup
	log             []byte
	logOffset       int64
	outputTruncated bool
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
		workspace: workspace,
		token:     strings.TrimSpace(token),
		processes: make(map[string]*managedProcess),
		pending:   make(map[string]struct{}),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/processes", s.handleList)
	mux.HandleFunc("POST /v1/processes", s.handleStart)
	mux.HandleFunc("GET /v1/processes/{processID}", s.handleGet)
	mux.HandleFunc("POST /v1/processes/{processID}/stdin", s.handleStdin)
	mux.HandleFunc("POST /v1/processes/{processID}/signal", s.handleSignal)
	return s.authenticate(mux)
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
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Command) == "" || input.AgentID <= 0 || input.SessionID <= 0 {
		writeError(w, http.StatusBadRequest, "id, agent_id, session_id and command are required")
		return
	}
	if len(input.Command) > 1<<20 || len(input.Environment) > 100 {
		writeError(w, http.StatusBadRequest, "command or environment is too large")
		return
	}
	s.mu.Lock()
	if len(s.processes)+len(s.pending) >= maxTrackedProcesses {
		s.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "sandbox process history limit reached; apply/restart the Sandbox to start a fresh runtime")
		return
	}
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
	if !input.Background {
		yield := time.Duration(input.YieldTimeMS) * time.Millisecond
		if yield <= 0 {
			yield = 10 * time.Second
		}
		select {
		case <-process.done:
		case <-time.After(yield):
		case <-r.Context().Done():
		}
	}
	writeJSON(w, http.StatusCreated, process.snapshot())
}

func (s *Server) start(input sandbox.ExecRequest) (*managedProcess, error) {
	workdir, err := s.resolveWorkdir(input.Workdir)
	if err != nil {
		return nil, err
	}
	command := exec.Command("/bin/sh", "-lc", input.Command)
	command.Dir = workdir
	command.Env = buildEnvironment(input.Environment, s.workspace, input.TTY)
	process := &managedProcess{
		info:    sandbox.Process{ID: input.ID, AgentID: input.AgentID, SessionID: input.SessionID, Command: input.Command, Status: sandbox.RunStarting, StartedAt: time.Now().UTC()},
		command: command, done: make(chan struct{}),
	}
	if input.TTY {
		terminal, err := pty.Start(command)
		if err != nil {
			return nil, err
		}
		process.stdin = terminal
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

func (s *Server) register(process *managedProcess) {
	s.mu.Lock()
	s.processes[process.info.ID] = process
	s.mu.Unlock()
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
	process.mu.Lock()
	stdin := process.stdin
	status := process.info.Status
	process.mu.Unlock()
	if status != sandbox.RunRunning || stdin == nil {
		writeError(w, http.StatusConflict, "process is not running")
		return
	}
	if input.Chars != "" {
		if _, err := io.WriteString(stdin, input.Chars); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	if input.YieldTimeMS > 0 {
		select {
		case <-process.done:
		case <-time.After(time.Duration(input.YieldTimeMS) * time.Millisecond):
		case <-r.Context().Done():
		}
	}
	writeJSON(w, http.StatusOK, process.snapshot())
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
	if len(p.log) > maxLogBytes {
		drop := len(p.log) - maxLogBytes
		p.log = append([]byte(nil), p.log[drop:]...)
		p.logOffset += int64(drop)
		p.outputTruncated = true
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
