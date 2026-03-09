package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/fork"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/server"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	unrestricted := flag.Bool("unrestricted", false, "skip path restrictions and symlink checks (audit logging still enabled)")
	flag.Parse()

	bootLogger, _ := zap.NewProduction()
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		bootLogger.Fatal("config load failed", zap.String("path", *configPath), zap.Error(err))
	}

	logger, err := logging.Init(logging.Config{
		Level:       cfg.Log.Level,
		Development: cfg.Log.Development,
		Encoding:    cfg.Log.Encoding,
	})
	if err != nil {
		logging.Set(bootLogger)
		logger = bootLogger
		logger.Warn("invalid log config, using production defaults", zap.Error(err))
	}
	defer func() { _ = logger.Sync() }()

	app := fx.New(
		// Supply config and flags
		fx.Supply(cfg),
		fx.Provide(func() *bool { return unrestricted }),

		// Core providers
		fx.Provide(
			// Database
			NewDB,
			// Stores
			NewMemoryStore,
			NewSkillsStore,
			NewAuditStore,
			// Managers
			NewSkillsManager,
			NewGuidelinesManager,
			// LLM
			NewLLMClient,
			NewContextConfig,
			NewReasoningConfig,
			NewMemoryEngine,
			// Tools
			NewFS,
			NewBrowserManager,
			NewExecManager,
			NewToolRegistry,
			// Named values for agent.Params
			fx.Annotate(
				func(cfg *config.Config) string { return cfg.LLMModel },
				fx.ResultTags(`name:"llm_model"`),
			),
			fx.Annotate(
				func(cfg *config.Config) string { return string(cfg.LLMPromptFormat) },
				fx.ResultTags(`name:"prompt_format"`),
			),
			fx.Annotate(
				func(fs *tools.FS) string { return fs.DefaultBase() },
				fx.ResultTags(`name:"default_base_dir"`),
			),
			fx.Annotate(
				func(cfg *config.Config) int { return cfg.ToolMaxTurns },
				fx.ResultTags(`name:"max_tool_turns"`),
			),
		),

		// Agent module
		agent.Module,

		// Fork and server
		fx.Provide(NewForkManager, NewServer),

		// Lifecycle
		fx.Invoke(RunApp),
	)

	app.Run()
}

// NewDB creates database connection with lifecycle management
func NewDB(lc fx.Lifecycle, cfg *config.Config, log *zap.Logger) *gorm.DB {
	conn, err := db.Open(cfg.TiDBDSN)
	if err != nil {
		log.Fatal("db open failed", zap.Error(err))
	}
	if err := db.ApplyMigrations(conn, cfg.Memory); err != nil {
		log.Fatal("db migrations failed", zap.Error(err))
	}
	lc.Append(fx.StopHook(func() error {
		sqlDB, _ := conn.DB()
		return sqlDB.Close()
	}))
	return conn
}

func NewMemoryStore(dbConn *gorm.DB) memory.StoreAPI {
	return memory.NewStore(dbConn)
}

func NewSkillsStore(dbConn *gorm.DB) *skills.Store {
	return skills.NewStore(dbConn)
}

func NewAuditStore(dbConn *gorm.DB) *tools.AuditStore {
	return tools.NewAuditStore(dbConn)
}

func NewSkillsManager(store *skills.Store, cfg *config.Config) *skills.Manager {
	return skills.NewManager(store, cfg.SkillsDir, cfg.SkillsRepoAllowlist)
}

func NewGuidelinesManager(dbConn *gorm.DB) *guidelines.Manager {
	return guidelines.NewManager(dbConn)
}

func NewFS(cfg *config.Config, auditStore *tools.AuditStore, unrestricted *bool) *tools.FS {
	return tools.NewFS(cfg.FSAllowedRoots, auditStore, *unrestricted)
}

func NewBrowserManager() *tools.BrowserManager {
	return tools.NewBrowserManager()
}

func NewExecManager() *tools.ExecManager {
	return tools.NewExecManager()
}

