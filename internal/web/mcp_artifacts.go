package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

type MCPArtifactSink struct {
	store   *Store
	objects *ObjectStore
	userID  int64
}

func NewMCPArtifactSink(store *Store, objects *ObjectStore, userID int64) *MCPArtifactSink {
	return &MCPArtifactSink{store: store, objects: objects, userID: userID}
}

func (s *MCPArtifactSink) SaveMCPArtifact(ctx context.Context, sessionID int64, name, mimeType string, data []byte) (string, error) {
	if s == nil || s.store == nil || s.objects == nil {
		return "", errors.New("artifact storage is unavailable")
	}
	if len(data) == 0 || len(data) > 4<<20 {
		return "", errors.New("artifact must be between 1 byte and 4 MiB")
	}
	detected := http.DetectContentType(data)
	allowed := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}
	ext, ok := allowed[detected]
	if !ok {
		return "", errors.New("unsupported MCP image artifact type")
	}
	if mimeType == "" || !strings.HasPrefix(mimeType, "image/") {
		mimeType = detected
	}
	keyID, err := randomID("", 16)
	if err != nil {
		return "", err
	}
	objectKey := fmt.Sprintf("mcp-artifacts/%d/%s%s", sessionID, keyID, ext)
	if err := s.objects.Put(ctx, objectKey, detected, data); err != nil {
		return "", err
	}
	base := filepath.Base(name)
	if base == "." || base == "" {
		base = "mcp-artifact" + ext
	}
	attachment, err := s.store.CreateAttachment(ctx, s.userID, sessionID, base, detected, objectKey, int64(len(data)))
	if err != nil {
		_ = s.objects.Delete(ctx, objectKey)
		return "", err
	}
	return attachment.ID, nil
}
