package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

const (
	sourceTypeGit = "git"
	// TODO: support claude marketplace and clawdhub sources.
)

type Manager struct {
	store     *Store
	baseDir   string
	allowlist []string

	mu     sync.RWMutex
	skills map[string]Metadata
}

func NewManager(store *Store, baseDir string, allowlist []string) *Manager {
	mgr := &Manager{
		store:     store,
		baseDir:   baseDir,
		allowlist: allowlist,
		skills:    make(map[string]Metadata),
	}
	if store != nil {
		_ = mgr.loadFromDB(context.Background())
	}
	return mgr
}

func (m *Manager) RegisterSource(ctx context.Context, src Source) (int64, error) {
	log := logging.L().Named("skills")
	src.SourceType = strings.ToLower(strings.TrimSpace(src.SourceType))
	if src.SourceType == "" {
		return 0, errors.New("source_type required")
	}
	if src.InstallMethod == "" {
		src.InstallMethod = src.SourceType
	}
	if src.Status == "" {
		src.Status = "active"
	}
	switch src.SourceType {
	case sourceTypeGit:
		src.URL = strings.TrimSpace(src.URL)
		if src.URL == "" {
			return 0, errors.New("url required")
		}
		if strings.HasPrefix(strings.ToLower(src.URL), "file://") {
			return 0, errors.New("file protocol not allowed")
		}
		if len(m.allowlist) > 0 && !allowedRepo(src.URL, m.allowlist) {
			return 0, errors.New("skills repo url not allowed")
		}
		src.Ref = strings.TrimSpace(src.Ref)
		if src.Ref == "" {
			src.Ref = "main"
		}
		cleanSubdir, err := normalizeSubdir(src.Subdir)
		if err != nil {
			return 0, err
		}
		src.Subdir = cleanSubdir
	default:
		return 0, errors.New("unsupported source_type")
	}
	id, err := m.store.UpsertSource(ctx, src)
	if err != nil {
		log.Warn("register source failed", zap.Error(err), zap.String("source_type", src.SourceType), zap.String("url", src.URL))
		return 0, err
	}
	log.Info("registered skill source", zap.Int64("source_id", id), zap.String("source_type", src.SourceType), zap.String("url", src.URL))
	return id, nil
}

func (m *Manager) RefreshAll(ctx context.Context) error {
	log := logging.L().Named("skills")
	sources, err := m.store.ListSources(ctx, false)
	if err != nil {
		return err
	}
	log.Info("refreshing skill sources", zap.Int("count", len(sources)))
	merged := make(map[string]Metadata)
	var firstErr error
	for _, src := range sources {
		log.Debug("refreshing source", zap.Int64("source_id", src.ID), zap.String("source_type", src.SourceType))
		version, err := m.refreshSource(ctx, src, merged)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			_ = m.store.UpdateSourceSync(ctx, src.ID, "", err.Error())
			log.Warn("refresh source failed", zap.Int64("source_id", src.ID), zap.Error(err))
			continue
		}
		_ = m.store.UpdateSourceSync(ctx, src.ID, version, "")
	}
	m.mu.Lock()
	m.skills = merged
	m.mu.Unlock()
	log.Info("skills refreshed", zap.Int("count", len(merged)))
	return firstErr
}

func (m *Manager) RefreshSource(ctx context.Context, sourceID int64) error {
	log := logging.L().Named("skills")
	sources, err := m.store.ListSources(ctx, true)
	if err != nil {
		return err
	}
	var target *Source
	for i := range sources {
		if sources[i].ID == sourceID {
			target = &sources[i]
			break
		}
	}
	if target == nil {
		return errors.New("source not found")
	}
	merged := make(map[string]Metadata)
	version, err := m.refreshSource(ctx, *target, merged)
	if err != nil {
		_ = m.store.UpdateSourceSync(ctx, target.ID, "", err.Error())
		log.Warn("refresh source failed", zap.Int64("source_id", target.ID), zap.Error(err))
		return err
	}
	_ = m.store.UpdateSourceSync(ctx, target.ID, version, "")

	m.mu.Lock()
	for name, meta := range merged {
		m.skills[name] = meta
	}
	m.mu.Unlock()
	log.Info("source refreshed", zap.Int64("source_id", target.ID), zap.Int("skills", len(merged)))
	return nil
}

func (m *Manager) List() []Metadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Metadata, 0, len(m.skills))
	for _, meta := range m.skills {
		out = append(out, meta)
	}
	return out
}

func (m *Manager) loadFromDB(ctx context.Context) error {
	log := logging.L().Named("skills")
	if m.store == nil {
		return errors.New("store not configured")
	}
	entries, err := m.store.ListSkills(ctx)
	if err != nil {
		return err
	}
	merged := make(map[string]Metadata)
	for _, entry := range entries {
		meta, ok := m.metadataFromEntry(entry)
		if !ok {
			continue
		}
		if _, exists := merged[meta.Name]; !exists {
			merged[meta.Name] = meta
		}
	}
	m.mu.Lock()
	m.skills = merged
	m.mu.Unlock()
	log.Debug("loaded skills from db", zap.Int("count", len(merged)))
	return nil
}

