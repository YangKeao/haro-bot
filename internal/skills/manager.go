package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/skillbundle"
	skillsgit "github.com/YangKeao/haro-bot/internal/skills/source/git"
	"go.uber.org/zap"
)

const (
	sourceTypeGit = "git"
	// TODO: support claude marketplace and clawdhub sources.
)

var ErrSourceNotActive = errors.New("skill source is not active")

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
	var err error
	src, err = m.normalizeSource(src)
	if err != nil {
		return 0, err
	}
	id, err := m.store.UpsertSource(ctx, src)
	if err != nil {
		log.Warn("register source failed", zap.Error(err), zap.String("source_type", src.SourceType), zap.String("url", src.URL))
		return 0, err
	}
	log.Info("registered skill source", zap.Int64("source_id", id), zap.String("source_type", src.SourceType), zap.String("url", src.URL))
	return id, nil
}

func (m *Manager) normalizeSource(src Source) (Source, error) {
	src.SourceType = strings.ToLower(strings.TrimSpace(src.SourceType))
	if src.SourceType == "" {
		return Source{}, errors.New("source_type required")
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
			return Source{}, errors.New("url required")
		}
		if strings.HasPrefix(strings.ToLower(src.URL), "file://") {
			return Source{}, errors.New("file protocol not allowed")
		}
		if len(m.allowlist) > 0 && !allowedRepo(src.URL, m.allowlist) {
			return Source{}, errors.New("skills repo url not allowed")
		}
		src.Ref = strings.TrimSpace(src.Ref)
		if src.Ref == "" {
			src.Ref = "main"
		}
		src.SkillFilters = normalizeSkillFilters(src.SkillFilters)
		cleanSubdir, err := normalizeSubdir(src.Subdir)
		if err != nil {
			return Source{}, err
		}
		src.Subdir = cleanSubdir
	default:
		return Source{}, errors.New("unsupported source_type")
	}
	return src, nil
}

func (m *Manager) UpdateSource(ctx context.Context, sourceID int64, src Source) error {
	if m.store == nil {
		return errors.New("store not configured")
	}
	existing, err := m.store.GetSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if existing.Status != "active" {
		return ErrSourceNotActive
	}
	src.SourceType = existing.SourceType
	src.InstallMethod = existing.InstallMethod
	src.Status = existing.Status
	src, err = m.normalizeSource(src)
	if err != nil {
		return err
	}
	if err := m.store.UpdateSource(ctx, sourceID, src); err != nil {
		return err
	}
	logging.L().Named("skills").Info("updated skill source", zap.Int64("source_id", sourceID), zap.String("url", src.URL))
	return nil
}

func (m *Manager) RestoreSource(ctx context.Context, sourceID int64) error {
	if m.store == nil {
		return errors.New("store not configured")
	}
	if err := m.store.RestoreSource(ctx, sourceID); err != nil {
		return err
	}
	logging.L().Named("skills").Info("restored skill source", zap.Int64("source_id", sourceID))
	return nil
}

