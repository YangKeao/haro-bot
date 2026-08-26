package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
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
	if archived && s.mcp != nil {
		s.mcp.CloseChat(r.Context(), session.AgentID, sessionID)
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
	if !decodeRunInput(w, r, &input) {
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" && len(input.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "empty_message", "message text or an attachment is required")
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

	progress := newWebProgress(func(ctx context.Context, id string) (AttachmentRecord, error) {
		return s.store.GetAttachment(ctx, s.userID, id)
	})
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
	content, reasoning, trace := progress.partial()
	status := "error"
	event := "run.failed"
	if errors.Is(result.err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
		status = "cancelled"
		event = "run.cancelled"
	}
	if content != "" || reasoning != "" || len(trace) > 0 {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		_, _ = s.conversation.AddMessageAndGetID(persistCtx, sessionID, "assistant", content, &memory.MessageMetadata{ReasoningContent: reasoning, TraceSteps: trace, Status: status})
		cancel()
	}
	_ = writeSSE(w, flusher, event, map[string]any{"message": result.err.Error()})
}

func decodeRunInput(w http.ResponseWriter, r *http.Request, input *runInput) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return false
	}
	return true
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
	multipartReader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "a multipart file upload is required")
		return
	}
	part, err := nextUploadFile(multipartReader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "multipart field 'file' is required")
		return
	}
	defer part.Close()
	sniff := make([]byte, 512)
	n, sniffErr := io.ReadFull(part, sniff)
	if sniffErr != nil && !errors.Is(sniffErr, io.EOF) && !errors.Is(sniffErr, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusBadRequest, "invalid_upload", "could not read uploaded file")
		return
	}
	sniff = sniff[:n]
	mimeType := http.DetectContentType(sniff)
	keyID, _ := randomID("", 16)
	objectKey := fmt.Sprintf("attachments/%d/%s", sessionID, keyID)
	hash := sha256.New()
	stream := io.TeeReader(io.MultiReader(bytes.NewReader(sniff), part), hash)
	size, err := s.objects.PutReader(r.Context(), objectKey, mimeType, stream, -1)
	if err != nil {
		cleanupObject(s.objects, r.Context(), objectKey)
		writeError(w, http.StatusBadGateway, "object_store_error", "could not store attachment")
		return
	}
	name := sanitizeAttachmentName(part.FileName())
	attachment, err := s.store.CreateAttachment(r.Context(), s.userID, sessionID, name, mimeType, objectKey, size, fmt.Sprintf("%x", hash.Sum(nil)))
	if err != nil {
		cleanupObject(s.objects, r.Context(), objectKey)
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func cleanupObject(objects *ObjectStore, parent context.Context, objectKey string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	_ = objects.Delete(ctx, objectKey)
}

func nextUploadFile(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part, nil
		}
		_ = part.Close()
	}
}

func sanitizeAttachmentName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		name = "attachment"
	}
	runes := []rune(name)
	if len(runes) > 255 {
		name = string(runes[:255])
	}
	return name
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, err := s.store.GetAttachment(r.Context(), s.userID, r.PathValue("attachmentID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	reader, err := s.objects.Open(r.Context(), attachment.ObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "object_store_error", "could not read attachment")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", attachment.MIMEType)
	disposition := "attachment"
	if r.URL.Query().Get("download") != "1" && isPreviewImage(attachment.MIMEType) {
		disposition = "inline"
	}
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": attachment.OriginalName}); value != "" {
		w.Header().Set("Content-Disposition", value)
	}
	w.Header().Set("Content-Length", strconv.FormatInt(attachment.SizeBytes, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = io.Copy(w, reader)
}

func isPreviewImage(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
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
		writeError(w, http.StatusBadGateway, "object_store_error", "could not delete attachment")
		return
	}
	if _, err := s.store.DeletePendingAttachment(r.Context(), s.userID, attachment.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
