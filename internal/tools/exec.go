package tools

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

func runScript(ctx context.Context, scriptPath, workDir string, args []string, timeout time.Duration, maxOutputBytes int) (string, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(execCtx, scriptPath, args...)
	cmd.Dir = workDir
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"LANG=C",
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return truncate(buf.String(), maxOutputBytes), err
	}
	return truncate(buf.String(), maxOutputBytes), nil
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
