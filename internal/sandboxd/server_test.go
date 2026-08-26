package sandboxd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skillbundle"
	"golang.org/x/sys/unix"
)

func TestServerWritesAttachmentAtomicallyInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	runtime, err := New(workspace, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	content := []byte("zip-like attachment bytes")
	digest := sha256.Sum256(content)
	var result sandbox.FileWriteResult
	requestFile(t, server.URL, "test-token", "uploads/data.zip", false, fmt.Sprintf("%x", digest[:]), content, http.StatusCreated, &result)
	if result.Path != filepath.Join(workspace, "uploads", "data.zip") || result.SizeBytes != int64(len(content)) || result.SHA256 != fmt.Sprintf("%x", digest[:]) {
		t.Fatalf("unexpected write result: %#v", result)
	}
	stored, err := os.ReadFile(result.Path)
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored file mismatch: %q, %v", stored, err)
	}

	requestFile(t, server.URL, "test-token", "uploads/data.zip", false, "", []byte("replacement"), http.StatusBadRequest, nil)
	requestFile(t, server.URL, "test-token", "uploads/data.zip", true, "", []byte("replacement"), http.StatusCreated, &result)
	stored, _ = os.ReadFile(result.Path)
	if string(stored) != "replacement" {
		t.Fatalf("overwrite did not replace content: %q", stored)
	}
}

func TestServerRejectsAttachmentTraversalSymlinksAndHashMismatch(t *testing.T) {
	workspace := t.TempDir()
	runtime, err := New(workspace, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	requestFile(t, server.URL, "test-token", "../outside.zip", false, "", []byte("escape"), http.StatusBadRequest, nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	requestFile(t, server.URL, "test-token", "linked/escape.zip", false, "", []byte("escape"), http.StatusBadRequest, nil)
	if _, err := os.Stat(filepath.Join(outside, "escape.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink escape wrote outside workspace: %v", err)
	}
	requestFile(t, server.URL, "test-token", "bad-hash.zip", false, strings.Repeat("0", 64), []byte("content"), http.StatusBadRequest, nil)
	if _, err := os.Stat(filepath.Join(workspace, "bad-hash.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hash mismatch left a destination file: %v", err)
	}
}

func TestServerStreamsRegularWorkspaceFiles(t *testing.T) {
	workspace := t.TempDir()
	runtime, err := New(workspace, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	if err := os.MkdirAll(filepath.Join(workspace, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("generated image bytes")
	if err := os.WriteFile(filepath.Join(workspace, "generated", "output.png"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	response := requestReadFile(t, server.URL, "test-token", "generated/output.png", http.StatusOK)
	defer response.Body.Close()
	if response.ContentLength != int64(len(content)) {
		t.Fatalf("content length = %d, want %d", response.ContentLength, len(content))
	}
	data, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("streamed content mismatch: %q, %v", data, err)
	}
}

func TestServerRejectsUnsafeWorkspaceFileReads(t *testing.T) {
	workspace := t.TempDir()
	runtime, err := New(workspace, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(workspace, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	requestReadFile(t, server.URL, "test-token", "../outside.txt", http.StatusBadRequest).Body.Close()
	requestReadFile(t, server.URL, "test-token", "linked.txt", http.StatusBadRequest).Body.Close()
	requestReadFile(t, server.URL, "test-token", "directory", http.StatusBadRequest).Body.Close()
	requestReadFile(t, server.URL, "test-token", "pipe", http.StatusBadRequest).Body.Close()
	requestReadFile(t, server.URL, "wrong-token", "linked.txt", http.StatusUnauthorized).Body.Close()
}

func TestServerMaterializesSkillIdempotentlyAndExecutesScript(t *testing.T) {
	workspace := t.TempDir()
	runtime, err := New(workspace, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "scripts", "hello.sh"), []byte("#!/bin/sh\nprintf skill-ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, manifest, err := skillbundle.Archive(source)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := server.URL + "/v1/skills/" + manifest.Hash
	requestSkill(t, endpoint, "wrong", archive, http.StatusUnauthorized, nil)
	var materialized sandbox.SkillMaterialization
	requestSkill(t, endpoint, "test-token", archive, http.StatusCreated, &materialized)
	if materialized.Reused || materialized.SkillRoot != filepath.Join(workspace, ".haro", "skills", "sha256", manifest.Hash) {
		t.Fatalf("unexpected materialization: %#v", materialized)
	}
	requestSkill(t, endpoint, "test-token", archive, http.StatusOK, &materialized)
	if !materialized.Reused {
		t.Fatal("second materialization did not reuse the verified bundle")
	}
	requestSkill(t, server.URL+"/v1/skills/"+strings.Repeat("f", 64), "test-token", archive, http.StatusBadRequest, nil)

	input := sandbox.ExecRequest{ID: "skill-run", AgentID: 1, SessionID: 1, Command: fmt.Sprintf("%q", filepath.Join(materialized.SkillRoot, "scripts", "hello.sh")), YieldTimeMS: 2000}
	var process sandbox.Process
	requestJSON(t, server.URL+"/v1/processes", "test-token", input, http.StatusCreated, &process)
	if process.Status != sandbox.RunExited || process.ExitCode == nil || *process.ExitCode != 0 || process.Output != "skill-ran" {
		t.Fatalf("materialized script did not execute: %#v", process)
	}
}

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

func requestSkill(t *testing.T, endpoint, token string, archive []byte, wantStatus int, output any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/gzip")
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

func requestFile(t *testing.T, baseURL, token, destination string, overwrite bool, digest string, content []byte, wantStatus int, output any) {
	t.Helper()
	query := url.Values{"path": []string{destination}, "overwrite": []string{fmt.Sprintf("%t", overwrite)}}
	if digest != "" {
		query.Set("sha256", digest)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/v1/files?"+query.Encode(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

func requestReadFile(t *testing.T, baseURL, token, source string, wantStatus int) *http.Response {
	t.Helper()
	query := url.Values{"path": []string{source}}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/files?"+query.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d: %s", response.StatusCode, wantStatus, data)
	}
	return response
}
