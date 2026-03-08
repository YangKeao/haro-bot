package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	execDefaultYieldMs  = 10000
	writeDefaultYieldMs = 250
	execMaxBufferBytes  = 1024 * 1024
)

type ExecCommandTool struct {
	fs      *FS
	manager *ExecManager
}

type WriteStdinTool struct {
	manager *ExecManager
}

type execCommandArgs struct {
	Cmd             string `json:"cmd"`
	Workdir         string `json:"workdir"`
	Shell           string `json:"shell"`
	Login           *bool  `json:"login"`
	TTY             bool   `json:"tty"`
	YieldTimeMs     int    `json:"yield_time_ms"`
	MaxOutputTokens *int   `json:"max_output_tokens"`
}

type writeStdinArgs struct {
	SessionID       int    `json:"session_id"`
	Chars           string `json:"chars"`
	YieldTimeMs     int    `json:"yield_time_ms"`
	MaxOutputTokens *int   `json:"max_output_tokens"`
}

func NewExecCommandTool(fs *FS, manager *ExecManager) *ExecCommandTool {
	return &ExecCommandTool{fs: fs, manager: manager}
}

func NewWriteStdinTool(manager *ExecManager) *WriteStdinTool {
	return &WriteStdinTool{manager: manager}
}

func (t *ExecCommandTool) Name() string { return "exec_command" }

func (t *ExecCommandTool) Description() string {
	return "Runs a command in a PTY, returning output or a session ID for ongoing interaction."
}

func (t *ExecCommandTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cmd": map[string]any{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory to run the command in; defaults to the turn cwd.",
			},
			"shell": map[string]any{
				"type":        "string",
				"description": "Shell binary to launch. Defaults to the user's default shell.",
			},
			"login": map[string]any{
				"type":        "boolean",
				"description": "Whether to run the shell with -l/-i semantics. Defaults to true.",
			},
			"tty": map[string]any{
				"type":        "boolean",
				"description": "Whether to allocate a TTY for the command. Defaults to false (plain pipes); set to true to open a PTY and access TTY process.",
			},
			"yield_time_ms": map[string]any{
				"type":        "number",
				"description": "How long to wait (in milliseconds) for output before yielding.",
			},
			"max_output_tokens": map[string]any{
				"type":        "number",
				"description": "Maximum number of tokens to return. Excess output will be truncated.",
			},
		},
		"required":             []string{"cmd"},
		"additionalProperties": false,
	}
}

func (t *ExecCommandTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.fs == nil || t.manager == nil {
		return "", errors.New("exec_command not configured")
	}
	var a execCommandArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	cmdText := strings.TrimSpace(a.Cmd)
	if cmdText == "" {
		return "", errors.New("cmd must not be empty")
	}

	workdir := strings.TrimSpace(a.Workdir)
	if workdir == "" {
		workdir = tc.BaseDir
	}
	if workdir == "" {
		return "", errors.New("workdir required")
	}
	resolvedWorkdir, allowed, err := t.fs.resolvePath(tc.BaseDir, workdir, false)
	if err != nil && !(errors.Is(err, errPathDenied) && t.fs.approver != nil) {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "exec_command", workdir, allowed, err)
		return "", err
	}
	reason := fmt.Sprintf("Command: %s", cmdText)
	if errors.Is(err, errPathDenied) && strings.TrimSpace(resolvedWorkdir) != "" {
		reason += "\nWorkdir outside allowed roots: " + resolvedWorkdir
	}
	if err := t.fs.requestApproval(ctx, tc, "exec_command", resolvedWorkdir, reason); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "exec_command", resolvedWorkdir, allowed, err)
		return "", err
	}
	info, err := os.Stat(resolvedWorkdir)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("workdir is not a directory")
		}
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "exec_command", resolvedWorkdir, true, err)
		return "", err
	}

	yieldMs := a.YieldTimeMs
	if yieldMs <= 0 {
		yieldMs = execDefaultYieldMs
	}

	useLogin := true
	if a.Login != nil {
		useLogin = *a.Login
	}

	command, err := buildShellCommand(ctx, cmdText, a.Shell, useLogin, resolvedWorkdir)
	if err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "exec_command", resolvedWorkdir, true, err)
		return "", err
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return "", err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", err
	}
	command.Dir = resolvedWorkdir
	command.Env = append(os.Environ(), "LANG=C")

	start := time.Now()
	if err := command.Start(); err != nil {
		t.fs.auditError(ctx, tc.SessionID, tc.UserID, "exec_command", resolvedWorkdir, true, err)
		return "", err
	}

	proc := newExecProcess(command, stdin, start)
	proc.startReaders(stdout, stderr)
	procID := t.manager.add(proc)
	go proc.wait()

	output, truncated, originalBytes, done := proc.collectOutput(waitDuration(yieldMs), maxOutputBytes(a.MaxOutputTokens))
	if done && proc.isDrained() {
		t.manager.remove(procID)
	}

	t.fs.auditOK(ctx, tc.SessionID, tc.UserID, "exec_command", resolvedWorkdir, nil)
	resp := execResponse{
		WallTime:      time.Since(start),
		ExitCode:      proc.exitCode(),
		ProcessID:     nil,
		Output:        output,
		OriginalBytes: originalBytes,
		Truncated:     truncated,
	}
	if !done {
		resp.ProcessID = &procID
	}
	return formatExecResponse(resp), nil
}

