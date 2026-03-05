package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YangKeao/haro-bot/internal/agent"
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/db"
	"github.com/YangKeao/haro-bot/internal/llm"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/server"
	"github.com/YangKeao/haro-bot/internal/skills"
	"github.com/YangKeao/haro-bot/internal/tools"
)

func main() {
	baseCfg := config.LoadBase()

	dbConn, err := db.Open(baseCfg.TiDBDSN)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	if err := db.ApplyMigrations(dbConn); err != nil {
		log.Fatalf("db migrations: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.LoadFromDB(ctx, dbConn, baseCfg)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}

	store := memory.NewStore(dbConn)
	skillsStore := skills.NewStore(dbConn)
	skillsMgr := skills.NewManager(skillsStore, cfg.SkillsDir, cfg.SkillsRepoAllowlist)
	auditStore := tools.NewAuditStore(dbConn)
	fsTools := tools.NewFS(cfg.FSAllowedRoots, cfg.FSAllowExec, cfg.FSAllowedExecDirs, auditStore)
	toolRegistry := tools.NewRegistry(
		tools.NewActivateSkillTool(skillsMgr),
		tools.NewReadTool(fsTools, 64*1024),
		tools.NewWriteTool(fsTools),
		tools.NewSearchTool(fsTools),
		tools.NewEditTool(fsTools),
	)
	if fsTools.ExecEnabled() {
		toolRegistry.Register(tools.NewExecTool(fsTools, 64*1024))
	}
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey)

	agentSvc := agent.New(store, skillsMgr, toolRegistry, fsTools.DefaultBase(), cfg.ToolMaxTurns, llmClient, cfg.LLMModel, cfg.LLMPromptFormat)
	srv := server.New(cfg, agentSvc, store, skillsMgr)

	srv.StartTelegramPolling(ctx)

	if err := skillsMgr.RefreshAll(ctx); err != nil {
		log.Printf("skills refresh: %v", err)
	}
	go syncLoop(ctx, skillsMgr, cfg.SkillsSyncInterval)

	httpServer := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: srv.Routes(),
	}

	go func() {
		log.Printf("listening on %s", cfg.ServerAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
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
				log.Printf("skills refresh: %v", err)
			}
		}
	}
}
