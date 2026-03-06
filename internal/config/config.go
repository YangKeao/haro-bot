package config

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type LogConfig struct {
	Level       string `toml:"level"`
	Development bool   `toml:"development"`
	Encoding    string `toml:"encoding"`
}

type Config struct {
	ServerAddr string

	TiDBDSN string

	LLMBaseURL      string
	LLMAPIKey       string
	LLMModel        string
	LLMPromptFormat PromptFormat

	LLMReasoningEnabled              bool
	LLMReasoningEffort               string
	LLMHTTPDebug                     bool
	LLMContextWindow                 int
	LLMAutoCompactTokenLimit         int
	LLMEffectiveContextWindowPercent int

	TelegramToken string

	SkillsDir           string
	SkillsRepoAllowlist []string
	SkillsSyncInterval  time.Duration

	BraveSearchAPIKey string

	FSAllowedRoots    []string
	FSAllowedExecDirs []string

	ToolMaxTurns int

	Log LogConfig
}

type fileConfig struct {
	ServerAddr string `toml:"server_addr"`

	TiDBDSN string `toml:"tidb_dsn"`

	LLMBaseURL      string `toml:"llm_base_url"`
	LLMAPIKey       string `toml:"llm_api_key"`
	LLMModel        string `toml:"llm_model"`
	LLMPromptFormat string `toml:"llm_prompt_format"`

	LLMReasoningEnabled              bool   `toml:"llm_reasoning_enabled"`
	LLMReasoningEffort               string `toml:"llm_reasoning_effort"`
	LLMHTTPDebug                     bool   `toml:"llm_http_debug"`
	LLMContextWindow                 int    `toml:"llm_context_window"`
	LLMAutoCompactTokenLimit         int    `toml:"llm_auto_compact_token_limit"`
	LLMEffectiveContextWindowPercent int    `toml:"llm_effective_context_window_percent"`

	TelegramToken string `toml:"telegram_token"`

	SkillsDir           string   `toml:"skills_dir"`
	SkillsRepoAllowlist []string `toml:"skills_repo_allowlist"`
	SkillsSyncInterval  string   `toml:"skills_sync_interval"`

	BraveSearchAPIKey string `toml:"brave_search_api_key"`

	FSAllowedRoots    []string `toml:"fs_allowed_roots"`
	FSAllowedExecDirs []string `toml:"fs_allowed_exec_dirs"`

	ToolMaxTurns int `toml:"tool_max_turns"`

	Log LogConfig `toml:"log"`
}

func LoadFromFile(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, errors.New("config path required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var rec fileConfig
	if err := toml.Unmarshal(data, &rec); err != nil {
		return Config{}, err
	}
	rec = rec.withDefaults()
	rec.normalize()
	return rec.toConfig(), nil
}

func defaultFileConfig() fileConfig {
	skillsDir := "./skills"
	return fileConfig{
		ServerAddr:                       ":8080",
		TiDBDSN:                          "root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true",
		LLMBaseURL:                       "https://api.openai.com/v1",
		LLMModel:                         "gpt-4o-mini",
		LLMPromptFormat:                  string(PromptFormatOpenAI),
		LLMEffectiveContextWindowPercent: 95,
		SkillsDir:                        skillsDir,
		SkillsSyncInterval:               "10m",
		FSAllowedRoots:                   []string{skillsDir},
		ToolMaxTurns:                     1024,
		Log: LogConfig{
			Level:       "info",
			Development: false,
			Encoding:    "json",
		},
	}
}

func (r fileConfig) withDefaults() fileConfig {
	def := defaultFileConfig()
	if r.ServerAddr == "" {
		r.ServerAddr = def.ServerAddr
	}
	if r.TiDBDSN == "" {
		r.TiDBDSN = def.TiDBDSN
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
	if r.LLMEffectiveContextWindowPercent <= 0 {
		r.LLMEffectiveContextWindowPercent = def.LLMEffectiveContextWindowPercent
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
	if strings.TrimSpace(r.Log.Level) == "" {
		r.Log.Level = def.Log.Level
	}
	if strings.TrimSpace(r.Log.Encoding) == "" {
		r.Log.Encoding = def.Log.Encoding
	}
	return r
}

func (r *fileConfig) normalize() {
	r.LLMPromptFormat = string(NormalizePromptFormat(r.LLMPromptFormat))
	r.LLMReasoningEffort = strings.ToLower(strings.TrimSpace(r.LLMReasoningEffort))
	if r.LLMEffectiveContextWindowPercent <= 0 {
		r.LLMEffectiveContextWindowPercent = 95
	}
	if r.LLMEffectiveContextWindowPercent > 100 {
		r.LLMEffectiveContextWindowPercent = 100
	}
}

func (r fileConfig) toConfig() Config {
	r = r.withDefaults()
	syncInterval := parseDurationDefault(r.SkillsSyncInterval, 10*time.Minute)
	fsRoots := r.FSAllowedRoots
	if len(fsRoots) == 0 {
		fsRoots = []string{r.SkillsDir}
	}
	format := NormalizePromptFormat(r.LLMPromptFormat)
	return Config{
		ServerAddr:                       r.ServerAddr,
		TiDBDSN:                          r.TiDBDSN,
		LLMBaseURL:                       r.LLMBaseURL,
		LLMAPIKey:                        r.LLMAPIKey,
		LLMModel:                         r.LLMModel,
		LLMPromptFormat:                  format,
		LLMReasoningEnabled:              r.LLMReasoningEnabled,
		LLMReasoningEffort:               r.LLMReasoningEffort,
		LLMHTTPDebug:                     r.LLMHTTPDebug,
		LLMContextWindow:                 r.LLMContextWindow,
		LLMAutoCompactTokenLimit:         r.LLMAutoCompactTokenLimit,
		LLMEffectiveContextWindowPercent: r.LLMEffectiveContextWindowPercent,
		TelegramToken:                    r.TelegramToken,
		SkillsDir:                        r.SkillsDir,
		SkillsRepoAllowlist:              r.SkillsRepoAllowlist,
		SkillsSyncInterval:               syncInterval,
		BraveSearchAPIKey:                r.BraveSearchAPIKey,
		FSAllowedRoots:                   fsRoots,
		FSAllowedExecDirs:                r.FSAllowedExecDirs,
		ToolMaxTurns:                     r.ToolMaxTurns,
		Log:                              r.Log,
	}
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