func (t *WriteStdinTool) Name() string { return "write_stdin" }

func (t *WriteStdinTool) Description() string {
	return "Writes characters to an existing unified exec session and returns recent output."
}

func (t *WriteStdinTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_id": map[string]any{
				"type":        "number",
				"description": "Identifier of the running unified exec session.",
			},
			"chars": map[string]any{
				"type":        "string",
				"description": "Bytes to write to stdin (may be empty to poll).",
			},
			"yield_time_ms": map[string]any{
				"type":        "number",
				"description": "How long to wait (in milliseconds) for output before yielding.",
			},
			"max_output_tokens": map[string]any{
				"type":        "number",
				"description": "Maximum number of tokens to return. Excess output will be truncated.",
			},
		},
		"required":             []string{"session_id"},
		"additionalProperties": false,
	}
}

func (t *WriteStdinTool) Execute(ctx context.Context, _ ToolContext, args json.RawMessage) (string, error) {
	if t == nil || t.manager == nil {
		return "", errors.New("write_stdin not configured")
	}
	var a writeStdinArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if a.SessionID == 0 {
		return "", errors.New("session_id required")
	}
	proc, ok := t.manager.get(int64(a.SessionID))
	if !ok {
		return "", errors.New("unknown session_id")
	}
	if a.Chars != "" {
		if err := proc.writeInput(a.Chars); err != nil {
			return "", err
		}
	}

	yieldMs := a.YieldTimeMs
	if yieldMs <= 0 {
		yieldMs = writeDefaultYieldMs
	}
	output, truncated, originalBytes, done := proc.collectOutput(waitDuration(yieldMs), maxOutputBytes(a.MaxOutputTokens))
	if done && proc.isDrained() {
		t.manager.remove(int64(a.SessionID))
	}

	resp := execResponse{
		WallTime:      time.Since(proc.start),
		ExitCode:      proc.exitCode(),
		ProcessID:     nil,
		Output:        output,
		OriginalBytes: originalBytes,
		Truncated:     truncated,
	}
	if !done {
		id := int64(a.SessionID)
		resp.ProcessID = &id
	}
	return formatExecResponse(resp), nil
}

func buildShellCommand(ctx context.Context, cmdText, shellPath string, login bool, workdir string) (*exec.Cmd, error) {
	shell := strings.TrimSpace(shellPath)
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "/bin/bash"
		} else {
			shell = "/bin/sh"
		}
	}

	flag := "-c"
	if login {
		flag = "-lc"
	}
	command := exec.CommandContext(ctx, shell, flag, cmdText)
	command.Dir = workdir
	return command, nil
}

