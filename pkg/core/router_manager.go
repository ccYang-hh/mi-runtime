package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	. "xfusion.com/tmatrix/runtime/pkg/api"
	. "xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

// RouterManager 路由管理器 - 基于Gin生态的优雅设计
type RouterManager struct {
	// 核心组件
	engine *gin.Engine

	// 路由注册表
	systemProviders   map[string]RouteProvider
	pluginProviders   map[string]RouteProvider
	pipelineProviders map[string]pipelines.IPipeline

	// 已注册路由
	registeredRoutes []RegisteredRoute

	// 请求分发器
	dispatcher *RequestDispatcher

	// 配置
	config RouterManagerConfig

	// 并发安全
	mu sync.RWMutex

	// 生命周期管理
	initialized int32
	closed      int32
}

// RouterManagerConfig 路由管理器配置
type RouterManagerConfig struct {
	EnableLogging    bool `json:"enable_logging"`
	EnableMiddleware bool `json:"enable_middleware"`
	EnableMetrics    bool `json:"enable_metrics"`
	MaxProviders     int  `json:"max_providers"`
}

// NewRouterManager 创建路由管理器
func NewRouterManager(engine *gin.Engine, dispatcher *RequestDispatcher, config RouterManagerConfig) *RouterManager {
	if config.MaxProviders <= 0 {
		config.MaxProviders = 1000 // 默认最大Provider数量
	}

	return &RouterManager{
		engine:            engine,
		dispatcher:        dispatcher,
		config:            config,
		systemProviders:   make(map[string]RouteProvider),
		pluginProviders:   make(map[string]RouteProvider),
		pipelineProviders: make(map[string]pipelines.IPipeline),
		registeredRoutes:  make([]RegisteredRoute, 0),
	}
}

// Initialize 初始化路由管理器
func (rm *RouterManager) Initialize() error {
	if !atomic.CompareAndSwapInt32(&rm.initialized, 0, 1) {
		logger.Errorf("router manager already initialized")
		return fmt.Errorf("router manager already initialized")
	}

	// 注册全局NoRoute处理器
	if rm.dispatcher != nil {
		rm.engine.NoRoute(rm.dispatcher.GetHandler())
	}

	// 标记为已初始化
	atomic.StoreInt32(&rm.initialized, 1)

	logger.Infof("router manager initialized with config: %+v", rm.config)
	return nil
}

// RegisterSystemProvider 注册系统路由提供者
func (rm *RouterManager) RegisterSystemProvider(provider RouteProvider) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	name := provider.GetName()
	if name == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查重复注册
	if _, exists := rm.systemProviders[name]; exists {
		return fmt.Errorf("system provider already registered: %s", name)
	}

	// 检查Provider数量限制
	if len(rm.systemProviders) >= rm.config.MaxProviders {
		return fmt.Errorf("system providers limit exceeded: %d", rm.config.MaxProviders)
	}

	// 注册provider
	rm.systemProviders[name] = provider

	// 注册路由到Gin
	err := rm.registerProviderRoutes(provider, RouteSourceSystem)
	if err != nil {
		delete(rm.systemProviders, name)
		return fmt.Errorf("failed to register system routes for %s: %w", name, err)
	}

	logger.Infof("registered system provider: %s, group: %s", name, provider.GetGroupPath())

	return nil
}

// RegisterPluginProvider 注册插件路由提供者
func (rm *RouterManager) RegisterPluginProvider(provider RouteProvider) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	name := provider.GetName()
	if name == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查重复注册
	if _, exists := rm.pluginProviders[name]; exists {
		return fmt.Errorf("plugin provider already registered: %s", name)
	}

	// 检查Provider数量限制
	if len(rm.pluginProviders) >= rm.config.MaxProviders {
		return fmt.Errorf("plugin providers limit exceeded: %d", rm.config.MaxProviders)
	}

	// 注册provider
	rm.pluginProviders[name] = provider

	// 注册路由到Gin
	err := rm.registerProviderRoutes(provider, RouteSourcePlugin)
	if err != nil {
		delete(rm.pluginProviders, name)
		return fmt.Errorf("failed to register plugin routes for %s: %w", name, err)
	}

	logger.Infof("registered plugin provider: %s, group: %s", name, provider.GetGroupPath())

	return nil
}

