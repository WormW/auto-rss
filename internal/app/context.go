package app

import (
	"sync"

	"github.com/WormW/auto-rss/internal/config"
	"github.com/WormW/auto-rss/internal/pkg/logger"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/WormW/auto-rss/internal/service/bangumi"
	"github.com/WormW/auto-rss/internal/service/medialibrary"
	"github.com/WormW/auto-rss/internal/service/organizer"
	"gorm.io/gorm"
)

// Context 应用上下文，管理可动态重载的组件
type Context struct {
	mu               sync.RWMutex
	db               *gorm.DB
	cfg              *config.Config
	subscriptionRepo repository.SubscriptionRepository
	downloadRepo     repository.DownloadRepository
	bangumiService   *bangumi.BangumiService
	mediaLibrarySvc  *medialibrary.Service
	renameTemplate   string
	fileOrganizer    *organizer.FileOrganizer
	shutdownHooks    []func()
}

// NewContext 创建应用上下文
func NewContext(db *gorm.DB, cfg *config.Config, subscriptionRepo repository.SubscriptionRepository, downloadRepo repository.DownloadRepository, bangumiService *bangumi.BangumiService) *Context {
	return &Context{
		db:               db,
		cfg:              cfg,
		subscriptionRepo: subscriptionRepo,
		downloadRepo:     downloadRepo,
		bangumiService:   bangumiService,
	}
}

// SetRenameTemplate 设置重命名模板
func (ctx *Context) SetRenameTemplate(template string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.renameTemplate = template
}

// SetMediaLibraryService 设置媒体库刷新服务
func (ctx *Context) SetMediaLibraryService(service *medialibrary.Service) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.mediaLibrarySvc = service
}

// GetMediaLibraryService 获取媒体库刷新服务
func (ctx *Context) GetMediaLibraryService() *medialibrary.Service {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.mediaLibrarySvc
}

// GetFileOrganizer 获取文件整理服务
func (ctx *Context) GetFileOrganizer() *organizer.FileOrganizer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.fileOrganizer
}

// RegisterShutdownHook registers a service cleanup callback for graceful shutdown.
func (ctx *Context) RegisterShutdownHook(hook func()) {
	if hook == nil {
		return
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.shutdownHooks = append(ctx.shutdownHooks, hook)
}

// ReloadFileOrganizer 重新加载文件整理服务
func (ctx *Context) ReloadFileOrganizer() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	// 停止旧的服务
	if ctx.fileOrganizer != nil {
		ctx.fileOrganizer.Stop()
		ctx.fileOrganizer = nil
	}

	// 重新从数据库加载配置
	if err := ctx.cfg.LoadFromDB(ctx.db); err != nil {
		logger.Error("Failed to reload config from DB", "error", err)
		return err
	}

	// 检查是否启用
	if !ctx.cfg.FileOrganizerEnabled {
		logger.Info("File organizer disabled after reload")
		return nil
	}

	// 检查目录配置
	if ctx.cfg.FileOrganizerDir == "" {
		logger.Warn("File organizer enabled but directory not configured")
		return nil
	}

	// 创建新的文件整理服务
	fileOrg, err := organizer.NewFileOrganizer(
		ctx.cfg.FileOrganizerDir,
		ctx.cfg.FileOrganizerDir,
		ctx.subscriptionRepo,
		ctx.downloadRepo,
		ctx.db,
		ctx.bangumiService,
		ctx.renameTemplate,
		ctx.mediaLibrarySvc,
	)
	if err != nil {
		logger.Error("Failed to create file organizer", "error", err)
		return err
	}

	// 启动服务
	if err := fileOrg.Start(); err != nil {
		logger.Error("Failed to start file organizer", "error", err)
		return err
	}

	ctx.fileOrganizer = fileOrg
	logger.Info("File organizer reloaded successfully", "dir", ctx.cfg.FileOrganizerDir)

	return nil
}

// InitializeFileOrganizer 初始化文件整理服务
func (ctx *Context) InitializeFileOrganizer() error {
	return ctx.ReloadFileOrganizer()
}

// Shutdown 关闭所有服务
func (ctx *Context) Shutdown() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.fileOrganizer != nil {
		ctx.fileOrganizer.Stop()
		ctx.fileOrganizer = nil
	}

	for i := len(ctx.shutdownHooks) - 1; i >= 0; i-- {
		ctx.shutdownHooks[i]()
	}
	ctx.shutdownHooks = nil
}
