package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/memory"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	sessions, err := s.store.ListSessions(r.Context(), s.userID, agentID, r.URL.Query().Get("archived") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleListRecentSessions(w http.ResponseWriter, r *http.Request) {
	limit := 6
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be an integer")
			return
		}
		limit = parsed
	}
	sessions, err := s.store.ListRecentSessions(r.Context(), s.userID, limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	agentID, err := parseID(r, "agentID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var input struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &input) {
		return
	}
	session, err := s.store.CreateSession(r.Context(), s.userID, agentID, strings.TrimSpace(input.Title))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	session, err := s.store.GetSession(r.Context(), s.userID, sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	runtime, _, err := s.runtimes.Get(r.Context(), session.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session, "status": runtime.GetSessionStatus(sessionID)})
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var input struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len([]rune(input.Title)) > 255 {
		writeError(w, http.StatusBadRequest, "invalid_title", "title is required and must be at most 255 characters")
		return
	}
	if err := s.store.RenameSession(r.Context(), s.userID, sessionID, input.Title); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": input.Title})
}

func (s *Server) handleArchiveSession(w http.ResponseWriter, r *http.Request) {
	s.setSessionArchived(w, r, true)
}

func (s *Server) handleRestoreSession(w http.ResponseWriter, r *http.Request) {
	s.setSessionArchived(w, r, false)
}

