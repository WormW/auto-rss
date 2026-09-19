package app

import (
	"sync"

	"github.com/WormW/auto-rss/internal/service/medialibrary"
)

// Context 应用上下文，管理可动态重载的组件
type Context struct {
	mu              sync.RWMutex
	mediaLibrarySvc *medialibrary.Service
	shutdownHooks   []func()
}

// NewContext 创建应用上下文
func NewContext() *Context {
	return &Context{}
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

// RegisterShutdownHook registers a service cleanup callback for graceful shutdown.
func (ctx *Context) RegisterShutdownHook(hook func()) {
	if hook == nil {
		return
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.shutdownHooks = append(ctx.shutdownHooks, hook)
}

// Shutdown 关闭所有服务
func (ctx *Context) Shutdown() {
	ctx.mu.Lock()
	hooks := ctx.shutdownHooks
	ctx.shutdownHooks = nil
	ctx.mu.Unlock()

	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i]()
	}
}
