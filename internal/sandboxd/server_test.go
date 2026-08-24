package sandboxd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
)

func TestServerExecutesInWorkspaceWithAgentEnvironment(t *testing.T) {
	runtime, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	input := sandbox.ExecRequest{ID: "run-1", AgentID: 3, SessionID: 4, Command: `printf '%s:%s' "$MYSQL_HOST" "$PWD"`, Environment: map[string]string{"MYSQL_HOST": "database.internal"}, YieldTimeMS: 2000}
	var process sandbox.Process
	requestJSON(t, server.URL+"/v1/processes", "test-token", input, http.StatusCreated, &process)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(process.Output, "database.internal:") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		requestJSON(t, server.URL+"/v1/processes/run-1", "test-token", nil, http.StatusOK, &process)
	}
	if process.Status != sandbox.RunExited || process.ExitCode == nil || *process.ExitCode != 0 {
		t.Fatalf("unexpected process: %#v", process)
	}
	if process.TTY == nil || *process.TTY {
		t.Fatalf("non-TTY mode was not preserved: %#v", process.TTY)
	}
	if !strings.Contains(process.Output, "database.internal:") || !strings.Contains(process.Output, runtime.workspace) {
		t.Fatalf("environment or workspace missing from %q", process.Output)
	}
}

func TestServerRequiresBearerTokenAndConfinesWorkdir(t *testing.T) {
	runtime, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)
	requestJSON(t, server.URL+"/v1/processes", "wrong", sandbox.ExecRequest{ID: "x", AgentID: 1, SessionID: 1, Command: "true"}, http.StatusUnauthorized, nil)
	requestJSON(t, server.URL+"/v1/processes", "test-token", sandbox.ExecRequest{ID: "x", AgentID: 1, SessionID: 1, Command: "true", Workdir: "/tmp"}, http.StatusBadRequest, nil)
}

func TestServerDrainsTTYOutputBeforeExit(t *testing.T) {
	runtime, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	input := sandbox.ExecRequest{ID: "tty-run", AgentID: 1, SessionID: 1, Command: "printf tty-output", TTY: true, YieldTimeMS: 2000}
	var process sandbox.Process
	requestJSON(t, server.URL+"/v1/processes", "test-token", input, http.StatusCreated, &process)
	if process.Status != sandbox.RunExited || process.ExitCode == nil || *process.ExitCode != 0 {
		t.Fatalf("unexpected TTY process: %#v", process)
	}
	if process.TTY == nil || !*process.TTY {
		t.Fatalf("TTY mode was not preserved: %#v", process.TTY)
	}
	if !strings.Contains(process.Output, "tty-output") {
		t.Fatalf("TTY output was not drained: %q", process.Output)
	}
}

func TestServerReturnsOnlyUnreadOutputForEachInteraction(t *testing.T) {
	runtime, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	input := sandbox.ExecRequest{ID: "incremental", AgentID: 1, SessionID: 1, Command: "printf first; sleep 0.5; printf second", YieldTimeMS: 250}
	var first sandbox.Process
	requestJSON(t, server.URL+"/v1/processes", "test-token", input, http.StatusCreated, &first)
	if !first.InteractionOutputAvailable || first.InteractionOutput != "first" || first.Status != sandbox.RunRunning {
		t.Fatalf("unexpected initial interaction: %#v", first)
	}

	var second sandbox.Process
	requestJSON(t, server.URL+"/v1/processes/incremental/stdin", "test-token", sandbox.StdinRequest{YieldTimeMS: 1}, http.StatusOK, &second)
	if second.Status != sandbox.RunExited || second.InteractionOutput != "second" {
		t.Fatalf("unexpected second interaction: %#v", second)
	}
	if second.Output != first.InteractionOutput+second.InteractionOutput {
		t.Fatalf("cumulative output %q does not match interactions", second.Output)
	}

	var final sandbox.Process
	requestJSON(t, server.URL+"/v1/processes/incremental/stdin", "test-token", sandbox.StdinRequest{}, http.StatusOK, &final)
	if !final.InteractionOutputAvailable || final.InteractionOutput != "" {
		t.Fatalf("terminal poll repeated output: %#v", final)
	}
}

func TestWebTerminalSupportsTTYInputAndResizeWithoutAgentSession(t *testing.T) {
	runtime, err := New(t.TempDir(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	input := sandbox.ExecRequest{ID: "web-terminal", Kind: "web_terminal", Command: `IFS= read -r value; stty size; printf 'received:%s' "$value"; sleep 2`, TTY: true, Background: true}
	var process sandbox.Process
	requestJSON(t, server.URL+"/v1/processes", "test-token", input, http.StatusCreated, &process)
	if process.Status != sandbox.RunRunning || process.Kind != "web_terminal" {
		t.Fatalf("unexpected terminal process: %#v", process)
	}
	requestJSON(t, server.URL+"/v1/processes/web-terminal/resize", "test-token", sandbox.ResizeRequest{Columns: 91, Rows: 37}, http.StatusNoContent, nil)
	started := time.Now()
	requestJSON(t, server.URL+"/v1/processes/web-terminal/stdin", "test-token", sandbox.StdinRequest{Chars: "hello\n", YieldTimeMS: 2000}, http.StatusOK, &process)
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("Web Terminal input waited for process output: %s", elapsed)
	}
	deadline := time.Now().Add(time.Second)
	for (!strings.Contains(process.Output, "37 91") || !strings.Contains(process.Output, "received:hello")) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		requestJSON(t, server.URL+"/v1/processes/web-terminal", "test-token", nil, http.StatusOK, &process)
	}
	if !strings.Contains(process.Output, "37 91") || !strings.Contains(process.Output, "received:hello") {
		t.Fatalf("TTY resize or input was not observed: %q", process.Output)
	}
	requestJSON(t, server.URL+"/v1/processes/web-terminal/signal", "test-token", sandbox.SignalRequest{Signal: "TERM"}, http.StatusOK, &process)

	requestJSON(t, server.URL+"/v1/processes", "test-token", sandbox.ExecRequest{ID: "agent-process", Command: "true"}, http.StatusBadRequest, nil)
}

func requestJSON(t *testing.T, endpoint, token string, input any, wantStatus int, output any) {
	t.Helper()
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if input == nil {
		req, err = http.NewRequest(http.MethodGet, endpoint, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d", response.StatusCode, wantStatus)
	}
	if output != nil {
		if err := json.NewDecoder(response.Body).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
}
