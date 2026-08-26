package web

import (
	"context"
	"encoding/json"
	"errors"
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
