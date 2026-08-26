package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/tools"
)

type fakeAttachmentToolStore struct {
	attachment AttachmentRecord
}

func (s fakeAttachmentToolStore) ListSessionAttachmentsForAgent(context.Context, int64, int64, int, int) ([]AttachmentRecord, error) {
	return []AttachmentRecord{s.attachment}, nil
}

func (s fakeAttachmentToolStore) GetSessionAttachmentForAgent(_ context.Context, _, sessionID int64, id string) (AttachmentRecord, error) {
	if id != s.attachment.ID || sessionID != s.attachment.SessionID || s.attachment.MessageID == nil {
		return AttachmentRecord{}, errors.New("not found")
	}
	return s.attachment, nil
}

type fakeAttachmentObjectReader struct{ content string }

func (r fakeAttachmentObjectReader) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}

type fakeAttachmentSandboxWriter struct {
	input sandbox.FileWriteRequest
	body  string
}

func (w *fakeAttachmentSandboxWriter) WriteFile(_ context.Context, _ int64, input sandbox.FileWriteRequest, reader io.Reader) (sandbox.FileWriteResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return sandbox.FileWriteResult{}, err
	}
	w.input, w.body = input, string(data)
	return sandbox.FileWriteResult{Path: "/workspace/import/data.zip", SizeBytes: int64(len(data)), SHA256: input.SHA256}, nil
}

func TestAttachmentToolsListAndDownloadOpaqueReference(t *testing.T) {
	messageID := int64(9)
	attachment := AttachmentRecord{
		ID: "abc123", SessionID: 7, MessageID: &messageID, ObjectKey: "attachments/7/object",
		OriginalName: "food data.zip", MIMEType: "application/zip", SizeBytes: 12, SHA256: strings.Repeat("a", 64),
	}
	store := fakeAttachmentToolStore{attachment: attachment}
	list := &listAttachmentsTool{agentID: 3, store: store}
	listed, err := list.Execute(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{}`))
	if err != nil || !strings.Contains(listed, "attachment://abc123/food%20data.zip") || strings.Contains(listed, attachment.ObjectKey) {
		t.Fatalf("unsafe or missing list output: %q, %v", listed, err)
	}

	writer := &fakeAttachmentSandboxWriter{}
	download := &downloadAttachmentTool{agentID: 3, store: store, objects: fakeAttachmentObjectReader{content: "archive-data"}, sandbox: writer}
	output, err := download.Execute(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{"attachment":"attachment://abc123/food%20data.zip","destination":"import/data.zip"}`))
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}
	if writer.body != "archive-data" || writer.input.Path != "import/data.zip" || writer.input.Overwrite || writer.input.SHA256 != attachment.SHA256 {
		t.Fatalf("unexpected sandbox write: input=%#v body=%q", writer.input, writer.body)
	}
	if !strings.Contains(output, "/workspace/import/data.zip") {
		t.Fatalf("unexpected tool output: %q", output)
	}
}

func TestDownloadAttachmentRequiresSandboxAndRejectsForeignSchemes(t *testing.T) {
	messageID := int64(9)
	store := fakeAttachmentToolStore{attachment: AttachmentRecord{ID: "abc123", SessionID: 7, MessageID: &messageID}}
	tool := &downloadAttachmentTool{agentID: 3, store: store, objects: fakeAttachmentObjectReader{}}
	if _, err := tool.Execute(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{"attachment":"https://example.test/file","destination":"file"}`)); err == nil {
		t.Fatal("foreign attachment scheme was accepted")
	}
	if _, err := tool.Execute(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{"attachment":"attachment://abc123/file","destination":"file"}`)); err == nil || !strings.Contains(err.Error(), "requires an agent with a sandbox") {
		t.Fatalf("expected clear sandbox error, got %v", err)
	}
}

type fakeAttachmentSandboxReader struct {
	content []byte
	err     error
}

