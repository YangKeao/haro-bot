package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Engine struct {
	cfg      config.MemoryConfig
	store    StoreAPI
	llm      *llm.Client
	model    string
	embedder Embedder
	vectors  VectorStore
	graph    GraphStore
	log      *zap.Logger
}

func NewEngine(db *gorm.DB, store StoreAPI, llmClient *llm.Client, model string, cfg config.MemoryConfig) (*Engine, error) {
	if db == nil {
		return nil, errors.New("memory db required")
	}
	if store == nil {
		return nil, errors.New("memory store required")
	}
	if llmClient == nil {
		return nil, errors.New("memory llm client required")
	}
	if strings.TrimSpace(cfg.Embedder.Provider) == "" {
		return nil, errors.New("memory embedder provider required")
	}
	if strings.TrimSpace(cfg.Embedder.Model) == "" {
		return nil, errors.New("memory embedder model required")
	}
	var embedder Embedder
	switch strings.ToLower(strings.TrimSpace(cfg.Embedder.Provider)) {
	case "openai", "openai_compatible":
		emb, err := NewOpenAIEmbedder(cfg.Embedder)
		if err != nil {
			return nil, err
		}
		embedder = emb
	default:
		return nil, fmt.Errorf("unsupported memory embedder provider: %s", cfg.Embedder.Provider)
	}
	vectors := NewTiDBVectorStore(db, cfg.Vector.Distance)
	if err := vectors.EnsureSchema(context.Background(), cfg); err != nil {
		return nil, err
	}
	graph := GraphStore(NewNoopGraphStore())
	return &Engine{
		cfg:      cfg,
		store:    store,
		llm:      llmClient,
		model:    model,
		embedder: embedder,
		vectors:  vectors,
		graph:    graph,
		log:      logging.L().Named("memory_engine"),
	}, nil
}

func (e *Engine) Enabled() bool {
	return e != nil
}

