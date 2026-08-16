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
	Level        string `toml:"level"`
	Development  bool   `toml:"development"`
	Encoding     string `toml:"encoding"`
	LLMHTTPDebug bool   `toml:"llm_http_debug"`
}

type WebConfig struct {
	AccessToken   string
	CookieSecure  bool
	AssetsDir     string
	ObjectStorage ObjectStorageConfig
}

type ObjectStorageConfig struct {
	Endpoint       string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	ForcePathStyle bool
}

// Config is the runtime configuration loaded from file.
type Config struct {
	ServerAddr          string
	TiDBDSN             string
	LLMHTTPDebug        bool
	TelegramToken       string
	SkillsDir           string
	SkillsRepoAllowlist []string
	SkillsSyncInterval  time.Duration
	BraveSearchAPIKey   string
	ToolMaxTurns        int
	Log                 LogConfig
	Web                 WebConfig
}

// fileConfig mirrors the TOML structure for deserialization.
type fileConfig struct {
	Server struct {
		Addr string `toml:"addr"`
	} `toml:"server"`
	DB struct {
		TiDBDSN string `toml:"tidb_dsn"`
	} `toml:"db"`
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
	Log LogConfig `toml:"log"`
	Web struct {
		AccessToken   string `toml:"access_token"`
		CookieSecure  bool   `toml:"cookie_secure"`
		AssetsDir     string `toml:"assets_dir"`
		ObjectStorage struct {
			Endpoint       string `toml:"endpoint"`
			Region         string `toml:"region"`
			Bucket         string `toml:"bucket"`
			AccessKey      string `toml:"access_key"`
			SecretKey      string `toml:"secret_key"`
			UseSSL         bool   `toml:"use_ssl"`
			ForcePathStyle bool   `toml:"force_path_style"`
		} `toml:"object_storage"`
	} `toml:"web"`
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
	return rec.toConfig(), nil
}

func defaultFileConfig() fileConfig {
	skillsDir := "./skills"
	rec := fileConfig{
		Server: struct {
			Addr string `toml:"addr"`
		}{Addr: ":8080"},
		DB: struct {
			TiDBDSN string `toml:"tidb_dsn"`
		}{TiDBDSN: "root:@tcp(127.0.0.1:4000)/haro_bot?parseTime=true"},
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
	}
	rec.Web.AssetsDir = "./web/dist"
	rec.Web.ObjectStorage.Region = "us-east-1"
	rec.Web.ObjectStorage.Bucket = "haro-bot"
	return rec
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

func (r fileConfig) withDefaults() fileConfig {
	def := defaultFileConfig()
	r.Server.Addr = strDefault(r.Server.Addr, def.Server.Addr)
	r.DB.TiDBDSN = strDefault(r.DB.TiDBDSN, def.DB.TiDBDSN)
	r.Skills.Dir = strDefault(r.Skills.Dir, def.Skills.Dir)
	r.Skills.SyncInterval = strDefault(r.Skills.SyncInterval, def.Skills.SyncInterval)
	r.Tool.MaxTurns = intDefault(r.Tool.MaxTurns, def.Tool.MaxTurns)
	r.Log.Level = strDefault(r.Log.Level, def.Log.Level)
	r.Log.Encoding = strDefault(r.Log.Encoding, def.Log.Encoding)
	r.Web.AssetsDir = strDefault(r.Web.AssetsDir, def.Web.AssetsDir)
	r.Web.ObjectStorage.Region = strDefault(r.Web.ObjectStorage.Region, def.Web.ObjectStorage.Region)
	r.Web.ObjectStorage.Bucket = strDefault(r.Web.ObjectStorage.Bucket, def.Web.ObjectStorage.Bucket)
	return r
}

func (r fileConfig) toConfig() Config {
	r = r.withDefaults()
	syncInterval := parseDurationDefault(r.Skills.SyncInterval, 10*time.Minute)
	return Config{
		ServerAddr:          r.Server.Addr,
		TiDBDSN:             r.DB.TiDBDSN,
		LLMHTTPDebug:        r.Log.LLMHTTPDebug,
		TelegramToken:       r.Telegram.Token,
		SkillsDir:           r.Skills.Dir,
		SkillsRepoAllowlist: r.Skills.RepoAllowlist,
		SkillsSyncInterval:  syncInterval,
		BraveSearchAPIKey:   r.Brave.SearchAPIKey,
		ToolMaxTurns:        r.Tool.MaxTurns,
		Log:                 r.Log,
		Web: WebConfig{
			AccessToken:  strings.TrimSpace(r.Web.AccessToken),
			CookieSecure: r.Web.CookieSecure,
			AssetsDir:    r.Web.AssetsDir,
			ObjectStorage: ObjectStorageConfig{
				Endpoint:       strings.TrimSpace(r.Web.ObjectStorage.Endpoint),
				Region:         r.Web.ObjectStorage.Region,
				Bucket:         r.Web.ObjectStorage.Bucket,
				AccessKey:      strings.TrimSpace(r.Web.ObjectStorage.AccessKey),
				SecretKey:      r.Web.ObjectStorage.SecretKey,
				UseSSL:         r.Web.ObjectStorage.UseSSL,
				ForcePathStyle: r.Web.ObjectStorage.ForcePathStyle,
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
