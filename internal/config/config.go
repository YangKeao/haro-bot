package config

import (
	"errors"
	"os"
	"strconv"
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
	PublicURL     string
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

type SandboxConfig struct {
	Enabled                        bool
	Namespace                      string
	RuntimeClass                   string
	DefaultImage                   string
	HelperImage                    string
	StorageClass                   string
	KubeAPIURL                     string
	KubeTokenFile                  string
	KubeCAFile                     string
	EncryptionKey                  string
	DefaultCPULimitMillis          int
	DefaultMemoryLimitMiB          int
	DefaultEphemeralStorageMiB     int
	DefaultWorkspaceStorageMiB     int
	MaxCPULimitMillis              int
	MaxMemoryLimitMiB              int
	MaxEphemeralStorageMiB         int
	MaxWorkspaceStorageMiB         int
	MaxRunning                     int
	RuntimePort                    int
	BackgroundTerminalMaxTimeoutMS int
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
	SecretKey           string
	Log                 LogConfig
	Web                 WebConfig
	Sandbox             SandboxConfig
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
		PublicURL     string `toml:"public_url"`
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
	Sandbox struct {
		Enabled                        bool   `toml:"enabled"`
		Namespace                      string `toml:"namespace"`
		RuntimeClass                   string `toml:"runtime_class"`
		DefaultImage                   string `toml:"default_image"`
		HelperImage                    string `toml:"helper_image"`
		StorageClass                   string `toml:"storage_class"`
		KubeAPIURL                     string `toml:"kube_api_url"`
		KubeTokenFile                  string `toml:"kube_token_file"`
		KubeCAFile                     string `toml:"kube_ca_file"`
		DefaultCPULimitMillis          int    `toml:"default_cpu_limit_millis"`
		DefaultMemoryLimitMiB          int    `toml:"default_memory_limit_mib"`
		DefaultEphemeralStorageMiB     int    `toml:"default_ephemeral_storage_mib"`
		DefaultWorkspaceStorageMiB     int    `toml:"default_workspace_storage_mib"`
		MaxCPULimitMillis              int    `toml:"max_cpu_limit_millis"`
		MaxMemoryLimitMiB              int    `toml:"max_memory_limit_mib"`
		MaxEphemeralStorageMiB         int    `toml:"max_ephemeral_storage_mib"`
		MaxWorkspaceStorageMiB         int    `toml:"max_workspace_storage_mib"`
		MaxRunning                     int    `toml:"max_running"`
		RuntimePort                    int    `toml:"runtime_port"`
		BackgroundTerminalMaxTimeoutMS int    `toml:"background_terminal_max_timeout_ms"`
	} `toml:"sandbox"`
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
	rec.Sandbox.Namespace = "haro-sandboxes"
	rec.Sandbox.RuntimeClass = "gvisor"
	rec.Sandbox.DefaultImage = "ghcr.io/yangkeao/haro-bot-sandbox:latest"
	rec.Sandbox.HelperImage = rec.Sandbox.DefaultImage
	rec.Sandbox.KubeTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	rec.Sandbox.KubeCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	rec.Sandbox.DefaultCPULimitMillis = 2000
	rec.Sandbox.DefaultMemoryLimitMiB = 2048
	rec.Sandbox.DefaultEphemeralStorageMiB = 10240
	rec.Sandbox.DefaultWorkspaceStorageMiB = 10240
	rec.Sandbox.MaxCPULimitMillis = 4000
	rec.Sandbox.MaxMemoryLimitMiB = 8192
	rec.Sandbox.MaxEphemeralStorageMiB = 51200
	rec.Sandbox.MaxWorkspaceStorageMiB = 102400
	rec.Sandbox.MaxRunning = 10
	rec.Sandbox.RuntimePort = 8888
	rec.Sandbox.BackgroundTerminalMaxTimeoutMS = 300000
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
	r.Sandbox.Namespace = strDefault(r.Sandbox.Namespace, def.Sandbox.Namespace)
	r.Sandbox.RuntimeClass = strDefault(r.Sandbox.RuntimeClass, def.Sandbox.RuntimeClass)
	r.Sandbox.DefaultImage = strDefault(r.Sandbox.DefaultImage, def.Sandbox.DefaultImage)
	r.Sandbox.HelperImage = strDefault(r.Sandbox.HelperImage, def.Sandbox.HelperImage)
	r.Sandbox.KubeTokenFile = strDefault(r.Sandbox.KubeTokenFile, def.Sandbox.KubeTokenFile)
	r.Sandbox.KubeCAFile = strDefault(r.Sandbox.KubeCAFile, def.Sandbox.KubeCAFile)
	r.Sandbox.DefaultCPULimitMillis = intDefault(r.Sandbox.DefaultCPULimitMillis, def.Sandbox.DefaultCPULimitMillis)
	r.Sandbox.DefaultMemoryLimitMiB = intDefault(r.Sandbox.DefaultMemoryLimitMiB, def.Sandbox.DefaultMemoryLimitMiB)
	r.Sandbox.DefaultEphemeralStorageMiB = intDefault(r.Sandbox.DefaultEphemeralStorageMiB, def.Sandbox.DefaultEphemeralStorageMiB)
	r.Sandbox.DefaultWorkspaceStorageMiB = intDefault(r.Sandbox.DefaultWorkspaceStorageMiB, def.Sandbox.DefaultWorkspaceStorageMiB)
	r.Sandbox.MaxCPULimitMillis = intDefault(r.Sandbox.MaxCPULimitMillis, def.Sandbox.MaxCPULimitMillis)
	r.Sandbox.MaxMemoryLimitMiB = intDefault(r.Sandbox.MaxMemoryLimitMiB, def.Sandbox.MaxMemoryLimitMiB)
	r.Sandbox.MaxEphemeralStorageMiB = intDefault(r.Sandbox.MaxEphemeralStorageMiB, def.Sandbox.MaxEphemeralStorageMiB)
	r.Sandbox.MaxWorkspaceStorageMiB = intDefault(r.Sandbox.MaxWorkspaceStorageMiB, def.Sandbox.MaxWorkspaceStorageMiB)
	r.Sandbox.MaxRunning = intDefault(r.Sandbox.MaxRunning, def.Sandbox.MaxRunning)
	r.Sandbox.RuntimePort = intDefault(r.Sandbox.RuntimePort, def.Sandbox.RuntimePort)
	r.Sandbox.BackgroundTerminalMaxTimeoutMS = intDefault(r.Sandbox.BackgroundTerminalMaxTimeoutMS, def.Sandbox.BackgroundTerminalMaxTimeoutMS)
	return r
}

func (r fileConfig) toConfig() Config {
	r = r.withDefaults()
	sandboxEnabled := r.Sandbox.Enabled
	if value := strings.TrimSpace(os.Getenv("HARO_SANDBOX_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			sandboxEnabled = parsed
		}
	}
	webUseSSL := envBool("HARO_WEB_OBJECT_STORAGE_USE_SSL", r.Web.ObjectStorage.UseSSL)
	webForcePathStyle := envBool("HARO_WEB_OBJECT_STORAGE_FORCE_PATH_STYLE", r.Web.ObjectStorage.ForcePathStyle)
	syncInterval := parseDurationDefault(r.Skills.SyncInterval, 10*time.Minute)
	secretKey := strings.TrimSpace(os.Getenv("HARO_SECRET_KEY"))
	if secretKey == "" {
		secretKey = strings.TrimSpace(os.Getenv("HARO_SANDBOX_SECRET_KEY"))
	}
	return Config{
		ServerAddr:          strDefault(os.Getenv("HARO_SERVER_ADDR"), r.Server.Addr),
		TiDBDSN:             r.DB.TiDBDSN,
		LLMHTTPDebug:        r.Log.LLMHTTPDebug,
		TelegramToken:       r.Telegram.Token,
		SkillsDir:           r.Skills.Dir,
		SkillsRepoAllowlist: r.Skills.RepoAllowlist,
		SkillsSyncInterval:  syncInterval,
		BraveSearchAPIKey:   r.Brave.SearchAPIKey,
		ToolMaxTurns:        r.Tool.MaxTurns,
		SecretKey:           secretKey,
		Log:                 r.Log,
		Web: WebConfig{
			AccessToken:  strings.TrimSpace(strDefault(os.Getenv("HARO_WEB_ACCESS_TOKEN"), r.Web.AccessToken)),
			CookieSecure: r.Web.CookieSecure,
			AssetsDir:    r.Web.AssetsDir,
			PublicURL:    strings.TrimRight(strings.TrimSpace(strDefault(os.Getenv("HARO_WEB_PUBLIC_URL"), r.Web.PublicURL)), "/"),
			ObjectStorage: ObjectStorageConfig{
				Endpoint:       strings.TrimSpace(strDefault(os.Getenv("HARO_WEB_OBJECT_STORAGE_ENDPOINT"), r.Web.ObjectStorage.Endpoint)),
				Region:         strDefault(os.Getenv("HARO_WEB_OBJECT_STORAGE_REGION"), r.Web.ObjectStorage.Region),
				Bucket:         strDefault(os.Getenv("HARO_WEB_OBJECT_STORAGE_BUCKET"), r.Web.ObjectStorage.Bucket),
				AccessKey:      strings.TrimSpace(strDefault(os.Getenv("HARO_WEB_OBJECT_STORAGE_ACCESS_KEY"), r.Web.ObjectStorage.AccessKey)),
				SecretKey:      strDefault(os.Getenv("HARO_WEB_OBJECT_STORAGE_SECRET_KEY"), r.Web.ObjectStorage.SecretKey),
				UseSSL:         webUseSSL,
				ForcePathStyle: webForcePathStyle,
			},
		},
		Sandbox: SandboxConfig{
			Enabled:      sandboxEnabled,
			Namespace:    strDefault(os.Getenv("HARO_SANDBOX_NAMESPACE"), r.Sandbox.Namespace),
			RuntimeClass: strDefault(os.Getenv("HARO_SANDBOX_RUNTIME_CLASS"), r.Sandbox.RuntimeClass),
			DefaultImage: strDefault(os.Getenv("HARO_SANDBOX_DEFAULT_IMAGE"), r.Sandbox.DefaultImage),
			HelperImage:  strDefault(os.Getenv("HARO_SANDBOX_HELPER_IMAGE"), r.Sandbox.HelperImage),
			StorageClass: strings.TrimSpace(strDefault(os.Getenv("HARO_SANDBOX_STORAGE_CLASS"), r.Sandbox.StorageClass)),
			KubeAPIURL:   strings.TrimSpace(r.Sandbox.KubeAPIURL), KubeTokenFile: r.Sandbox.KubeTokenFile, KubeCAFile: r.Sandbox.KubeCAFile,
			EncryptionKey:         secretKey,
			DefaultCPULimitMillis: r.Sandbox.DefaultCPULimitMillis, DefaultMemoryLimitMiB: r.Sandbox.DefaultMemoryLimitMiB,
			DefaultEphemeralStorageMiB: r.Sandbox.DefaultEphemeralStorageMiB, DefaultWorkspaceStorageMiB: r.Sandbox.DefaultWorkspaceStorageMiB,
			MaxCPULimitMillis: r.Sandbox.MaxCPULimitMillis, MaxMemoryLimitMiB: r.Sandbox.MaxMemoryLimitMiB,
			MaxEphemeralStorageMiB: r.Sandbox.MaxEphemeralStorageMiB, MaxWorkspaceStorageMiB: r.Sandbox.MaxWorkspaceStorageMiB,
			MaxRunning: r.Sandbox.MaxRunning, RuntimePort: r.Sandbox.RuntimePort,
			BackgroundTerminalMaxTimeoutMS: envInt("HARO_SANDBOX_BACKGROUND_TERMINAL_MAX_TIMEOUT_MS", r.Sandbox.BackgroundTerminalMaxTimeoutMS),
		},
	}
}

func envBool(name string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return def
	}
	return parsed
}

func envInt(name string, def int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
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