func (m *Manager) ListBySource(ctx context.Context, sourceID int64) ([]Metadata, error) {
	if m.store == nil {
		return nil, errors.New("store not configured")
	}
	entries, err := m.store.ListSkillsBySource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	out := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		meta, ok := m.metadataFromEntry(entry)
		if !ok {
			continue
		}
		out = append(out, meta)
	}
	return out, nil
}

func (m *Manager) metadataFromEntry(entry RegistryEntry) (Metadata, bool) {
	if entry.Status != "" && entry.Status != "active" {
		return Metadata{}, false
	}
	if strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Description) == "" {
		return Metadata{}, false
	}
	repoDir := filepath.Join(m.baseDir, fmt.Sprintf("source-%d", entry.SourceID))
	dir, err := safeJoinAllowMissing(repoDir, entry.SkillPath)
	if err != nil {
		return Metadata{}, false
	}
	return Metadata{
		Name:        entry.Name,
		Description: entry.Description,
		Dir:         dir,
		Version:     entry.Version,
		Hash:        entry.ContentHash,
	}, true
}

func (m *Manager) Load(name string) (Skill, error) {
	log := logging.L().Named("skills")
	m.mu.RLock()
	meta, ok := m.skills[name]
	m.mu.RUnlock()
	if !ok {
		return Skill{}, errors.New("skill not found")
	}
	skillFile := filepath.Join(meta.Dir, "SKILL.md")
	if err := ensureNoSymlink(skillFile); err != nil {
		return Skill{}, err
	}
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return Skill{}, err
	}
	_, body, hash, err := parseSkillFile(data)
	if err != nil {
		log.Warn("load skill failed", zap.String("name", name), zap.Error(err))
		return Skill{}, err
	}
	meta.Hash = hash
	return Skill{
		Metadata:     meta,
		Instructions: body,
	}, nil
}

func (m *Manager) refreshSource(ctx context.Context, src Source, merged map[string]Metadata) (string, error) {
	log := logging.L().Named("skills")
	if src.SourceType != sourceTypeGit {
		return "", errors.New("unsupported source_type")
	}
	if len(m.allowlist) > 0 && !allowedRepo(src.URL, m.allowlist) {
		return "", errors.New("skills repo url not allowed")
	}
	repoDir := filepath.Join(m.baseDir, fmt.Sprintf("source-%d", src.ID))
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", err
	}
	_, version, err := syncRepo(ctx, src.URL, src.Ref, repoDir)
	if err != nil {
		log.Warn("sync repo failed", zap.Int64("source_id", src.ID), zap.Error(err))
		return "", err
	}
	root := repoDir
	if strings.TrimSpace(src.Subdir) != "" {
		// NOTE: we only scan the subdirectory; go-git lacks sparse checkout for true partial clones.
		cleanSubdir, err := normalizeSubdir(src.Subdir)
		if err != nil {
			return "", err
		}
		root, err = safeJoin(repoDir, cleanSubdir)
		if err != nil {
			return "", err
		}
	}
	err = m.scanSource(ctx, src, repoDir, root, version, merged)
	if err != nil {
		log.Warn("scan source failed", zap.Int64("source_id", src.ID), zap.Error(err))
	}
	return version, err
}

func (m *Manager) scanSource(ctx context.Context, src Source, repoDir, root, version string, merged map[string]Metadata) error {
	log := logging.L().Named("skills")
	repoDirAbs, err := filepath.Abs(repoDir)
	if err != nil {
		repoDirAbs = repoDir
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		dir := filepath.Dir(path)
		if err := ensureNoSymlink(path); err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fm, _, hash, err := parseSkillFile(data)
		if err != nil {
			return nil
		}
		name := strings.TrimSpace(fm.Name)
		desc := strings.TrimSpace(fm.Description)
		if name == "" || desc == "" {
			return nil
		}
		if filepath.Base(dir) != name {
			return nil
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			absDir = dir
		}
		relPath, err := filepath.Rel(repoDirAbs, absDir)
		if err != nil {
			return nil
		}
		meta := Metadata{
			Name:        name,
			Description: desc,
			Dir:         absDir,
			Version:     version,
			Hash:        hash,
		}
		if _, exists := merged[name]; !exists {
			merged[name] = meta
		}
		if err := m.store.UpsertSkill(ctx, RegistryEntry{
			SourceID:    src.ID,
			Name:        name,
			Description: desc,
			Version:     version,
			SkillPath:   relPath,
			ContentHash: hash,
			Status:      "active",
		}); err != nil {
			log.Warn("upsert skill failed", zap.Int64("source_id", src.ID), zap.String("name", name), zap.Error(err))
			return err
		}
		return nil
	})
}

func normalizeSubdir(subdir string) (string, error) {
	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return "", nil
	}
	clean := filepath.Clean(subdir)
	if clean == "." {
		return "", nil
	}
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", errors.New("invalid subdir")
	}
	return clean, nil
}

func allowedRepo(repo string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if strings.HasPrefix(repo, allowed) {
			return true
		}
	}
	return false
}