// RegisterPipelineProvider 注册Pipeline路由提供者
func (rm *RouterManager) RegisterPipelineProvider(pipeline pipelines.IPipeline) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	if pipeline == nil {
		return fmt.Errorf("pipeline cannot be nil")
	}

	name := pipeline.GetName()
	if name == "" {
		return fmt.Errorf("pipeline name cannot be empty")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查重复注册
	if _, exists := rm.pipelineProviders[name]; exists {
		return fmt.Errorf("pipeline provider already registered: %s", name)
	}

	// 注册pipeline
	rm.pipelineProviders[name] = pipeline

	// 注册路由到分发器和Gin
	err := rm.registerPipelineRoutes(pipeline)
	if err != nil {
		delete(rm.pipelineProviders, name)
		return fmt.Errorf("failed to register pipeline routes for %s: %w", name, err)
	}

	// 记录注册信息
	registeredRoute := RegisteredRoute{
		Name:         name,
		GroupPath:    "", // Pipeline通常没有统一前缀
		Source:       RouteSourcePipeline,
		RegisteredAt: time.Now(),
	}
	rm.registeredRoutes = append(rm.registeredRoutes, registeredRoute)

	logger.Infof("registered pipeline provider: %s", name)

	return nil
}

// registerProviderRoutes 注册Provider路由到Gin
func (rm *RouterManager) registerProviderRoutes(provider RouteProvider, source RouteSource) error {
	groupPath := provider.GetGroupPath()
	middlewares := provider.GetMiddlewares()

	// 创建Gin路由组
	var ginGroup *gin.RouterGroup
	if groupPath != "" {
		ginGroup = rm.engine.Group(groupPath)
	} else {
		// 如果没有指定groupPath，使用根路由组
		ginGroup = &rm.engine.RouterGroup
	}

	// 应用分组级别的中间件
	if len(middlewares) > 0 {
		ginGroup.Use(middlewares...)
	}

	// 让Provider注册路由
	provider.RegisterRoutes(ginGroup)

	// 记录注册信息
	registeredRoute := RegisteredRoute{
		Name:         provider.GetName(),
		GroupPath:    groupPath,
		Source:       source,
		RegisteredAt: time.Now(),
	}
	rm.registeredRoutes = append(rm.registeredRoutes, registeredRoute)

	return nil
}

// registerPipelineRoutes 注册Pipeline路由
func (rm *RouterManager) registerPipelineRoutes(pipeline pipelines.IPipeline) error {
	routes := pipeline.GetRoutes()
	if len(routes) == 0 {
		logger.Errorf("pipeline has no routes: %s", pipeline.GetName())
		return fmt.Errorf("pipeline has no routes")
	}

	for _, route := range routes {
		// 注册到分发器
		if rm.dispatcher != nil {
			if err := rm.dispatcher.RegisterRoute(route.Path, pipeline, []string{route.Method}); err != nil {
				return fmt.Errorf("failed to register route %s to dispatcher: %w", route.Path, err)
			}
		}

		// 注册到Gin引擎
		rm.registerGinRoute(route.Method, route.Path, rm.dispatcher.GetHandler())

		logger.Debugf("registered pipeline route: %v %s -> %s", route.Method, route.Path, pipeline.GetName())
	}

	return nil
}

// registerGinRoute 注册Gin路由
func (rm *RouterManager) registerGinRoute(method, path string, handler gin.HandlerFunc) {
	switch method {
	case "GET":
		rm.engine.GET(path, handler)
	case "POST":
		rm.engine.POST(path, handler)
	case "PUT":
		rm.engine.PUT(path, handler)
	case "DELETE":
		rm.engine.DELETE(path, handler)
	case "PATCH":
		rm.engine.PATCH(path, handler)
	case "HEAD":
		rm.engine.HEAD(path, handler)
	case "OPTIONS":
		rm.engine.OPTIONS(path, handler)
	default:
		rm.engine.Any(path, handler)
	}
}

