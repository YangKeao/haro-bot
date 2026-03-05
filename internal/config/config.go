package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	dbmodel "github.com/YangKeao/haro-bot/internal/db"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Config struct {
	ServerAddr string

	TiDBDSN string

	LLMBaseURL      string
	LLMAPIKey       string
	LLMModel        string
	LLMPromptFormat PromptFormat

	TelegramToken string

	SkillsDir           string
	SkillsRepoAllowlist []string
	SkillsSyncInterval  time.Duration

	FSAllowedRoots    []string
	FSAllowExec       bool
	FSAllowedExecDirs []string

	ToolMaxTurns int
}

type ConfigRecord struct {
	ServerAddr          string   `json:"server_addr"`
	LLMBaseURL          string   `json:"llm_base_url"`
	LLMAPIKey           string   `json:"llm_api_key"`
	LLMModel            string   `json:"llm_model"`
	LLMPromptFormat     string   `json:"llm_prompt_format"`
	TelegramToken       string   `json:"telegram_token"`
	SkillsDir           string   `json:"skills_dir"`
	SkillsRepoAllowlist []string `json:"skills_repo_allowlist"`
	SkillsSyncInterval  string   `json:"skills_sync_interval"`
	FSAllowedRoots      []string `json:"fs_allowed_roots"`
	FSAllowExec         bool     `json:"fs_allow_exec"`
	FSAllowedExecDirs   []string `json:"fs_allowed_exec_dirs"`
	ToolMaxTurns        int      `json:"tool_max_turns"`
}

func LoadBase() Config {
	return Config{
		TiDBDSN: envDefault("TIDB_DSN", "root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true"),
	}
}

func LoadFromDB(ctx context.Context, db *gorm.DB, base Config) (Config, error) {
	if db == nil {
		return Config{}, errors.New("db required")
	}
	rec, found, err := loadRecord(ctx, db)
	if err != nil {
		return Config{}, err
	}
	if !found {
		rec = defaultRecord()
		rec.applyEnvOverrides()
		rec.normalize()
		if err := saveRecord(ctx, db, rec); err != nil {
			return Config{}, err
		}
	} else {
		rec = rec.withDefaults()
		rec.applyEnvOverrides()
		rec.normalize()
		if err := saveRecord(ctx, db, rec); err != nil {
			return Config{}, err
		}
	}
	cfg := rec.toConfig()
	cfg.TiDBDSN = base.TiDBDSN
	return cfg, nil
}

func loadRecord(ctx context.Context, db *gorm.DB) (ConfigRecord, bool, error) {
	var row dbmodel.AppConfig
	if err := db.WithContext(ctx).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ConfigRecord{}, false, nil
		}
		return ConfigRecord{}, false, err
	}
	if len(row.ConfigJSON) == 0 {
		return ConfigRecord{}, true, nil
	}
	var rec ConfigRecord
	if err := json.Unmarshal(row.ConfigJSON, &rec); err != nil {
		return ConfigRecord{}, false, err
	}
	return rec, true, nil
}

func saveRecord(ctx context.Context, db *gorm.DB, rec ConfigRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	row := dbmodel.AppConfig{
		ID:         1,
		ConfigJSON: datatypes.JSON(raw),
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&row).Error
}

func defaultRecord() ConfigRecord {
	skillsDir := "./skills"
	return ConfigRecord{
		ServerAddr:         ":8080",
		LLMBaseURL:         "https://api.openai.com/v1",
		LLMModel:           "gpt-4o-mini",
		LLMPromptFormat:    string(PromptFormatOpenAI),
		SkillsDir:          skillsDir,
		SkillsSyncInterval: "10m",
		FSAllowedRoots:     []string{skillsDir},
		ToolMaxTurns:       1024,
	}
}