func NewToolRegistry(
	browserMgr *tools.BrowserManager,
	fsTools *tools.FS,
	execMgr *tools.ExecManager,
	cfg *config.Config,
	skillsMgr *skills.Manager,
	store memory.StoreAPI,
	guidelinesMgr *guidelines.Manager,
) *tools.Registry {
	return tools.NewRegistry(
		tools.NewBrowserGotoTool(browserMgr),
		tools.NewBrowserGoBackTool(browserMgr),
		tools.NewBrowserGetPageStateTool(browserMgr),
		tools.NewBrowserTakeScreenshotTool(browserMgr),
		tools.NewBrowserClickTool(browserMgr),
		tools.NewBrowserFillTextTool(browserMgr),
		tools.NewBrowserPressKeyTool(browserMgr),
		tools.NewBrowserScrollTool(browserMgr),
		tools.NewBraveSearchTool(cfg.BraveSearchAPIKey),
		tools.NewSessionSummaryTool(store),
		tools.NewMemorySearchTool(store),
		tools.NewInstallSkillTool(skillsMgr),
		tools.NewActivateSkillTool(skillsMgr),
		tools.NewGrepFilesTool(fsTools),
		tools.NewReadFileTool(fsTools),
		tools.NewListDirTool(fsTools),
		tools.NewExecCommandTool(fsTools, execMgr),
		tools.NewWriteStdinTool(execMgr),
		tools.NewUpdateGuidelinesTool(guidelinesMgr),
	)
}

func NewLLMClient(cfg *config.Config) *llm.Client {
	return llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, llm.WithHTTPDebug(cfg.LLMHTTPDebug))
}

func NewContextConfig(cfg *config.Config) llm.ContextConfig {
	return llm.ContextConfig{
		WindowTokens:                  cfg.LLMContextWindow,
		AutoCompactTokenLimit:         cfg.LLMAutoCompactTokenLimit,
		EffectiveContextWindowPercent: cfg.LLMEffectiveContextWindowPercent,
	}
}

func NewReasoningConfig(cfg *config.Config) llm.ReasoningConfig {
	return llm.ReasoningConfig{Enabled: cfg.LLMReasoningEnabled, Effort: cfg.LLMReasoningEffort}
}

func NewMemoryEngine(dbConn *gorm.DB, store memory.StoreAPI, llmClient *llm.Client, cfg *config.Config, log *zap.Logger) *memory.Engine {
	engine, err := memory.NewEngine(dbConn, store, llmClient, cfg.LLMModel, cfg.Memory)
	if err != nil {
		log.Fatal("memory engine init failed", zap.Error(err))
	}
	return engine
}

func NewForkManager(agentSvc *agent.Agent, store memory.StoreAPI) *fork.Manager {
	return fork.NewManager(agentSvc, store)
}

func NewServer(cfg *config.Config, agentSvc *agent.Agent, store memory.StoreAPI, skillsMgr *skills.Manager, memoryEngine *memory.Engine) *server.Server {
	return server.New(*cfg, agentSvc, store, skillsMgr, memoryEngine)
}

// RunApp is the main application invoker
func RunApp(
	lc fx.Lifecycle,
	cfg *config.Config,
	unrestricted *bool,
	agentSvc *agent.Agent,
	forkMgr *fork.Manager,
	toolRegistry *tools.Registry,
	srv *server.Server,
	fsTools *tools.FS,
	skillsMgr *skills.Manager,
	log *zap.Logger,
) {
	ctx, cancel := context.WithCancel(context.Background())

	// Register fork tools (these need forkMgr which depends on agent)
	toolRegistry.Register(fork.NewForkTool(forkMgr))
	toolRegistry.Register(fork.NewForkInterruptTool(forkMgr))
	toolRegistry.Register(fork.NewForkCancelTool(forkMgr))
	toolRegistry.Register(fork.NewForkStatusTool(forkMgr))

	// Setup messenger and approver for Telegram
	if cfg.TelegramToken != "" && !*unrestricted {
		agentSvc.SetSessionMessenger(srv)
		fsTools.SetApprover(srv)
	}

	// Security audit client
	if cfg.SecurityAuditModel != "" && !*unrestricted {
		auditClient := llm.NewClient(cfg.SecurityAuditBaseURL, cfg.SecurityAuditAPIKey, llm.WithHTTPDebug(cfg.LLMHTTPDebug))
		srv.SetSecurityAudit(auditClient, cfg.SecurityAuditModel)
	}

	// Start Telegram polling
	srv.StartTelegramPolling(ctx)

	// Initial skills refresh
	if err := skillsMgr.RefreshAll(ctx); err != nil {
		log.Warn("skills refresh failed", zap.Error(err))
	}

	// Background skills sync
	go syncLoop(ctx, skillsMgr, cfg.SkillsSyncInterval)

	// HTTP server with health check and pprof
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	httpServer := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: mux,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				log.Info("server listening", zap.String("addr", cfg.ServerAddr))
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatal("server error", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			return httpServer.Shutdown(shutdownCtx)
		},
	})
}

func syncLoop(ctx context.Context, mgr *skills.Manager, interval time.Duration) {
	log := logging.L().Named("skills_sync")
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := mgr.RefreshAll(ctx); err != nil {
				log.Warn("skills refresh failed", zap.Error(err))
			}
		}
	}
}