func (m *Manager) RefreshAll(ctx context.Context) error {
	log := logging.L().Named("skills")
	sources, err := m.store.ListSources(ctx, false)
	if err != nil {
		return err
	}
	log.Info("refreshing skill sources", zap.Int("count", len(sources)))
	merged := make(map[string]Metadata)
	conflicts := make(map[string]struct{})
	var firstErr error
	for _, src := range sources {
		log.Debug("refreshing source", zap.Int64("source_id", src.ID), zap.String("source_type", src.SourceType))
		version, err := m.refreshSource(ctx, src, merged, conflicts)
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
	if err := m.loadFromDB(ctx); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else {
		active := make(map[string]Metadata)
		for _, meta := range m.List() {
			active[meta.Name] = meta
		}
		m.gcBundles(active, 7*24*time.Hour)
	}
	log.Info("skills refreshed", zap.Int("count", len(m.List())))
	return firstErr
}

func (m *Manager) RefreshSource(ctx context.Context, sourceID int64) error {
	log := logging.L().Named("skills")
	target, err := m.store.GetSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if target.Status != "active" {
		return ErrSourceNotActive
	}
	merged := make(map[string]Metadata)
	version, err := m.refreshSource(ctx, target, merged, make(map[string]struct{}))
	if err != nil {
		_ = m.store.UpdateSourceSync(ctx, target.ID, "", err.Error())
		log.Warn("refresh source failed", zap.Int64("source_id", target.ID), zap.Error(err))
		return err
	}
	_ = m.store.UpdateSourceSync(ctx, target.ID, version, "")
	if err := m.loadFromDB(ctx); err != nil {
		return err
	}
	log.Info("source refreshed", zap.Int64("source_id", target.ID), zap.Int("skills", len(merged)))
	return nil
}

func (m *Manager) GetSource(ctx context.Context, sourceID int64) (Source, error) {
	if m.store == nil {
		return Source{}, errors.New("store not configured")
	}
	return m.store.GetSource(ctx, sourceID)
}

func (m *Manager) ListSources(ctx context.Context, includeDisabled bool) ([]Source, error) {
	if m.store == nil {
		return nil, errors.New("store not configured")
	}
	return m.store.ListSources(ctx, includeDisabled)
}

func (m *Manager) DeleteSource(ctx context.Context, sourceID int64) error {
	if m.store == nil {
		return errors.New("store not configured")
	}
	if sourceID <= 0 {
		return errors.New("source_id required")
	}
	if err := m.store.DeleteSource(ctx, sourceID); err != nil {
		return err
	}
	if err := os.RemoveAll(m.repoDirForSource(sourceID)); err != nil {
		logging.L().Named("skills").Warn("remove source dir failed", zap.Int64("source_id", sourceID), zap.Error(err))
	}
	return m.loadFromDB(ctx)
}

func (m *Manager) List() []Metadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Metadata, 0, len(m.skills))
	for _, meta := range m.skills {
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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
	conflicts := make(map[string]struct{})
	for _, entry := range entries {
		meta, ok := m.metadataFromEntry(entry)
		if !ok {
			continue
		}
		mergeMetadata(merged, conflicts, meta)
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
	dir := skillbundle.BundleDir(m.baseDir, entry.ContentHash)
	manifest, err := skillbundle.Scan(dir)
	if err != nil || manifest.Hash != entry.ContentHash {
		repoDir := filepath.Join(m.baseDir, fmt.Sprintf("source-%d", entry.SourceID))
		sourceDir, joinErr := safeJoinAllowMissing(repoDir, entry.SkillPath)
		if joinErr != nil {
			return Metadata{}, false
		}
		manifest, dir, err = skillbundle.Snapshot(m.baseDir, sourceDir)
	}
	if err != nil {
		return Metadata{}, false
	}
	return Metadata{
		Name:        entry.Name,
		Description: entry.Description,
		Dir:         dir,
		Version:     entry.Version,
		Hash:        manifest.Hash,
	}, true
}

func (m *Manager) Get(name string) (Metadata, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.skills[name]
	return meta, ok
}

func Package(meta Metadata) string {
	return "skill://" + meta.Name + "/" + meta.Hash
}

func (m *Manager) ReadResource(name, hash, resource string) (Metadata, []byte, error) {
	meta, ok := m.Get(name)
	if !ok {
		return Metadata{}, nil, errors.New("skill not found")
	}
	if hash != meta.Hash || !skillbundle.ValidHash(hash) {
		return Metadata{}, nil, errors.New("skill package is not current")
	}
	if resource == "" {
		resource = "SKILL.md"
	}
	if !skillbundle.ValidResourcePath(resource) {
		return Metadata{}, nil, errors.New("invalid skill resource path")
	}
	path, err := safeJoin(meta.Dir, filepath.FromSlash(resource))
	if err != nil {
		return Metadata{}, nil, err
	}
	if err := ensureNoSymlink(path); err != nil {
		return Metadata{}, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, nil, err
	}
	if int64(len(data)) > skillbundle.MaxFileSize {
		return Metadata{}, nil, errors.New("skill resource is too large")
	}
	return meta, data, nil
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
	_, body, err := parseSkillFile(data)
	if err != nil {
		log.Warn("load skill failed", zap.String("name", name), zap.Error(err))
		return Skill{}, err
	}
	return Skill{
		Metadata:     meta,
		Instructions: body,
	}, nil
}

func (m *Manager) refreshSource(ctx context.Context, src Source, merged map[string]Metadata, conflicts map[string]struct{}) (string, error) {
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
	_, version, err := skillsgit.SyncRepo(ctx, src.URL, src.Ref, repoDir)
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
	entries, metas, err := m.scanSource(ctx, src, repoDir, root, version)
	if err != nil {
		log.Warn("scan source failed", zap.Int64("source_id", src.ID), zap.Error(err))
		return version, err
	}
	if err := m.store.ReplaceSkillsForSource(ctx, src.ID, entries); err != nil {
		log.Warn("replace skills failed", zap.Int64("source_id", src.ID), zap.Error(err))
		return version, err
	}
	for _, meta := range metas {
		mergeMetadata(merged, conflicts, meta)
	}
	return version, nil
}

func (m *Manager) scanSource(_ context.Context, src Source, repoDir, root, version string) ([]RegistryEntry, []Metadata, error) {
	repoDirAbs, err := filepath.Abs(repoDir)
	if err != nil {
		repoDirAbs = repoDir
	}
	filterSet := make(map[string]struct{}, len(src.SkillFilters))
	for _, filter := range src.SkillFilters {
		filterSet[filter] = struct{}{}
	}
	var entries []RegistryEntry
	var metas []Metadata
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		fm, _, err := parseSkillFile(data)
		if err != nil {
			return nil
		}
		name := strings.TrimSpace(fm.Name)
		desc := strings.TrimSpace(fm.Description)
		if name == "" || desc == "" {
			return nil
		}
		if len(filterSet) > 0 {
			if _, ok := filterSet[name]; !ok {
				return nil
			}
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
		manifest, bundleDir, err := skillbundle.Snapshot(m.baseDir, absDir)
		if err != nil {
			return fmt.Errorf("bundle skill %s: %w", name, err)
		}
		meta := Metadata{
			Name:        name,
			Description: desc,
			Dir:         bundleDir,
			Version:     version,
			Hash:        manifest.Hash,
		}
		metas = append(metas, meta)
		entries = append(entries, RegistryEntry{
			SourceID:    src.ID,
			Name:        name,
			Description: desc,
			Version:     version,
			SkillPath:   relPath,
			ContentHash: manifest.Hash,
			Status:      "active",
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].Name < metas[j].Name
	})
	return entries, metas, nil
}

func mergeMetadata(merged map[string]Metadata, conflicts map[string]struct{}, meta Metadata) {
	if _, conflicted := conflicts[meta.Name]; conflicted {
		return
	}
	if _, exists := merged[meta.Name]; exists {
		delete(merged, meta.Name)
		conflicts[meta.Name] = struct{}{}
		logging.L().Named("skills").Warn("duplicate skill name excluded", zap.String("name", meta.Name))
		return
	}
	merged[meta.Name] = meta
}

func (m *Manager) gcBundles(active map[string]Metadata, grace time.Duration) {
	referenced := make(map[string]struct{}, len(active))
	for _, meta := range active {
		referenced[meta.Hash] = struct{}{}
	}
	root := filepath.Join(m.baseDir, "bundles", "sha256")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-grace)
	for _, entry := range entries {
		if !entry.IsDir() || !skillbundle.ValidHash(entry.Name()) {
			continue
		}
		if _, ok := referenced[entry.Name()]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			logging.L().Named("skills").Warn("remove stale skill bundle failed", zap.String("hash", entry.Name()), zap.Error(err))
		}
	}
}

func (m *Manager) repoDirForSource(sourceID int64) string {
	return filepath.Join(m.baseDir, fmt.Sprintf("source-%d", sourceID))
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
