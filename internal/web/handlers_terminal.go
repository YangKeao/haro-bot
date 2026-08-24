package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type terminalClientMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Columns uint16 `json:"columns,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
}

type terminalServerMessage struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	Message  string `json:"message,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

func (s *Server) handleSandboxTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	id, err := parseID(r, "sandboxID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	connection.SetReadLimit(64 << 10)
	defer connection.CloseNow()

	process, err := s.sandboxes.StartWebTerminal(r.Context(), id)
	if err != nil {
		writeTerminalMessage(r.Context(), connection, terminalServerMessage{Type: "error", Message: err.Error()})
		_ = connection.Close(websocket.StatusPolicyViolation, "terminal unavailable")
		return
	}
	defer s.stopWebTerminal(id, process.ID)

	events := make(chan terminalServerMessage, 16)
	readDone := make(chan error, 1)
	go s.readTerminalClient(r.Context(), connection, id, process.ID, events, readDone)

	nextOffset := process.OutputOffset
	if process.Output != "" {
		if err := writeTerminalMessage(r.Context(), connection, terminalServerMessage{Type: "output", Data: process.Output}); err != nil {
			return
		}
		nextOffset += int64(len(process.Output))
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-readDone:
			if err != nil && !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				return
			}
			return
		case event := <-events:
			if err := writeTerminalMessage(r.Context(), connection, event); err != nil {
				return
			}
		case <-ticker.C:
			current, err := s.sandboxes.GetWebTerminal(r.Context(), id, process.ID)
			if err != nil {
				_ = writeTerminalMessage(r.Context(), connection, terminalServerMessage{Type: "error", Message: err.Error()})
				return
			}
			chunk, offset := terminalOutputSince(current, nextOffset)
			if chunk != "" {
				if err := writeTerminalMessage(r.Context(), connection, terminalServerMessage{Type: "output", Data: chunk}); err != nil {
					return
				}
			}
			nextOffset = offset
			if current.Status != sandbox.RunStarting && current.Status != sandbox.RunRunning {
				_ = writeTerminalMessage(r.Context(), connection, terminalServerMessage{Type: "exit", ExitCode: current.ExitCode})
				_ = connection.Close(websocket.StatusNormalClosure, "terminal exited")
				return
			}
		}
	}
}

func (s *Server) readTerminalClient(ctx context.Context, connection *websocket.Conn, sandboxID int64, processID string, events chan<- terminalServerMessage, done chan<- error) {
	for {
		var message terminalClientMessage
		if err := wsjson.Read(ctx, connection, &message); err != nil {
			done <- err
			return
		}
		var err error
		switch message.Type {
		case "input":
			_, err = s.sandboxes.WriteWebTerminal(ctx, sandboxID, processID, message.Data)
		case "resize":
			err = s.sandboxes.ResizeWebTerminal(ctx, sandboxID, processID, sandbox.ResizeRequest{Columns: message.Columns, Rows: message.Rows})
		default:
			events <- terminalServerMessage{Type: "error", Message: "unknown terminal message type"}
			continue
		}
		if err != nil {
			events <- terminalServerMessage{Type: "error", Message: err.Error()}
		}
	}
}

func (s *Server) stopWebTerminal(sandboxID int64, processID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	process, err := s.sandboxes.StopWebTerminal(ctx, sandboxID, processID, "TERM")
	if err != nil || (process.Status != sandbox.RunStarting && process.Status != sandbox.RunRunning) {
		return
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	process, err = s.sandboxes.GetWebTerminal(ctx, sandboxID, processID)
	if err == nil && (process.Status == sandbox.RunStarting || process.Status == sandbox.RunRunning) {
		_, _ = s.sandboxes.StopWebTerminal(ctx, sandboxID, processID, "KILL")
	}
}

func writeTerminalMessage(ctx context.Context, connection *websocket.Conn, message terminalServerMessage) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(writeCtx, connection, message)
}

func terminalOutputSince(process sandbox.Process, offset int64) (string, int64) {
	start := offset - process.OutputOffset
	if start < 0 {
		start = 0
	}
	if start > int64(len(process.Output)) {
		start = int64(len(process.Output))
	}
	return process.Output[start:], process.OutputOffset + int64(len(process.Output))
}
