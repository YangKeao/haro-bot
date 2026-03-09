package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// FileHandler handles Telegram file messages
type FileHandler struct {
	bot        *bot.Bot
	tempDir    string
	maxSize    int64 // max file size in bytes
	processors []FileProcessor
}

// FileProcessor interface for processing different file types
type FileProcessor interface {
	CanHandle(filename string, mimeType string) bool
	Process(ctx context.Context, filepath string) (string, error)
}

// NewFileHandler creates a new file handler
func NewFileHandler(b *bot.Bot, tempDir string, maxSize int64) *FileHandler {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024 // 50MB default
	}

	return &FileHandler{
		bot:     b,
		tempDir: tempDir,
		maxSize: maxSize,
		processors: []FileProcessor{
			&TextProcessor{},
			&HTMLProcessor{},
			&PDFProcessor{},
			&ImageProcessor{},
		},
	}
}

// ExtractFileContent extracts content from Telegram message files
func (h *FileHandler) ExtractFileContent(ctx context.Context, msg *models.Message) string {
	log := logging.L().Named("telegram_file")

	var content string
	var err error

	// Handle Document
	if msg.Document != nil {
		content, err = h.handleDocument(ctx, msg.Document)
		if err != nil {
			log.Warn("document processing error", zap.Error(err), zap.String("filename", msg.Document.FileName))
			return ""
		}
		return content
	}

	// Handle Photo (get the largest size)
	if len(msg.Photo) > 0 {
		// Find largest photo
		var largestPhoto *models.PhotoSize
		for i := range msg.Photo {
			if largestPhoto == nil || msg.Photo[i].FileSize > largestPhoto.FileSize {
				largestPhoto = &msg.Photo[i]
			}
		}
		if largestPhoto != nil {
			content, err = h.handlePhoto(ctx, largestPhoto, len(msg.Photo))
			if err != nil {
				log.Warn("photo processing error", zap.Error(err))
				return ""
			}
			return content
		}
	}

	// Handle Video
	if msg.Video != nil {
		content, err = h.handleVideo(ctx, msg.Video)
		if err != nil {
			log.Warn("video processing error", zap.Error(err))
			return ""
		}
		return content
	}

	// Handle Audio
	if msg.Audio != nil {
		content, err = h.handleAudio(ctx, msg.Audio)
		if err != nil {
			log.Warn("audio processing error", zap.Error(err))
			return ""
		}
		return content
	}

	// Handle Voice
	if msg.Voice != nil {
		content, err = h.handleVoice(ctx, msg.Voice)
		if err != nil {
			log.Warn("voice processing error", zap.Error(err))
			return ""
		}
		return content
	}

	return ""
}

func (h *FileHandler) handleDocument(ctx context.Context, doc *models.Document) (string, error) {
	// Check file size
	if doc.FileSize > h.maxSize {
		return "", fmt.Errorf("file too large: %d bytes (max: %d)", doc.FileSize, h.maxSize)
	}

	// Download file
	filepath, err := h.downloadFile(ctx, doc.FileID, doc.FileName)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(filepath)

	// Detect mime type
	mimeType := doc.MimeType
	if mimeType == "" {
		mimeType = detectMimeType(doc.FileName)
	}

	// Find appropriate processor
	for _, processor := range h.processors {
		if processor.CanHandle(doc.FileName, mimeType) {
			content, err := processor.Process(ctx, filepath)
			if err != nil {
				return "", fmt.Errorf("processing failed: %w", err)
			}
			return formatFileContent("Document", doc.FileName, mimeType, content), nil
		}
	}

	// Unsupported type - return basic info
	return formatFileInfo("Document", doc.FileName, mimeType, doc.FileSize), nil
}

func (h *FileHandler) handlePhoto(ctx context.Context, photo *models.PhotoSize, count int) (string, error) {
	// Download photo
	filename := fmt.Sprintf("photo_%d.jpg", photo.FileID)
	filepath, err := h.downloadFile(ctx, photo.FileID, filename)
	if err != nil {
		return "", err
	}
	defer os.Remove(filepath)

	// Try to process with image processor
	for _, processor := range h.processors {
		if processor.CanHandle(filename, "image/jpeg") {
			content, err := processor.Process(ctx, filepath)
			if err != nil {
				return "", err
			}
			info := fmt.Sprintf("Photo (%d sizes available)", count)
			return formatFileContent("Photo", info, "image/jpeg", content), nil
		}
	}

	return fmt.Sprintf("[Photo: %d bytes, %dx%d]", photo.FileSize, photo.Width, photo.Height), nil
}

func (h *FileHandler) handleVideo(ctx context.Context, video *models.Video) (string, error) {
	return fmt.Sprintf("[Video: %s, duration: %ds, %dx%d, %d bytes]",
		video.MimeType, video.Duration, video.Width, video.Height, video.FileSize), nil
}

func (h *FileHandler) handleAudio(ctx context.Context, audio *models.Audio) (string, error) {
	filename := audio.FileName
	if filename == "" {
		filename = audio.Title
	}
	return fmt.Sprintf("[Audio: %s, duration: %ds, %d bytes]",
		filename, audio.Duration, audio.FileSize), nil
}

func (h *FileHandler) handleVoice(ctx context.Context, voice *models.Voice) (string, error) {
	return fmt.Sprintf("[Voice message: duration: %ds, %d bytes]",
		voice.Duration, voice.FileSize), nil
}

func (h *FileHandler) downloadFile(ctx context.Context, fileID string, filename string) (string, error) {
	// Get file info from Telegram
	file, err := h.bot.GetFile(ctx, &bot.GetFileParams{
		FileID: fileID,
	})
	if err != nil {
		return "", fmt.Errorf("get file info failed: %w", err)
	}

	// Construct download URL
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", h.bot.Token(), file.FilePath)

	// Download file
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Create temp file with safe filename
	safeFilename := sanitizeFilename(filename)
	tmpPath := filepath.Join(h.tempDir, fmt.Sprintf("%s_%s", fileID, safeFilename))

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file failed: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("save file failed: %w", err)
	}

	return tmpPath, nil
}

// Helper functions

func formatFileContent(fileType, filename, mimeType, content string) string {
	return fmt.Sprintf("[%s: %s (%s)]\n%s", fileType, filename, mimeType, content)
}

func formatFileInfo(fileType, filename, mimeType string, size int64) string {
	return fmt.Sprintf("[%s: %s (%s, %d bytes)]", fileType, filename, mimeType, size)
}

func detectMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	mimeTypes := map[string]string{
		".txt":  "text/plain",
		".md":   "text/markdown",
		".html": "text/html",
		".htm":  "text/html",
		".json": "application/json",
		".csv":  "text/csv",
		".pdf":  "application/pdf",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".mp3":  "audio/mpeg",
		".mp4":  "video/mp4",
	}
	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}
	return "application/octet-stream"
}

func sanitizeFilename(filename string) string {
	// Remove path separators and null bytes
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	filename = strings.ReplaceAll(filename, "\x00", "")
	// Limit length
	if len(filename) > 200 {
		ext := filepath.Ext(filename)
		name := filename[:200-len(ext)]
		filename = name + ext
	}
	return filename
}
