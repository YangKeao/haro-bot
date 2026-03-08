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

type MemoryConfig struct {
	Embedder MemoryEmbedderConfig
	Vector   MemoryVectorConfig
	Ingest   MemoryIngestConfig
	Retrieve MemoryRetrieveConfig
	Graph    MemoryGraphConfig
}

type MemoryEmbedderConfig struct {
	Provider   string
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
}

type MemoryVectorConfig struct {
	Distance string
}

type MemoryIngestConfig struct {
	RecentWindow    int
	MaxCandidates   int
	MinConfidence   float64
	MinImportance   int
	MatchTopK       int
	UpdateThreshold float64
	NoopThreshold   float64
}

type MemoryRetrieveConfig struct {
	TopK     int
	MinScore float64
}

type MemoryGraphConfig struct {
	Enabled  bool
	Provider string
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

	FSAllowedRoots []string

	ToolMaxTurns int

	Log    LogConfig
	Memory MemoryConfig
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
	AllowedRoots []string `toml:"allowed_roots"`
}

type toolConfig struct {
	MaxTurns int `toml:"max_turns"`
}

type memoryEmbedderConfig struct {
	Provider   string `toml:"provider"`
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	Model      string `toml:"model"`
	Dimensions int    `toml:"dimensions"`
}

type memoryVectorConfig struct {
	Distance string `toml:"distance"`
}

type memoryIngestConfig struct {
	RecentWindow    int     `toml:"recent_window"`
	MaxCandidates   int     `toml:"max_candidates"`
	MinConfidence   float64 `toml:"min_confidence"`
	MinImportance   int     `toml:"min_importance"`
	MatchTopK       int     `toml:"match_top_k"`
	UpdateThreshold float64 `toml:"update_threshold"`
	NoopThreshold   float64 `toml:"noop_threshold"`
}

type memoryRetrieveConfig struct {
	TopK     int     `toml:"top_k"`
	MinScore float64 `toml:"min_score"`
}

type memoryGraphConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
}

type memoryConfig struct {
	Embedder memoryEmbedderConfig `toml:"embedder"`
	Vector   memoryVectorConfig   `toml:"vector"`
	Ingest   memoryIngestConfig   `toml:"ingest"`
	Retrieve memoryRetrieveConfig `toml:"retrieve"`
	Graph    memoryGraphConfig    `toml:"graph"`
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
	Memory   memoryConfig   `toml:"memory"`
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
		Memory: memoryConfig{
			Embedder: memoryEmbedderConfig{
				Provider:   "openai",
				BaseURL:    "https://api.openai.com/v1",
				Model:      "text-embedding-3-small",
				Dimensions: 1536,
			},
			Vector: memoryVectorConfig{
				Distance: "cosine",
			},
			Ingest: memoryIngestConfig{
				RecentWindow:    20,
				MaxCandidates:   8,
				MinConfidence:   0.6,
				MinImportance:   1,
				MatchTopK:       5,
				UpdateThreshold: 0.85,
				NoopThreshold:   0.92,
			},
			Retrieve: memoryRetrieveConfig{
				TopK:     8,
				MinScore: 0.2,
			},
			Graph: memoryGraphConfig{
				Enabled:  false,
				Provider: "",
			},
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
	if strings.TrimSpace(r.Memory.Embedder.Provider) == "" {
		r.Memory.Embedder.Provider = def.Memory.Embedder.Provider
	}
	if strings.TrimSpace(r.Memory.Embedder.BaseURL) == "" {
		r.Memory.Embedder.BaseURL = def.Memory.Embedder.BaseURL
	}
	if strings.TrimSpace(r.Memory.Embedder.Model) == "" {
		r.Memory.Embedder.Model = def.Memory.Embedder.Model
	}
	if r.Memory.Embedder.Dimensions <= 0 {
		r.Memory.Embedder.Dimensions = def.Memory.Embedder.Dimensions
	}
	if strings.TrimSpace(r.Memory.Vector.Distance) == "" {
		r.Memory.Vector.Distance = def.Memory.Vector.Distance
	}
	if r.Memory.Ingest.RecentWindow <= 0 {
		r.Memory.Ingest.RecentWindow = def.Memory.Ingest.RecentWindow
	}
	if r.Memory.Ingest.MaxCandidates <= 0 {
		r.Memory.Ingest.MaxCandidates = def.Memory.Ingest.MaxCandidates
	}
	if r.Memory.Ingest.MinConfidence <= 0 {
		r.Memory.Ingest.MinConfidence = def.Memory.Ingest.MinConfidence
	}
	if r.Memory.Ingest.MinImportance <= 0 {
		r.Memory.Ingest.MinImportance = def.Memory.Ingest.MinImportance
	}
	if r.Memory.Ingest.MatchTopK <= 0 {
		r.Memory.Ingest.MatchTopK = def.Memory.Ingest.MatchTopK
	}
	if r.Memory.Ingest.UpdateThreshold <= 0 {
		r.Memory.Ingest.UpdateThreshold = def.Memory.Ingest.UpdateThreshold
	}
	if r.Memory.Ingest.NoopThreshold <= 0 {
		r.Memory.Ingest.NoopThreshold = def.Memory.Ingest.NoopThreshold
	}
	if r.Memory.Retrieve.TopK <= 0 {
		r.Memory.Retrieve.TopK = def.Memory.Retrieve.TopK
	}
	if r.Memory.Retrieve.MinScore < 0 {
		r.Memory.Retrieve.MinScore = def.Memory.Retrieve.MinScore
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
	r.Memory.Embedder.Provider = strings.ToLower(strings.TrimSpace(r.Memory.Embedder.Provider))
	if r.Memory.Embedder.Provider == "" {
		r.Memory.Embedder.Provider = "openai"
	}
	r.Memory.Vector.Distance = strings.ToLower(strings.TrimSpace(r.Memory.Vector.Distance))
	if r.Memory.Vector.Distance == "" {
		r.Memory.Vector.Distance = "cosine"
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
		ToolMaxTurns:                     r.Tool.MaxTurns,
		Log:                              r.Log,
		Memory: MemoryConfig{
			Embedder: MemoryEmbedderConfig{
				Provider:   r.Memory.Embedder.Provider,
				BaseURL:    r.Memory.Embedder.BaseURL,
				APIKey:     r.Memory.Embedder.APIKey,
				Model:      r.Memory.Embedder.Model,
				Dimensions: r.Memory.Embedder.Dimensions,
			},
			Vector: MemoryVectorConfig{
				Distance: r.Memory.Vector.Distance,
			},
			Ingest: MemoryIngestConfig{
				RecentWindow:    r.Memory.Ingest.RecentWindow,
				MaxCandidates:   r.Memory.Ingest.MaxCandidates,
				MinConfidence:   r.Memory.Ingest.MinConfidence,
				MinImportance:   r.Memory.Ingest.MinImportance,
				MatchTopK:       r.Memory.Ingest.MatchTopK,
				UpdateThreshold: r.Memory.Ingest.UpdateThreshold,
				NoopThreshold:   r.Memory.Ingest.NoopThreshold,
			},
			Retrieve: MemoryRetrieveConfig{
				TopK:     r.Memory.Retrieve.TopK,
				MinScore: r.Memory.Retrieve.MinScore,
			},
			Graph: MemoryGraphConfig{
				Enabled:  r.Memory.Graph.Enabled,
				Provider: r.Memory.Graph.Provider,
			},
		},
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