func waitDuration(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func maxOutputBytes(maxTokens *int) int {
	if maxTokens == nil || *maxTokens <= 0 {
		return 0
	}
	const approxBytesPerToken = 4
	return *maxTokens * approxBytesPerToken
}

type execResponse struct {
	WallTime      time.Duration
	ExitCode      *int
	ProcessID     *int64
	Output        string
	OriginalBytes int
	Truncated     bool
}

func formatExecResponse(resp execResponse) string {
	var sections []string
	sections = append(sections, fmt.Sprintf("Wall time: %.4f seconds", resp.WallTime.Seconds()))
	if resp.ExitCode != nil {
		sections = append(sections, fmt.Sprintf("Process exited with code %d", *resp.ExitCode))
	}
	if resp.ProcessID != nil {
		sections = append(sections, fmt.Sprintf("Process running with session ID %d", *resp.ProcessID))
	}
	if resp.Truncated && resp.OriginalBytes > 0 {
		approxTokens := (resp.OriginalBytes + 3) / 4
		sections = append(sections, fmt.Sprintf("Original token count: %d", approxTokens))
	}
	sections = append(sections, "Output:")
	sections = append(sections, resp.Output)
	return strings.Join(sections, "\n")
}

type ExecManager struct {
	mu     sync.Mutex
	nextID int64
	procs  map[int64]*execProcess
}

func NewExecManager() *ExecManager {
	return &ExecManager{
		nextID: 1,
		procs:  make(map[int64]*execProcess),
	}
}

func (m *ExecManager) add(p *execProcess) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	m.nextID++
	p.id = id
	m.procs[id] = p
	return id
}

func (m *ExecManager) get(id int64) (*execProcess, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[id]
	return p, ok
}

func (m *ExecManager) remove(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.procs, id)
}

type execProcess struct {
	id      int64
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	start   time.Time
	done    chan struct{}
	exit    *int
	exitErr error
	bufMu   sync.Mutex
	buf     []byte
	readPos int
}

func newExecProcess(cmd *exec.Cmd, stdin io.WriteCloser, start time.Time) *execProcess {
	return &execProcess{
		cmd:   cmd,
		stdin: stdin,
		start: start,
		done:  make(chan struct{}),
	}
}

func (p *execProcess) startReaders(stdout, stderr io.ReadCloser) {
	go p.readPipe(stdout)
	go p.readPipe(stderr)
}

func (p *execProcess) readPipe(r io.Reader) {
	defer func() {
		if closer, ok := r.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p.appendOutput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *execProcess) wait() {
	err := p.cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	p.bufMu.Lock()
	p.exit = &exitCode
	p.exitErr = err
	p.bufMu.Unlock()
	close(p.done)
}

func (p *execProcess) writeInput(chars string) error {
	if p.stdin == nil {
		return errors.New("stdin unavailable")
	}
	_, err := io.WriteString(p.stdin, chars)
	return err
}

func (p *execProcess) collectOutput(wait time.Duration, maxBytes int) (string, bool, int, bool) {
	if wait > 0 {
		select {
		case <-p.done:
		case <-time.After(wait):
		}
	} else {
		select {
		case <-p.done:
		default:
		}
	}

	out, truncated, original := p.consumeOutput(maxBytes)
	done := p.isDone()
	return out, truncated, original, done
}

func (p *execProcess) consumeOutput(maxBytes int) (string, bool, int) {
	p.bufMu.Lock()
	defer p.bufMu.Unlock()
	available := len(p.buf) - p.readPos
	if available <= 0 {
		return "", false, 0
	}
	original := available
	if maxBytes > 0 && available > maxBytes {
		available = maxBytes
	}
	out := string(p.buf[p.readPos : p.readPos+available])
	p.readPos += available
	truncated := maxBytes > 0 && available < original
	return out, truncated, original
}

func (p *execProcess) appendOutput(chunk []byte) {
	p.bufMu.Lock()
	defer p.bufMu.Unlock()
	if len(chunk) == 0 {
		return
	}
	if len(p.buf)+len(chunk) > execMaxBufferBytes {
		excess := len(p.buf) + len(chunk) - execMaxBufferBytes
		if excess >= len(p.buf) {
			p.buf = nil
			p.readPos = 0
		} else {
			p.buf = append([]byte(nil), p.buf[excess:]...)
			if p.readPos >= excess {
				p.readPos -= excess
			} else {
				p.readPos = 0
			}
		}
	}
	p.buf = append(p.buf, chunk...)
}

func (p *execProcess) isDone() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *execProcess) isDrained() bool {
	p.bufMu.Lock()
	defer p.bufMu.Unlock()
	return p.readPos >= len(p.buf)
}

func (p *execProcess) exitCode() *int {
	p.bufMu.Lock()
	defer p.bufMu.Unlock()
	if p.exit == nil {
		return nil
	}
	val := *p.exit
	return &val
}
