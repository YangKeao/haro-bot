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
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	unrestricted := flag.Bool("unrestricted", false, "skip path restrictions and symlink checks")
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
		fx.Supply(cfg),
		fx.Supply(unrestricted),
		fx.Provide(func() *zap.Logger { return logger }),

		// Core modules
		db.Module,
		llm.Module,
		memory.Module,
		skills.Module,
		guidelines.Module,
		tools.Module,
		agent.Module,
		fork.Module,
		server.Module,

		// Named values for agent.Params
		fx.Provide(
			fx.Annotate(func(cfg *config.Config) string { return cfg.LLMModel }, fx.ResultTags(`name:"llm_model"`)),
			fx.Annotate(func(cfg *config.Config) string { return string(cfg.LLMPromptFormat) }, fx.ResultTags(`name:"prompt_format"`)),
			fx.Annotate(func(fs *tools.FS) string { return fs.DefaultBase() }, fx.ResultTags(`name:"default_base_dir"`)),
			fx.Annotate(func(cfg *config.Config) int { return cfg.ToolMaxTurns }, fx.ResultTags(`name:"max_tool_turns"`)),
		),

		fx.Invoke(RunApp),
	)

	app.Run()
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

	// Register fork tools
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