func (s *Server) setSessionArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	session, err := s.store.GetSession(r.Context(), s.userID, sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	runtime, _, err := s.runtimes.Get(r.Context(), session.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if status := runtime.GetSessionStatus(sessionID); status.State != agent.StateIdle && status.State != "" {
		writeError(w, http.StatusConflict, "session_busy", "stop the active run before archiving this session")
		return
	}
	if err := s.store.SetSessionArchived(r.Context(), s.userID, sessionID, archived); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"archived": archived})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	messages, err := s.store.ListMessages(r.Context(), s.userID, sessionID, before, limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var next any
	if len(messages) == limit {
		next = messages[0].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": messages, "next_cursor": next})
}

type runInput struct {
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var input runInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" && len(input.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "empty_message", "message text or an image is required")
		return
	}
	if len(input.AttachmentIDs) > 4 {
		writeError(w, http.StatusBadRequest, "too_many_attachments", "a message can contain at most 4 images")
		return
	}
	session, err := s.store.GetSession(r.Context(), s.userID, sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if session.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "session_archived", "restore the session before sending a message")
		return
	}
	for _, id := range input.AttachmentIDs {
		attachment, err := s.store.GetAttachment(r.Context(), s.userID, id)
		if err != nil || attachment.SessionID != sessionID || attachment.MessageID != nil {
			writeError(w, http.StatusBadRequest, "invalid_attachment", "one or more attachments are invalid or already used")
			return
		}
	}
	runtime, profile, err := s.runtimes.Get(r.Context(), session.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if profile.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "agent_archived", "restore the agent before sending a message")
		return
	}
	if status := runtime.GetSessionStatus(sessionID); status.State != agent.StateIdle && status.State != "" {
		writeError(w, http.StatusConflict, "session_busy", "this session already has an active run")
		return
	}
	runCtx, releaseRun, ok := s.beginRun(r.Context(), sessionID)
	if !ok {
		writeError(w, http.StatusConflict, "session_busy", "this session already has an active run")
		return
	}
	defer releaseRun()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	runID, _ := randomID("run_", 12)
	writeSSE(w, flusher, "run.started", map[string]any{"run_id": runID, "session_id": sessionID})
	writeSSE(w, flusher, "message.created", map[string]any{"role": "user", "content": input.Content, "attachment_ids": input.AttachmentIDs})

	progress := newWebProgress()
	type runResult struct {
		output string
		err    error
	}
	done := make(chan runResult, 1)
	go func() {
		metadata := &memory.MessageMetadata{AttachmentIDs: input.AttachmentIDs}
		output, err := runtime.HandleSessionWithMiddleware(runCtx, s.userID, sessionID, session.Channel, input.Content, metadata, agent.MiddlewareSet{
			LLMDeltaListeners: []agent.LLMDeltaListener{progress}, ToolCallListeners: []agent.ToolCallListener{progress},
			ToolResultListeners: []agent.ToolResultListener{progress}, OutputListeners: []agent.OutputListener{progress},
		})
		done <- runResult{output: output, err: err}
		close(progress.events)
	}()

	for event := range progress.events {
		if !writeSSE(w, flusher, event.Name, event.Data) {
			return
		}
	}
	result := <-done
	_ = s.store.AutoTitleSession(context.WithoutCancel(r.Context()), s.userID, sessionID, input.Content)
	_ = s.store.TouchSession(context.WithoutCancel(r.Context()), sessionID)
	if result.err == nil {
		return
	}
	content, reasoning := progress.partial()
	status := "error"
	event := "run.failed"
	if errors.Is(result.err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
		status = "cancelled"
		event = "run.cancelled"
	}
	if content != "" || reasoning != "" {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		_, _ = s.conversation.AddMessageAndGetID(persistCtx, sessionID, "assistant", content, &memory.MessageMetadata{ReasoningContent: reasoning, Status: status})
		cancel()
	}
	_ = writeSSE(w, flusher, event, map[string]any{"message": result.err.Error()})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	session, err := s.store.GetSession(r.Context(), s.userID, sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	runtime, _, err := s.runtimes.Get(r.Context(), session.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cancelled := s.cancelRun(sessionID)
	if runtime.CancelSession(sessionID) {
		cancelled = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, name string, data any) bool {
	payload, err := json.Marshal(data)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	sessionID, err := parseID(r, "sessionID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if _, err := s.store.GetSession(r.Context(), s.userID, sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(1<<20))
	if err := r.ParseMultipartForm(maxImageBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "image is too large or malformed")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "multipart field 'file' is required")
		return
	}
	defer file.Close()
	data, mimeType, err := readImage(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[mimeType]
	keyID, _ := randomID("", 16)
	objectKey := fmt.Sprintf("images/%d/%s%s", sessionID, keyID, ext)
	if err := s.objects.Put(r.Context(), objectKey, mimeType, data); err != nil {
		writeError(w, http.StatusBadGateway, "object_store_error", "could not store image")
		return
	}
	name := filepath.Base(header.Filename)
	if len([]rune(name)) > 255 {
		name = string([]rune(name)[:255])
	}
	attachment, err := s.store.CreateAttachment(r.Context(), s.userID, sessionID, name, mimeType, objectKey, int64(len(data)))
	if err != nil {
		_ = s.objects.Delete(r.Context(), objectKey)
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func readImage(file multipart.File) ([]byte, string, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || int64(len(data)) > maxImageBytes {
		return nil, "", errors.New("image must be between 1 byte and 10 MiB")
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
		return nil, "", errors.New("only JPEG, PNG, and WebP images are supported")
	}
	return data, mimeType, nil
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, err := s.store.GetAttachment(r.Context(), s.userID, r.PathValue("attachmentID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	reader, err := s.objects.Open(r.Context(), attachment.ObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "object_store_error", "could not read image")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", attachment.MIMEType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, reader)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, err := s.store.GetAttachment(r.Context(), s.userID, r.PathValue("attachmentID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if attachment.MessageID != nil {
		writeError(w, http.StatusConflict, "attachment_in_use", "sent attachments cannot be deleted")
		return
	}
	if err := s.objects.Delete(r.Context(), attachment.ObjectKey); err != nil {
		writeError(w, http.StatusBadGateway, "object_store_error", "could not delete image")
		return
	}
	if _, err := s.store.DeletePendingAttachment(r.Context(), s.userID, attachment.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
