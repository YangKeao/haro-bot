package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/fork"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/server"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
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
	log := logger.Named("agentd")

	dbConn, err := db.Open(cfg.TiDBDSN)
	if err != nil {
		log.Fatal("db open failed", zap.Error(err))
	}
	if err := db.ApplyMigrations(dbConn); err != nil {
		log.Fatal("db migrations failed", zap.Error(err))
	}

	log.Info("config loaded", zap.Any("cfg", cfg))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := memory.NewStore(dbConn)
	skillsStore := skills.NewStore(dbConn)
	skillsMgr := skills.NewManager(skillsStore, cfg.SkillsDir, cfg.SkillsRepoAllowlist)
	auditStore := tools.NewAuditStore(dbConn)
	fsTools := tools.NewFS(cfg.FSAllowedRoots, auditStore)
	browserMgr := tools.NewBrowserManager()
	execMgr := tools.NewExecManager()
	toolRegistry := tools.NewRegistry(
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
	)
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, llm.WithHTTPDebug(cfg.LLMHTTPDebug))
	contextCfg := llm.ContextConfig{
		WindowTokens:                  cfg.LLMContextWindow,
		AutoCompactTokenLimit:         cfg.LLMAutoCompactTokenLimit,
		EffectiveContextWindowPercent: cfg.LLMEffectiveContextWindowPercent,
	}
	memoryEngine, err := memory.NewEngine(dbConn, store, llmClient, cfg.LLMModel, cfg.Memory)
	if err != nil {
		log.Fatal("memory engine init failed", zap.Error(err))
	}

	agentSvc := agent.New(
		store,
		memoryEngine,
		skillsMgr,
		toolRegistry,
		fsTools.DefaultBase(),
		cfg.ToolMaxTurns,
		llmClient,
		cfg.LLMModel,
		string(cfg.LLMPromptFormat),
		llm.ReasoningConfig{Enabled: cfg.LLMReasoningEnabled, Effort: cfg.LLMReasoningEffort},
		contextCfg,
	)
	forkMgr := fork.NewManager(agentSvc, store)
	toolRegistry.Register(fork.NewForkTool(forkMgr))
	toolRegistry.Register(fork.NewForkInterruptTool(forkMgr))
	toolRegistry.Register(fork.NewForkCancelTool(forkMgr))
	toolRegistry.Register(fork.NewForkStatusTool(forkMgr))
	srv := server.New(cfg, agentSvc, store, skillsMgr)

	srv.StartTelegramPolling(ctx)

	if err := skillsMgr.RefreshAll(ctx); err != nil {
		log.Warn("skills refresh failed", zap.Error(err))
	}
	go syncLoop(ctx, skillsMgr, cfg.SkillsSyncInterval)

	httpServer := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: srv.Routes(),
	}

	go func() {
		log.Info("listening", zap.String("addr", cfg.ServerAddr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
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