func (r ConfigRecord) withDefaults() ConfigRecord {
	def := defaultRecord()
	if r.ServerAddr == "" {
		r.ServerAddr = def.ServerAddr
	}
	if r.LLMBaseURL == "" {
		r.LLMBaseURL = def.LLMBaseURL
	}
	if r.LLMModel == "" {
		r.LLMModel = def.LLMModel
	}
	if r.LLMPromptFormat == "" {
		r.LLMPromptFormat = def.LLMPromptFormat
	}
	if r.SkillsDir == "" {
		r.SkillsDir = def.SkillsDir
	}
	if r.SkillsSyncInterval == "" {
		r.SkillsSyncInterval = def.SkillsSyncInterval
	}
	if len(r.FSAllowedRoots) == 0 {
		r.FSAllowedRoots = []string{r.SkillsDir}
	}
	if r.ToolMaxTurns <= 0 {
		r.ToolMaxTurns = def.ToolMaxTurns
	}
	return r
}

func (r ConfigRecord) toConfig() Config {
	r = r.withDefaults()
	syncInterval := parseDurationDefault(r.SkillsSyncInterval, 10*time.Minute)
	fsRoots := r.FSAllowedRoots
	if len(fsRoots) == 0 {
		fsRoots = []string{r.SkillsDir}
	}
	format := NormalizePromptFormat(r.LLMPromptFormat)
	cfg := Config{
		ServerAddr:          r.ServerAddr,
		LLMBaseURL:          r.LLMBaseURL,
		LLMAPIKey:           r.LLMAPIKey,
		LLMModel:            r.LLMModel,
		LLMPromptFormat:     format,
		TelegramToken:       r.TelegramToken,
		SkillsDir:           r.SkillsDir,
		SkillsRepoAllowlist: r.SkillsRepoAllowlist,
		SkillsSyncInterval:  syncInterval,
		FSAllowedRoots:      fsRoots,
		FSAllowExec:         r.FSAllowExec,
		FSAllowedExecDirs:   r.FSAllowedExecDirs,
		ToolMaxTurns:        r.ToolMaxTurns,
	}
	return cfg
}

func (r *ConfigRecord) normalize() {
	r.LLMPromptFormat = string(NormalizePromptFormat(r.LLMPromptFormat))
}

func (r *ConfigRecord) applyEnvOverrides() {
	if v := os.Getenv("SERVER_ADDR"); v != "" {
		r.ServerAddr = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		r.LLMBaseURL = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		r.LLMAPIKey = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		r.LLMModel = v
	}
	if v := os.Getenv("LLM_PROMPT_FORMAT"); v != "" {
		r.LLMPromptFormat = v
	}
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		r.TelegramToken = v
	}
	if v := os.Getenv("SKILLS_DIR"); v != "" {
		r.SkillsDir = v
	}
	if v := os.Getenv("SKILLS_REPO_ALLOWLIST"); v != "" {
		r.SkillsRepoAllowlist = splitComma(v)
	}
	if v := os.Getenv("SKILLS_SYNC_INTERVAL"); v != "" {
		r.SkillsSyncInterval = v
	}
	if v := os.Getenv("FS_ALLOWED_ROOTS"); v != "" {
		r.FSAllowedRoots = splitComma(v)
	}
	if envBool("FS_ALLOW_EXEC") || envBool("SKILLS_ALLOW_SCRIPTS") {
		r.FSAllowExec = true
	}
	if v := os.Getenv("FS_ALLOWED_EXEC_DIRS"); v != "" {
		r.FSAllowedExecDirs = splitComma(v)
	} else if v := os.Getenv("SKILLS_ALLOWED_SCRIPT_DIRS"); v != "" {
		r.FSAllowedExecDirs = splitComma(v)
	}
	if v := os.Getenv("TOOL_MAX_TURNS"); v != "" {
		if n, err := parseInt(v); err == nil {
			r.ToolMaxTurns = n
		}
	}
}

func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func parseDurationDefault(v string, def time.Duration) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func parseInt(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, errors.New("empty")
	}
	var n int
	_, err := fmt.Sscanf(v, "%d", &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func splitComma(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
