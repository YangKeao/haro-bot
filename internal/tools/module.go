package tools

import (
	"github.com/YangKeao/haro-bot/internal/config"
	"github.com/YangKeao/haro-bot/internal/guidelines"
	"github.com/YangKeao/haro-bot/internal/memory"
	"github.com/YangKeao/haro-bot/internal/skills"
	"go.uber.org/fx"
)

// Module provides tools and tool registry.
var Module = fx.Module("tools",
	fx.Provide(
		NewAuditStore,
		NewBrowserManager,
		NewExecManager,
		NewFSFromConfig,
		NewRegistryFromDeps,
	),
)

// FSParams contains dependencies for creating FS.
type FSParams struct {
	fx.In

	Cfg         *config.Config
	AuditStore  *AuditStore
	Unrestricted *bool
}

// NewFSFromConfig creates FS with config.
func NewFSFromConfig(p FSParams) *FS {
	return NewFS(p.Cfg.FSAllowedRoots, p.AuditStore, *p.Unrestricted)
}

// RegistryParams contains dependencies for creating Registry.
type RegistryParams struct {
	fx.In

	BrowserMgr   *BrowserManager
	FS           *FS
	ExecMgr      *ExecManager
	Cfg          *config.Config
	SkillsMgr    *skills.Manager
	Store        memory.StoreAPI
	GuidelinesMgr *guidelines.Manager
}

// NewRegistryFromDeps creates a registry with all tools.
func NewRegistryFromDeps(p RegistryParams) *Registry {
	return NewRegistry(
		NewBrowserGotoTool(p.BrowserMgr),
		NewBrowserGoBackTool(p.BrowserMgr),
		NewBrowserGetPageStateTool(p.BrowserMgr),
		NewBrowserTakeScreenshotTool(p.BrowserMgr),
		NewBrowserClickTool(p.BrowserMgr),
		NewBrowserFillTextTool(p.BrowserMgr),
		NewBrowserPressKeyTool(p.BrowserMgr),
		NewBrowserScrollTool(p.BrowserMgr),
		NewBraveSearchTool(p.Cfg.BraveSearchAPIKey),
		NewSessionSummaryTool(p.Store),
		NewMemorySearchTool(p.Store),
		NewInstallSkillTool(p.SkillsMgr),
		NewActivateSkillTool(p.SkillsMgr),
		NewGrepFilesTool(p.FS),
		NewReadFileTool(p.FS),
		NewListDirTool(p.FS),
		NewExecCommandTool(p.FS, p.ExecMgr),
		NewWriteStdinTool(p.ExecMgr),
		NewUpdateGuidelinesTool(p.GuidelinesMgr),
	)
}