// UnregisterSystemProvider 注销系统路由提供者
func (rm *RouterManager) UnregisterSystemProvider(name string) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.systemProviders[name]; !exists {
		return fmt.Errorf("system provider not found: %s", name)
	}

	delete(rm.systemProviders, name)
	rm.removeRegisteredRoute(name, RouteSourceSystem)

	logger.Infof("unregistered system provider: %s", name)
	return nil
}

// UnregisterPluginProvider 注销插件路由提供者
func (rm *RouterManager) UnregisterPluginProvider(name string) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.pluginProviders[name]; !exists {
		return fmt.Errorf("plugin provider not found: %s", name)
	}

	delete(rm.pluginProviders, name)
	rm.removeRegisteredRoute(name, RouteSourcePlugin)

	logger.Infof("unregistered plugin provider: %s", name)
	return nil
}

// UnregisterPipelineProvider 注销Pipeline路由提供者
func (rm *RouterManager) UnregisterPipelineProvider(name string) error {
	if !rm.isInitialized() {
		return fmt.Errorf("router manager not initialized")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.pipelineProviders[name]; !exists {
		return fmt.Errorf("pipeline provider not found: %s", name)
	}

	delete(rm.pipelineProviders, name)
	rm.removeRegisteredRoute(name, RouteSourcePipeline)

	logger.Infof("unregistered pipeline provider: %s", name)
	return nil
}

// removeRegisteredRoute 从注册记录中移除路由
func (rm *RouterManager) removeRegisteredRoute(name string, source RouteSource) {
	for i, route := range rm.registeredRoutes {
		if route.Name == name && route.Source == source {
			rm.registeredRoutes = append(rm.registeredRoutes[:i], rm.registeredRoutes[i+1:]...)
			break
		}
	}
}

// GetProvider 获取Provider
func (rm *RouterManager) GetProvider(name string, source RouteSource) (interface{}, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	switch source {
	case RouteSourceSystem:
		provider, exists := rm.systemProviders[name]
		return provider, exists
	case RouteSourcePlugin:
		provider, exists := rm.pluginProviders[name]
		return provider, exists
	case RouteSourcePipeline:
		pipeline, exists := rm.pipelineProviders[name]
		return pipeline, exists
	default:
		return nil, false
	}
}

// ListProviders 列出所有Provider
func (rm *RouterManager) ListProviders() map[RouteSource][]string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[RouteSource][]string)

	// 系统Provider
	systemNames := make([]string, 0, len(rm.systemProviders))
	for name := range rm.systemProviders {
		systemNames = append(systemNames, name)
	}
	result[RouteSourceSystem] = systemNames

	// 插件Provider
	pluginNames := make([]string, 0, len(rm.pluginProviders))
	for name := range rm.pluginProviders {
		pluginNames = append(pluginNames, name)
	}
	result[RouteSourcePlugin] = pluginNames

	// Pipeline Provider
	pipelineNames := make([]string, 0, len(rm.pipelineProviders))
	for name := range rm.pipelineProviders {
		pipelineNames = append(pipelineNames, name)
	}
	result[RouteSourcePipeline] = pipelineNames

	return result
}

// ListRegisteredRoutes 列出所有已注册的路由
func (rm *RouterManager) ListRegisteredRoutes() []RegisteredRoute {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// 复制切片避免并发问题
	routes := make([]RegisteredRoute, len(rm.registeredRoutes))
	copy(routes, rm.registeredRoutes)

	return routes
}

// Shutdown 关闭路由管理器
func (rm *RouterManager) Shutdown() error {
	if !atomic.CompareAndSwapInt32(&rm.closed, 0, 1) {
		return fmt.Errorf("router manager already closed")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 清理资源
	rm.systemProviders = make(map[string]RouteProvider)
	rm.pluginProviders = make(map[string]RouteProvider)
	rm.pipelineProviders = make(map[string]pipelines.IPipeline)
	rm.registeredRoutes = rm.registeredRoutes[:0]

	logger.Infof("router manager closed")
	return nil
}

// isInitialized 检查是否已初始化
func (rm *RouterManager) isInitialized() bool {
	return atomic.LoadInt32(&rm.initialized) == 1
}