func (r fakeAttachmentSandboxReader) ReadFile(context.Context, int64, sandbox.FileReadRequest) (sandbox.FileReadResult, error) {
	if r.err != nil {
		return sandbox.FileReadResult{}, r.err
	}
	return sandbox.FileReadResult{Body: io.NopCloser(bytes.NewReader(r.content)), SizeBytes: int64(len(r.content))}, nil
}

type fakeAttachmentObjectWriter struct {
	content  []byte
	mimeType string
	key      string
	deleted  bool
	putErr   error
}

func (w *fakeAttachmentObjectWriter) PutReader(_ context.Context, key, mimeType string, reader io.Reader, size int64) (int64, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	w.key, w.mimeType, w.content = key, mimeType, data
	if w.putErr != nil {
		return 0, w.putErr
	}
	if size != int64(len(data)) {
		return 0, errors.New("unexpected size")
	}
	return int64(len(data)), nil
}

func (w *fakeAttachmentObjectWriter) Delete(context.Context, string) error {
	w.deleted = true
	return nil
}

type fakeAttachmentPublishStore struct {
	attachment AttachmentRecord
	agentID    int64
	sessionID  int64
	name       string
	mimeType   string
	objectKey  string
	size       int64
	digest     string
	err        error
}

func (s *fakeAttachmentPublishStore) CreateAttachmentForAgent(_ context.Context, agentID, sessionID int64, name, mimeType, objectKey string, size int64, digest string) (AttachmentRecord, error) {
	s.agentID, s.sessionID, s.name, s.mimeType, s.objectKey, s.size, s.digest = agentID, sessionID, name, mimeType, objectKey, size, digest
	if s.err != nil {
		return AttachmentRecord{}, s.err
	}
	attachment := s.attachment
	attachment.SessionID, attachment.OriginalName, attachment.MIMEType = sessionID, name, mimeType
	attachment.ObjectKey, attachment.SizeBytes, attachment.SHA256 = objectKey, size, digest
	return attachment, nil
}

func TestPublishAttachmentStreamsSandboxFileAndReturnsArtifact(t *testing.T) {
	content := append([]byte("\x89PNG\r\n\x1a\n"), []byte("generated-image")...)
	store := &fakeAttachmentPublishStore{attachment: AttachmentRecord{ID: "published123"}}
	objects := &fakeAttachmentObjectWriter{}
	publish := &publishAttachmentTool{
		agentID: 3, store: store, objects: objects,
		sandbox: fakeAttachmentSandboxReader{content: content},
	}
	result, err := publish.ExecuteRich(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{"path":"/workspace/generated/output.png","name":"result.png"}`))
	if err != nil {
		t.Fatalf("publish attachment: %v", err)
	}
	digest := sha256.Sum256(content)
	if store.agentID != 3 || store.sessionID != 7 || store.name != "result.png" || store.mimeType != "image/png" || store.digest != fmt.Sprintf("%x", digest[:]) {
		t.Fatalf("unexpected stored attachment: %#v", store)
	}
	if !bytes.Equal(objects.content, content) || !strings.HasPrefix(objects.key, "published-artifacts/7/") {
		t.Fatalf("unexpected object: key=%q content=%q", objects.key, objects.content)
	}
	if len(result.ArtifactIDs) != 1 || result.ArtifactIDs[0] != "published123" || !strings.Contains(result.ModelText, "attachment://published123/result.png") {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}

func TestPublishAttachmentCleansObjectWhenPersistenceFails(t *testing.T) {
	store := &fakeAttachmentPublishStore{attachment: AttachmentRecord{ID: "published123"}, err: errors.New("database unavailable")}
	objects := &fakeAttachmentObjectWriter{}
	publish := &publishAttachmentTool{agentID: 3, store: store, objects: objects, sandbox: fakeAttachmentSandboxReader{content: []byte("archive")}}
	if _, err := publish.ExecuteRich(context.Background(), tools.ToolContext{SessionID: 7}, json.RawMessage(`{"path":"output.zip"}`)); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !objects.deleted {
		t.Fatal("published object was not cleaned up")
	}
}
