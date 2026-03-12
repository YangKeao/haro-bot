package config

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// LogConfig holds logging configuration.
type LogConfig struct {
	Level       string `toml:"level"`
	Development bool   `toml:"development"`
	Encoding    string `toml:"encoding"`
}

// MemoryEmbedderConfig configures the embedding provider.
type MemoryEmbedderConfig struct {
	Provider   string `toml:"provider"`
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	Model      string `toml:"model"`
	Dimensions int    `toml:"dimensions"`
}

// MemoryVectorConfig configures vector search.
type MemoryVectorConfig struct {
	Distance string `toml:"distance"`
}

// MemoryIngestConfig configures memory ingestion.
type MemoryIngestConfig struct {
	RecentWindow    int     `toml:"recent_window"`
	MaxCandidates   int     `toml:"max_candidates"`
	MinConfidence   float64 `toml:"min_confidence"`
	MinImportance   int     `toml:"min_importance"`
	MatchTopK       int     `toml:"match_top_k"`
	UpdateThreshold float64 `toml:"update_threshold"`
	NoopThreshold   float64 `toml:"noop_threshold"`
}

// MemoryRetrieveConfig configures memory retrieval.
type MemoryRetrieveConfig struct {
	TopK     int     `toml:"top_k"`
	MinScore float64 `toml:"min_score"`
}

// MemoryGraphConfig configures graph-based memory.
type MemoryGraphConfig struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
}

// MemoryConfig holds all memory subsystem configuration.
type MemoryConfig struct {
	Embedder MemoryEmbedderConfig `toml:"embedder"`
	Vector   MemoryVectorConfig   `toml:"vector"`
	Ingest   MemoryIngestConfig   `toml:"ingest"`
	Retrieve MemoryRetrieveConfig `toml:"retrieve"`
	Graph    MemoryGraphConfig    `toml:"graph"`
}

// Config is the runtime configuration loaded from file.
type Config struct {
	ServerAddr                       string
	TiDBDSN                          string
	LLMBaseURL                       string
	LLMAPIKey                        string
	LLMModel                         string
	LLMPromptFormat                  PromptFormat
	LLMReasoningEnabled              bool
	LLMReasoningEffort               string
	LLMHTTPDebug                     bool
	LLMContextWindow                 int
	LLMAutoCompactTokenLimit         int
	LLMEffectiveContextWindowPercent int
	TelegramToken                    string
	SkillsDir                        string
	SkillsRepoAllowlist              []string
	SkillsSyncInterval               time.Duration
	BraveSearchAPIKey                string
	ToolMaxTurns                     int
	Log                              LogConfig
	Memory                           MemoryConfig
}

// fileConfig mirrors the TOML structure for deserialization.
type fileConfig struct {
	Server struct {
		Addr string `toml:"addr"`
	} `toml:"server"`
	DB struct {
		TiDBDSN string `toml:"tidb_dsn"`
	} `toml:"db"`
	LLM struct {
		BaseURL                       string `toml:"base_url"`
		APIKey                        string `toml:"api_key"`
		Model                         string `toml:"model"`
		PromptFormat                  string `toml:"prompt_format"`
		ReasoningEnabled              bool   `toml:"reasoning_enabled"`
		ReasoningEffort               string `toml:"reasoning_effort"`
		HTTPDebug                     bool   `toml:"http_debug"`
		ContextWindow                 int    `toml:"context_window"`
		AutoCompactTokenLimit         int    `toml:"auto_compact_token_limit"`
		EffectiveContextWindowPercent int    `toml:"effective_context_window_percent"`
	} `toml:"llm"`
	Telegram struct {
		Token string `toml:"token"`
	} `toml:"telegram"`
	Skills struct {
		Dir           string   `toml:"dir"`
		RepoAllowlist []string `toml:"repo_allowlist"`
		SyncInterval  string   `toml:"sync_interval"`
	} `toml:"skills"`
	Brave struct {
		SearchAPIKey string `toml:"search_api_key"`
	} `toml:"brave"`
	Tool struct {
		MaxTurns int `toml:"max_turns"`
	} `toml:"tool"`
	Log    LogConfig    `toml:"log"`
	Memory MemoryConfig `toml:"memory"`
}

