package api

import (
	"github.com/gin-gonic/gin"
)

// RouteProvider ...
type RouteProvider interface {
	// GetName 获取提供者名称
	GetName() string
	// GetGroupPath 获取分组路径前缀
	GetGroupPath() string
	// GetMiddlewares 获取分组级别的中间件
	GetMiddlewares() []gin.HandlerFunc
	// RegisterRoutes 在指定的路由分组上注册路由
	RegisterRoutes(group *gin.RouterGroup)
}

// BaseRouterGroup 基础路由分组实现
type BaseRouterGroup struct {
	name        string
	groupPath   string
	middlewares []gin.HandlerFunc
}

func NewBaseRouterGroup(name, groupPath string) *BaseRouterGroup {
	return &BaseRouterGroup{
		name:        name,
		groupPath:   groupPath,
		middlewares: make([]gin.HandlerFunc, 0),
	}
}

// GetName 获取提供者名称
func (b *BaseRouterGroup) GetName() string {
	return b.name
}

// GetGroupPath 获取分组路径前缀
func (b *BaseRouterGroup) GetGroupPath() string {
	return b.groupPath
}

// GetMiddlewares 获取分组级别的中间件
func (b *BaseRouterGroup) GetMiddlewares() []gin.HandlerFunc {
	return b.middlewares
}

// AddMiddleware 添加中间件
func (b *BaseRouterGroup) AddMiddleware(middleware gin.HandlerFunc) {
	b.middlewares = append(b.middlewares, middleware)
}

// SetMiddlewares 设置中间件
func (b *BaseRouterGroup) SetMiddlewares(middlewares []gin.HandlerFunc) {
	b.middlewares = middlewares
}