func (e *Engine) Retrieve(ctx context.Context, userID, sessionID int64, query string, limit int) ([]MemoryItem, error) {
	if e == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = e.cfg.Retrieve.TopK
	}
	vec, err := e.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	items, err := e.vectors.Search(ctx, userID, nil, vec, limit)
	if err != nil {
		return nil, err
	}
	minScore := e.cfg.Retrieve.MinScore
	if minScore <= 0 {
		return items, nil
	}
	filtered := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		if item.Score >= minScore {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (e *Engine) Ingest(ctx context.Context, userID, sessionID int64) error {
	if e == nil {
		return nil
	}
	recent, summary, err := e.store.LoadViewMessages(ctx, sessionID, e.cfg.Ingest.RecentWindow)
	if err != nil {
		return err
	}
	turn := selectLatestTurn(recent)
	if turn.User == "" || turn.Assistant == "" {
		return nil
	}
	prompt := buildExtractionPrompt(summaryText(summary), formatRecentMessages(recent), turn)
	candidates, err := e.extractCandidates(ctx, prompt)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	if e.cfg.Ingest.MaxCandidates > 0 && len(candidates) > e.cfg.Ingest.MaxCandidates {
		candidates = candidates[:e.cfg.Ingest.MaxCandidates]
	}
	for _, cand := range candidates {
		if !e.shouldKeepCandidate(cand) {
			continue
		}
		if err := e.ingestCandidate(ctx, userID, sessionID, cand); err != nil {
			e.log.Warn("ingest candidate failed", zap.Error(err))
		}
	}
	return nil
}

func (e *Engine) ingestCandidate(ctx context.Context, userID, sessionID int64, cand MemoryCandidate) error {
	vec, err := e.embedder.Embed(ctx, cand.Memory)
	if err != nil {
		return err
	}
	existing, err := e.vectors.Search(ctx, userID, nil, vec, e.cfg.Ingest.MatchTopK)
	if err != nil {
		return err
	}
	action, err := e.decideAction(ctx, cand, existing)
	if err != nil {
		action = e.fallbackAction(cand, existing)
	}
	content := normalizeMemoryContent(action.Memory, cand.Memory)
	actionType := strings.ToUpper(strings.TrimSpace(action.Action))
	if actionType == MemoryActionAdd || actionType == MemoryActionUpdate {
		if strings.TrimSpace(content) != strings.TrimSpace(cand.Memory) {
			vec, err = e.embedder.Embed(ctx, content)
			if err != nil {
				return err
			}
		}
	}
	switch actionType {
	case MemoryActionAdd:
		item := MemoryItem{
			UserID:    userID,
			SessionID: &sessionID,
			Type:      normalizeMemoryType(action.Type, cand.Type),
			Content:   content,
			Metadata:  candidateMetadata(cand),
		}
		_, err := e.vectors.Insert(ctx, item, vec)
		return err
	case MemoryActionUpdate:
		if action.TargetID == 0 {
			return errors.New("update requires target_id")
		}
		item := MemoryItem{
			ID:        action.TargetID,
			Type:      normalizeMemoryType(action.Type, cand.Type),
			Content:   content,
			Metadata:  candidateMetadata(cand),
			SessionID: &sessionID,
			UserID:    userID,
		}
		return e.vectors.Update(ctx, item, vec)
	case MemoryActionDelete:
		if action.TargetID == 0 {
			return errors.New("delete requires target_id")
		}
		return e.vectors.Delete(ctx, action.TargetID)
	default:
		return nil
	}
}

func (e *Engine) shouldKeepCandidate(cand MemoryCandidate) bool {
	if strings.TrimSpace(cand.Memory) == "" {
		return false
	}
	if cand.Confidence > 0 && cand.Confidence < e.cfg.Ingest.MinConfidence {
		return false
	}
	if cand.Importance > 0 && cand.Importance < float64(e.cfg.Ingest.MinImportance) {
		return false
	}
	return true
}

func (e *Engine) extractCandidates(ctx context.Context, prompt extractionPrompt) ([]MemoryCandidate, error) {
	req := llm.ChatRequest{
		Model: e.model,
		Messages: []llm.Message{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: prompt.String()},
		},
		Temperature: 0,
		Purpose:     llm.PurposeMemory,
	}
	resp, err := e.llm.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("empty extraction response")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return nil, nil
	}
	content = stripJSONCodeFence(content)
	var parsed struct {
		Memories []MemoryCandidate `json:"memories"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, err
	}
	return parsed.Memories, nil
}

func (e *Engine) decideAction(ctx context.Context, cand MemoryCandidate, existing []MemoryItem) (MemoryAction, error) {
	req := llm.ChatRequest{
		Model: e.model,
		Messages: []llm.Message{
			{Role: "system", Content: updateSystemPrompt},
			{Role: "user", Content: buildUpdatePrompt(cand, existing)},
		},
		Temperature: 0,
		Purpose:     llm.PurposeMemory,
	}
	resp, err := e.llm.Chat(ctx, req)
	if err != nil {
		return MemoryAction{}, err
	}
	if len(resp.Choices) == 0 {
		return MemoryAction{}, errors.New("empty update response")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return MemoryAction{}, errors.New("empty update content")
	}
	content = stripJSONCodeFence(content)
	var action MemoryAction
	if err := json.Unmarshal([]byte(content), &action); err != nil {
		return MemoryAction{}, err
	}
	return action, nil
}

func (e *Engine) fallbackAction(cand MemoryCandidate, existing []MemoryItem) MemoryAction {
	bestID, bestScore := bestMatch(existing)
	if bestScore >= e.cfg.Ingest.NoopThreshold {
		return MemoryAction{Action: MemoryActionNoop}
	}
	if bestScore >= e.cfg.Ingest.UpdateThreshold {
		return MemoryAction{Action: MemoryActionUpdate, TargetID: bestID, Memory: cand.Memory, Type: cand.Type}
	}
	return MemoryAction{Action: MemoryActionAdd, Memory: cand.Memory, Type: cand.Type}
}

func bestMatch(existing []MemoryItem) (int64, float64) {
	var bestID int64
	var bestScore float64
	for _, item := range existing {
		if item.Score > bestScore {
			bestScore = item.Score
			bestID = item.ID
		}
	}
	return bestID, bestScore
}

type extractionPrompt struct {
	Summary   string
	Recent    string
	User      string
	Assistant string
}

func buildExtractionPrompt(summary, recent string, turn turnPair) extractionPrompt {
	return extractionPrompt{Summary: summary, Recent: recent, User: turn.User, Assistant: turn.Assistant}
}

func (p extractionPrompt) String() string {
	out := extractionUserTemplate
	out = strings.ReplaceAll(out, "{{summary}}", safeBlock(p.Summary))
	out = strings.ReplaceAll(out, "{{recent}}", safeBlock(p.Recent))
	out = strings.ReplaceAll(out, "{{user}}", safeBlock(p.User))
	out = strings.ReplaceAll(out, "{{assistant}}", safeBlock(p.Assistant))
	return out
}

type turnPair struct {
	User      string
	Assistant string
}

func selectLatestTurn(messages []Message) turnPair {
	var turn turnPair
	var assistantIdx = -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			assistantIdx = i
			turn.Assistant = messages[i].Content
			break
		}
	}
	if assistantIdx == -1 {
		return turn
	}
	for i := assistantIdx - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			turn.User = messages[i].Content
			break
		}
	}
	return turn
}

func formatRecentMessages(messages []Message) string {
	var b strings.Builder
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		b.WriteString(msg.Role)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(msg.Content))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func summaryText(summary *Summary) string {
	if summary == nil {
		return ""
	}
	if summary.Summary != "" {
		return summary.Summary
	}
	if len(summary.State) > 0 {
		data, _ := json.Marshal(summary.State)
		return string(data)
	}
	return ""
}

func buildUpdatePrompt(cand MemoryCandidate, existing []MemoryItem) string {
	candJSON, _ := json.Marshal(cand)
	var b strings.Builder
	for _, item := range existing {
		b.WriteString(fmt.Sprintf("- id=%d score=%.4f type=%s content=%s\n", item.ID, item.Score, item.Type, item.Content))
	}
	out := updateUserTemplate
	out = strings.ReplaceAll(out, "{{candidate}}", string(candJSON))
	out = strings.ReplaceAll(out, "{{existing}}", safeBlock(strings.TrimSpace(b.String())))
	return out
}

func stripJSONCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func safeBlock(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(none)"
	}
	return text
}

func normalizeMemoryType(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func normalizeMemoryContent(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func candidateMetadata(cand MemoryCandidate) map[string]any {
	meta := map[string]any{
		"importance": cand.Importance,
		"confidence": cand.Confidence,
	}
	if len(cand.Tags) > 0 {
		meta["tags"] = cand.Tags
	}
	if cand.Source != "" {
		meta["source"] = cand.Source
	}
	return meta
}

func (e *Engine) IngestAsync(userID, sessionID int64) {
	if e == nil {
		return
	}
	go func() {
		ctx := context.Background()
		if err := e.Ingest(ctx, userID, sessionID); err != nil {
			e.log.Warn("memory ingest failed", zap.Error(err))
		}
	}()
}