// LoadFromFile reads configuration from a TOML file.
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
		Server: struct {
			Addr string `toml:"addr"`
		}{Addr: ":8080"},
		DB: struct {
			TiDBDSN string `toml:"tidb_dsn"`
		}{TiDBDSN: "root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true"},
		LLM: struct {
			BaseURL                       string `toml:"base_url"`
			APIKey                        string `toml:"api_key"`
			Model                         string `toml:"model"`
			PromptFormat                  string `toml:"prompt_format"`
			ReasoningEnabled              bool   `toml:"reasoning_enabled"`
			ReasoningEffort               string `toml:"reasoning_effort"`
			HTTPDebug                     bool   `toml:"http_debug"`
			ContextWindow                 int    `toml:"context_window"`
			AutoCompactTokenLimit         int    `toml:"auto_compact_token_limit"`
			EffectiveContextWindowPercent int    `toml:"effective_context_window_percent"`
		}{
			BaseURL:                       "https://api.openai.com/v1",
			Model:                         "gpt-4o-mini",
			PromptFormat:                  string(PromptFormatOpenAI),
			EffectiveContextWindowPercent: 95,
		},
		Skills: struct {
			Dir           string   `toml:"dir"`
			RepoAllowlist []string `toml:"repo_allowlist"`
			SyncInterval  string   `toml:"sync_interval"`
		}{
			Dir:          skillsDir,
			SyncInterval: "10m",
		},
		Tool: struct {
			MaxTurns int `toml:"max_turns"`
		}{
			MaxTurns: 1024,
		},
		Log: LogConfig{
			Level:    "info",
			Encoding: "json",
		},
		Memory: MemoryConfig{
			Embedder: MemoryEmbedderConfig{
				Provider:   "openai",
				BaseURL:    "https://api.openai.com/v1",
				Model:      "text-embedding-3-small",
				Dimensions: 1536,
			},
			Vector: MemoryVectorConfig{
				Distance: "cosine",
			},
			Ingest: MemoryIngestConfig{
				RecentWindow:    20,
				MaxCandidates:   8,
				MinConfidence:   0.6,
				MinImportance:   1,
				MatchTopK:       5,
				UpdateThreshold: 0.85,
				NoopThreshold:   0.92,
			},
			Retrieve: MemoryRetrieveConfig{
				TopK:     8,
				MinScore: 0.2,
			},
			Graph: MemoryGraphConfig{
				Enabled: false,
			},
		},
	}
}

// strDefault returns def if v is blank.
func strDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// intDefault returns def if v <= 0.
func intDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// floatDefault returns def if v <= 0.
func floatDefault(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

func (r fileConfig) withDefaults() fileConfig {
	def := defaultFileConfig()
	r.Server.Addr = strDefault(r.Server.Addr, def.Server.Addr)
	r.DB.TiDBDSN = strDefault(r.DB.TiDBDSN, def.DB.TiDBDSN)
	r.LLM.BaseURL = strDefault(r.LLM.BaseURL, def.LLM.BaseURL)
	r.LLM.Model = strDefault(r.LLM.Model, def.LLM.Model)
	r.LLM.PromptFormat = strDefault(r.LLM.PromptFormat, def.LLM.PromptFormat)
	r.LLM.EffectiveContextWindowPercent = intDefault(r.LLM.EffectiveContextWindowPercent, def.LLM.EffectiveContextWindowPercent)
	r.Skills.Dir = strDefault(r.Skills.Dir, def.Skills.Dir)
	r.Skills.SyncInterval = strDefault(r.Skills.SyncInterval, def.Skills.SyncInterval)
	r.Tool.MaxTurns = intDefault(r.Tool.MaxTurns, def.Tool.MaxTurns)
	r.Log.Level = strDefault(r.Log.Level, def.Log.Level)
	r.Log.Encoding = strDefault(r.Log.Encoding, def.Log.Encoding)
	r.Memory.Embedder.Provider = strDefault(r.Memory.Embedder.Provider, def.Memory.Embedder.Provider)
	r.Memory.Embedder.BaseURL = strDefault(r.Memory.Embedder.BaseURL, def.Memory.Embedder.BaseURL)
	r.Memory.Embedder.Model = strDefault(r.Memory.Embedder.Model, def.Memory.Embedder.Model)
	r.Memory.Embedder.Dimensions = intDefault(r.Memory.Embedder.Dimensions, def.Memory.Embedder.Dimensions)
	r.Memory.Vector.Distance = strDefault(r.Memory.Vector.Distance, def.Memory.Vector.Distance)
	r.Memory.Ingest.RecentWindow = intDefault(r.Memory.Ingest.RecentWindow, def.Memory.Ingest.RecentWindow)
	r.Memory.Ingest.MaxCandidates = intDefault(r.Memory.Ingest.MaxCandidates, def.Memory.Ingest.MaxCandidates)
	r.Memory.Ingest.MinConfidence = floatDefault(r.Memory.Ingest.MinConfidence, def.Memory.Ingest.MinConfidence)
	r.Memory.Ingest.MinImportance = intDefault(r.Memory.Ingest.MinImportance, def.Memory.Ingest.MinImportance)
	r.Memory.Ingest.MatchTopK = intDefault(r.Memory.Ingest.MatchTopK, def.Memory.Ingest.MatchTopK)
	r.Memory.Ingest.UpdateThreshold = floatDefault(r.Memory.Ingest.UpdateThreshold, def.Memory.Ingest.UpdateThreshold)
	r.Memory.Ingest.NoopThreshold = floatDefault(r.Memory.Ingest.NoopThreshold, def.Memory.Ingest.NoopThreshold)
	r.Memory.Retrieve.TopK = intDefault(r.Memory.Retrieve.TopK, def.Memory.Retrieve.TopK)
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
	return Config{
		ServerAddr:                       r.Server.Addr,
		TiDBDSN:                          r.DB.TiDBDSN,
		LLMBaseURL:                       r.LLM.BaseURL,
		LLMAPIKey:                        r.LLM.APIKey,
		LLMModel:                         r.LLM.Model,
		LLMPromptFormat:                  NormalizePromptFormat(r.LLM.PromptFormat),
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
		ToolMaxTurns:                     r.Tool.MaxTurns,
		Log:                              r.Log,
		Memory:                           r.Memory,
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
