package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/im"
	imtelegram "github.com/YangKeao/haro-bot/internal/im/telegram"
	"github.com/YangKeao/haro-bot/internal/logging"
	"github.com/YangKeao/haro-bot/internal/mcpmanager"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/sandbox"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
	webui "github.com/YangKeao/haro-bot/internal/web"
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

	log.Info("config loaded",
		zap.String("server_addr", cfg.ServerAddr),
		zap.String("web_assets_dir", cfg.Web.AssetsDir),
		zap.String("object_store_endpoint", cfg.Web.ObjectStorage.Endpoint),
		zap.String("object_store_bucket", cfg.Web.ObjectStorage.Bucket),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	objectStore, err := webui.NewObjectStore(cfg.Web.ObjectStorage)
	if err != nil {
		log.Fatal("web object storage config failed", zap.Error(err))
	}
	store := memory.NewStore(dbConn, memory.WithAttachmentLoader(objectStore.DataURL))
	skillsStore := skills.NewStore(dbConn)
	skillsMgr := skills.NewManager(skillsStore, cfg.SkillsDir, cfg.SkillsRepoAllowlist)
	guidelinesMgr := guidelines.NewManager(dbConn)

	toolRegistry := tools.NewRegistry(
		tools.NewBraveSearchTool(cfg.BraveSearchAPIKey),
		tools.NewSessionSummaryTool(store),
		tools.NewUpdateGuidelinesTool(guidelinesMgr),
	)
	webStore := webui.NewStore(dbConn)
	sandboxService, err := sandbox.NewService(dbConn, cfg.Sandbox)
	if err != nil {
		log.Fatal("sandbox initialization failed", zap.Error(err))
	}
	var secretBox *sandbox.SecretBox
	if cfg.SecretKey != "" {
		secretBox, err = sandbox.NewSecretBox(cfg.SecretKey)
		if err != nil {
			log.Fatal("secret encryption initialization failed", zap.Error(err))
		}
	}
	mcpManager, err := mcpmanager.New(dbConn, secretBox, sandboxService, cfg.Web.PublicURL)
	if err != nil {
		log.Fatal("MCP initialization failed", zap.Error(err))
	}
	webUserID, err := store.GetOrCreateUserByExternalID(ctx, "web", "owner")
	if err != nil {
		log.Fatal("create web owner failed", zap.Error(err))
	}
	mcpManager.SetArtifactSink(webui.NewMCPArtifactSink(webStore, objectStore, webUserID))
	webRuntimes := webui.NewRuntimeRegistry(webStore, store, skillsMgr, toolRegistry, objectStore, guidelinesMgr, sandboxService, mcpManager, "/workspace", cfg.ToolMaxTurns, cfg.LLMHTTPDebug)
	var imRuntime im.Runtime = imtelegram.New(cfg, webRuntimes, store, webStore)
	webServer, err := webui.NewServer(ctx, webui.ServerDeps{
		Config: cfg.Web, Store: webStore, Conversation: store, Objects: objectStore, Runtimes: webRuntimes,
		Guidelines: guidelinesMgr, Skills: skillsMgr, Sandboxes: sandboxService, MCP: mcpManager, UserID: webUserID, Logger: log,
		TelegramTokenConfigured: cfg.TelegramToken != "",
	})
	if err != nil {
		log.Fatal("web server init failed", zap.Error(err))
	}

	if err := skillsMgr.RefreshAll(ctx); err != nil {
		log.Warn("skills refresh failed", zap.Error(err))
	}
	go syncLoop(ctx, skillsMgr, cfg.SkillsSyncInterval)

	imRuntime.Start(ctx)

	// Create HTTP handler with health check and pprof
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// Mount pprof handlers under /debug/pprof/
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	webServer.Register(mux)
	webServer.StartCleanup(ctx)

	httpServer := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: mux,
	}

	go func() {
		log.Info("server listening", zap.String("addr", cfg.ServerAddr))
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
