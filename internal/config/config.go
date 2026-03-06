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

type serverConfig struct {
	Addr string `toml:"addr"`
}

type dbConfig struct {
	TiDBDSN string `toml:"tidb_dsn"`
}

type llmConfig struct {
	BaseURL      string `toml:"base_url"`
	APIKey       string `toml:"api_key"`
	Model        string `toml:"model"`
	PromptFormat string `toml:"prompt_format"`

	ReasoningEnabled              bool   `toml:"reasoning_enabled"`
	ReasoningEffort               string `toml:"reasoning_effort"`
	HTTPDebug                     bool   `toml:"http_debug"`
	ContextWindow                 int    `toml:"context_window"`
	AutoCompactTokenLimit         int    `toml:"auto_compact_token_limit"`
	EffectiveContextWindowPercent int    `toml:"effective_context_window_percent"`
}

type telegramConfig struct {
	Token string `toml:"token"`
}

type skillsConfig struct {
	Dir           string   `toml:"dir"`
	RepoAllowlist []string `toml:"repo_allowlist"`
	SyncInterval  string   `toml:"sync_interval"`
}

type braveConfig struct {
	SearchAPIKey string `toml:"search_api_key"`
}

type fsConfig struct {
	AllowedRoots    []string `toml:"allowed_roots"`
	AllowedExecDirs []string `toml:"allowed_exec_dirs"`
}

type toolConfig struct {
	MaxTurns int `toml:"max_turns"`
}

type fileConfig struct {
	Server   serverConfig   `toml:"server"`
	DB       dbConfig       `toml:"db"`
	LLM      llmConfig      `toml:"llm"`
	Telegram telegramConfig `toml:"telegram"`
	Skills   skillsConfig   `toml:"skills"`
	Brave    braveConfig    `toml:"brave"`
	FS       fsConfig       `toml:"fs"`
	Tool     toolConfig     `toml:"tool"`
	Log      LogConfig      `toml:"log"`
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
		Server: serverConfig{
			Addr: ":8080",
		},
		DB: dbConfig{
			TiDBDSN: "root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true",
		},
		LLM: llmConfig{
			BaseURL:                       "https://api.openai.com/v1",
			Model:                         "gpt-4o-mini",
			PromptFormat:                  string(PromptFormatOpenAI),
			EffectiveContextWindowPercent: 95,
		},
		Skills: skillsConfig{
			Dir:          skillsDir,
			SyncInterval: "10m",
		},
		FS: fsConfig{
			AllowedRoots: []string{skillsDir},
		},
		Tool: toolConfig{
			MaxTurns: 1024,
		},
		Log: LogConfig{
			Level:       "info",
			Development: false,
			Encoding:    "json",
		},
	}
}

func (r fileConfig) withDefaults() fileConfig {
	def := defaultFileConfig()
	if strings.TrimSpace(r.Server.Addr) == "" {
		r.Server.Addr = def.Server.Addr
	}
	if strings.TrimSpace(r.DB.TiDBDSN) == "" {
		r.DB.TiDBDSN = def.DB.TiDBDSN
	}
	if strings.TrimSpace(r.LLM.BaseURL) == "" {
		r.LLM.BaseURL = def.LLM.BaseURL
	}
	if strings.TrimSpace(r.LLM.Model) == "" {
		r.LLM.Model = def.LLM.Model
	}
	if strings.TrimSpace(r.LLM.PromptFormat) == "" {
		r.LLM.PromptFormat = def.LLM.PromptFormat
	}
	if r.LLM.EffectiveContextWindowPercent <= 0 {
		r.LLM.EffectiveContextWindowPercent = def.LLM.EffectiveContextWindowPercent
	}
	if strings.TrimSpace(r.Skills.Dir) == "" {
		r.Skills.Dir = def.Skills.Dir
	}
	if strings.TrimSpace(r.Skills.SyncInterval) == "" {
		r.Skills.SyncInterval = def.Skills.SyncInterval
	}
	if len(r.FS.AllowedRoots) == 0 {
		r.FS.AllowedRoots = []string{r.Skills.Dir}
	}
	if r.Tool.MaxTurns <= 0 {
		r.Tool.MaxTurns = def.Tool.MaxTurns
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
	r.LLM.PromptFormat = string(NormalizePromptFormat(r.LLM.PromptFormat))
	r.LLM.ReasoningEffort = strings.ToLower(strings.TrimSpace(r.LLM.ReasoningEffort))
	if r.LLM.EffectiveContextWindowPercent <= 0 {
		r.LLM.EffectiveContextWindowPercent = 95
	}
	if r.LLM.EffectiveContextWindowPercent > 100 {
		r.LLM.EffectiveContextWindowPercent = 100
	}
}

func (r fileConfig) toConfig() Config {
	r = r.withDefaults()
	syncInterval := parseDurationDefault(r.Skills.SyncInterval, 10*time.Minute)
	fsRoots := r.FS.AllowedRoots
	if len(fsRoots) == 0 {
		fsRoots = []string{r.Skills.Dir}
	}
	format := NormalizePromptFormat(r.LLM.PromptFormat)
	return Config{
		ServerAddr:                       r.Server.Addr,
		TiDBDSN:                          r.DB.TiDBDSN,
		LLMBaseURL:                       r.LLM.BaseURL,
		LLMAPIKey:                        r.LLM.APIKey,
		LLMModel:                         r.LLM.Model,
		LLMPromptFormat:                  format,
		LLMReasoningEnabled:              r.LLM.ReasoningEnabled,
		LLMReasoningEffort:               r.LLM.ReasoningEffort,
		LLMHTTPDebug:                     r.LLM.HTTPDebug,
		LLMContextWindow:                 r.LLM.ContextWindow,
		LLMAutoCompactTokenLimit:         r.LLM.AutoCompactTokenLimit,
		LLMEffectiveContextWindowPercent: r.LLM.EffectiveContextWindowPercent,
		TelegramToken:                    r.Telegram.Token,
		SkillsDir:                        r.Skills.Dir,
		SkillsRepoAllowlist:              r.Skills.RepoAllowlist,
		SkillsSyncInterval:               syncInterval,
		BraveSearchAPIKey:                r.Brave.SearchAPIKey,
		FSAllowedRoots:                   fsRoots,
		FSAllowedExecDirs:                r.FS.AllowedExecDirs,
		ToolMaxTurns:                     r.Tool.MaxTurns,
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
