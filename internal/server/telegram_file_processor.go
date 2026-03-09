package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// TextProcessor handles plain text files
type TextProcessor struct{}

func (p *TextProcessor) CanHandle(filename string, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".txt" || ext == ".md" || ext == ".json" ||
		ext == ".csv" || ext == ".log" || ext == ".yaml" || ext == ".yml" ||
		strings.HasPrefix(mimeType, "text/") && !strings.Contains(mimeType, "html")
}

func (p *TextProcessor) Process(ctx context.Context, filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}
	
	// Limit content size
	text := string(content)
	if len(text) > 100*1024 { // 100KB limit
		text = text[:100*1024] + "\n... [truncated]"
	}
	
	return text, nil
}

// HTMLProcessor handles HTML files
type HTMLProcessor struct{}

func (p *HTMLProcessor) CanHandle(filename string, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".html" || ext == ".htm" || mimeType == "text/html"
}

func (p *HTMLProcessor) Process(ctx context.Context, filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}
	
	// Simple HTML tag stripping
	text := stripHTMLTags(string(content))
	
	// Limit content size
	if len(text) > 100*1024 {
		text = text[:100*1024] + "\n... [truncated]"
	}
	
	return text, nil
}

// stripHTMLTags removes HTML tags and decodes basic entities
func stripHTMLTags(html string) string {
	// Remove script and style blocks
	html = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(html, "")
	
	// Remove comments
	html = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(html, "")
	
	// Convert block elements to newlines
	html = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`(?i)</p>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`(?i)</div>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`(?i)</li>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`(?i)</tr>`).ReplaceAllString(html, "\n")
	
	// Remove all other tags
	html = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, "")
	
	// Decode basic HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")
	
	// Clean up whitespace
	lines := strings.Split(html, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	
	return strings.Join(result, "\n")
}

// PDFProcessor handles PDF files
type PDFProcessor struct{}

func (p *PDFProcessor) CanHandle(filename string, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".pdf" || mimeType == "application/pdf"
}

func (p *PDFProcessor) Process(ctx context.Context, filepath string) (string, error) {
	// Try pdftotext command (needs poppler-utils installed)
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", filepath, "-")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: return file info
		return "[PDF file - pdftotext not available]", nil
	}
	
	text := string(output)
	if len(text) > 100*1024 {
		text = text[:100*1024] + "\n... [truncated]"
	}
	
	return text, nil
}

// ImageProcessor handles image files
type ImageProcessor struct{}

func (p *ImageProcessor) CanHandle(filename string, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" ||
		ext == ".gif" || ext == ".webp" || ext == ".bmp" ||
		strings.HasPrefix(mimeType, "image/")
}

func (p *ImageProcessor) Process(ctx context.Context, filepath string) (string, error) {
	// TODO: Integrate with Vision API or OCR service
	// For now, return file info
	info, err := os.Stat(filepath)
	if err != nil {
		return "", err
	}
	
	return fmt.Sprintf("[Image file: %d bytes]", info.Size()), nil
}
