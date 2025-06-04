package core

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.uber.org/atomic"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/discovery"
	"xfusion.com/tmatrix/runtime/pkg/metrics"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// Config 运行时配置
type Config struct {
	ConfigPath string
	Host       string
	Port       int
	LogLevel   string
}

// RuntimeCore 运行时核心
// 负责协调系统的所有组件，包括插件、管道、API等
type RuntimeCore struct {
	ctx    context.Context
	cancel context.CancelFunc

	// 配置相关
	config        *config.RuntimeConfig
	configManager *config.Manager
	//eventBus      *events.Bus

	// 业务流构建组件
	pluginManager   plugins.IPluginManager
	pipelineBuilder pipelines.IPipelineBuilder
	//routerManager   *router.Manager

	// 服务组件
	httpServer       *http.Server
	serviceDiscovery discovery.ServiceDiscovery
	monitor          metrics.IMetricsMonitor

	// 状态管理
	pipelines     map[string]pipelines.IPipeline
	isInitialized atomic.Bool
	startupTime   time.Time

	// 并发控制
	wg sync.WaitGroup
	mu sync.RWMutex
}

// NewRuntimeCore 创建运行时核心
func NewRuntimeCore(ctx context.Context, configPath string) (*RuntimeCore, error) {
	runtimeCtx, cancel := context.WithCancel(ctx)

	r := &RuntimeCore{
		ctx:       runtimeCtx,
		cancel:    cancel,
		pipelines: make(map[string]*pipelines.Pipeline),
	}

	// 初始化基础组件
	if err := r.initialize(configPath); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize base components: %w", err)
	}

	return r, nil
}

// Start 启动运行时核心
func (r *RuntimeCore) Start() error {
	if r.isInitialized.Load() {
		logger.Warnf("runtime already initialized")
		return nil
	}

	logger.Infof("starting runtime %s v%s", r.config.AppName, r.config.AppVersion)
	r.startupTime = time.Now()

	// 按顺序启动各个组件
	if err := r.startComponents(); err != nil {
		return fmt.Errorf("failed to start components: %w", err)
	}

	// 启动HTTP服务器
	if err := r.startHTTPServer(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	r.isInitialized.Store(true)
	logger.Infof("%s initialized successfully", r.config.AppName)

	return nil
}

// Shutdown 关闭Runtime Core
func (r *RuntimeCore) Shutdown(ctx context.Context) error {
	if !r.isInitialized.Load() {
		logger.Warnf("runtime already closed")
		return nil
	}

	logger.Infof("shutting down runtime...")

	defer r.cancel()
	defer r.isInitialized.Store(false)

	var err error

	// 关闭HTTP服务器
	if r.httpServer != nil {
		if err = r.httpServer.Shutdown(ctx); err != nil {
			logger.Errorf("failed to shutdown http server: %+v", err)
		}
	}

	// 关闭监控服务
	if r.monitor != nil {
		if err = r.monitor.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown runtime monitor: %+v", err)
		}
	}

	// 关闭插件管理器
	if r.pluginManager != nil {
		if err = r.pluginManager.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown plugin manager: %+v", err)
		}
	}

	// 关闭服务发现
	if r.serviceDiscovery != nil {
		if err = r.serviceDiscovery.Close(); err != nil {
			logger.Errorf("failed to close service discovery: %+v", err)
		}
	}

	// 关闭配置管理器
	if r.configManager != nil {
		if err = r.configManager.Close(); err != nil {
			logger.Errorf("failed to close service discovery: %+v", err)
		}
	}

	return err
}

// initialize 初始化Runtime
func (r *RuntimeCore) initialize(configPath string) error {
	var err error

	// 初始化事件总线
	// r.eventBus = events.NewBus()

	// 初始化配置管理器
	r.configManager, err = config.NewManager(configPath)
	if err != nil {
		logger.Errorf("failed to create config manager: %+v", err)
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	// 获取应用配置
	r.config = r.configManager.GetConfig()

	return nil
}

// startComponents 启动各个组件
func (r *RuntimeCore) startComponents() error {
	// 初始化插件管理器
	if err := r.initPluginManager(); err != nil {
		logger.Errorf("failed to initialize plugin manager: %+v", err)
		return fmt.Errorf("failed to initialize plugin manager: %w", err)
	}

	// 构建管道
	if err := r.buildPipelines(); err != nil {
		logger.Errorf("failed to build pipelines: %+v", err)
		return fmt.Errorf("failed to build pipelines: %w", err)
	}

	// 设置路由
	if err := r.setupRoutes(); err != nil {
		logger.Errorf("failed to setup routes: %+v", err)
		return fmt.Errorf("failed to setup routes: %w", err)
	}

	// 初始化服务发现
	if err := r.initServiceDiscovery(); err != nil {
		logger.Errorf("failed to initialize service discovery: %+v", err)
		return fmt.Errorf("failed to initialize service discovery: %w", err)
	}

	// 启动监控进程
	if err := r.startMonitor(); err != nil {
		logger.Errorf("failed to start monitor: %+v", err)
		return fmt.Errorf("failed to start monitor: %w", err)
	}

	return nil
}

// initPluginManager 初始化插件管理器
func (r *RuntimeCore) initPluginManager() error {
	var err error

	// 初始化PluginManager
	r.pluginManager, err = plugins.NewPluginManager(r.ctx, r.config.PluginRegistry)
	if err != nil {
		logger.Errorf("failed to create plugin manager: %+v", err)
		return fmt.Errorf("failed to create plugin manager: %w", err)
	}

	// 启动PluginManager
	if err = r.pluginManager.Start(); err != nil {
		logger.Errorf("failed to start plugin manager: %+v", err)
		return fmt.Errorf("failed to start plugin manager: %w", err)
	}

	return nil
}

// buildPipelines 构建所有管道
func (r *RuntimeCore) buildPipelines() error {
	logger.Infof("start building pipelines...")

	// 构建PipelineBuilder
	builderConfig := r.createPipelineBuilderConfig()
	r.pipelineBuilder = pipelines.NewPipelineBuilder(r.pluginManager, builderConfig)

	// 构建所有管道
	var err error
	r.pipelines, err = r.pipelineBuilder.BuildPipelines()
	if err != nil {
		logger.Errorf("failed to build pipelines: %+v", err)
		return fmt.Errorf("failed to build pipelines: %w", err)
	}

	logger.Infof("built %d pipelines successfully", len(r.pipelines))
	return nil
}

// createPipelineBuilderConfig 创建管道构建器配置
func (r *RuntimeCore) createPipelineBuilderConfig() *pipelines.PipelineBuilderConfig {
	// 默认管道配置
	defaultPipeline := &config.PipelineConfig{
		PipelineName: "default",
		Plugins:      []string{"request_analyzer", "vllm_router", "request_processor"},
		Routes: []*config.PipelineRoute{
			{Path: "/v1/models", Method: "GET"},
			{Path: "/v1/embeddings", Method: "POST"},
			{Path: "/v1/completions", Method: "POST"},
			{Path: "/v1/chat/completions", Method: "POST"},
		},
	}

	// 基于配置文件构造Pipelines
	registerPipelines := make([]*config.PipelineConfig, 0, len(r.config.Pipelines))
	for _, pipelineInfo := range r.config.Pipelines {
		registerPipelines = append(registerPipelines, &config.PipelineConfig{
			PipelineName: pipelineInfo.PipelineName,
			Plugins:      pipelineInfo.Plugins,
			Routes:       pipelineInfo.Routes,
		})
	}

	return &pipelines.PipelineBuilderConfig{
		Pipelines: registerPipelines,
		Default:   defaultPipeline,
	}
}

// setupRoutes 设置所有路由
func (r *RuntimeCore) setupRoutes() error {
	// 创建路由管理器
	r.routerManager = router.NewManager()

	// 注册系统路由
	r.registerSystemRoutes()

	// 注册插件路由
	r.registerPluginRoutes()

	// 注册管道路由
	r.registerPipelineRoutes()

	return nil
}

// initServiceDiscovery 初始化服务发现
func (r *RuntimeCore) initServiceDiscovery() error {
	// 1.构造Config
	discoveryConfig := &discovery.Config{
		Type:     r.config.ServiceDiscovery,
		CacheTTL: 30 * time.Second,
	}

	switch r.config.ServiceDiscovery {
	case discovery.ServiceDiscoveryTypeETCD:
		discoveryConfig.ETCD.Endpoints = []string{
			fmt.Sprintf("%s:%s", r.config.ETCD.Host, strconv.Itoa(r.config.ETCD.Port)),
		}
		discoveryConfig.ETCD.Prefix = "/tmatrix/runtime/endpoints"
	case discovery.ServiceDiscoveryTypeK8S:
		// TODO
		//  Support K8S
	default:
		logger.Errorf("unsupported service discovery type: %s", r.config.ServiceDiscovery)
		return fmt.Errorf("unsupported service discovery type: %s", r.config.ServiceDiscovery)
	}

	// 2.构造ServiceDiscovery实例
	var err error
	r.serviceDiscovery, err = discovery.NewServiceDiscovery(discoveryConfig)
	if err != nil {
		logger.Errorf("failed to init service discovery: %+v", err)
		return err
	}

	return nil
}

// startMonitor 启动监控进程
func (r *RuntimeCore) startMonitor() error {
	if !r.config.EnableMonitor {
		return nil
	}

	// TODO, Start Monitor

	return nil
}

// startHTTPServer 启动HTTP服务器
func (r *RuntimeCore) startHTTPServer() error {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// 添加中间件
	r.setupMiddlewares(engine)

	// 应用路由
	r.routerManager.ApplyRoutes(engine)

	// 注册健康检查和其他系统API
	r.registerSystemAPIs(engine)

	// 创建HTTP服务器
	r.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", r.config.Host, r.config.Port),
		Handler: engine,
	}

	// 在goroutine中启动服务器
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		logger.Infof("starting server on %s:%d", r.config.Host, r.config.Port)
		if err := r.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("failed to start HTTP server: %v", err)
		}
	}()

	return nil
}

// setupMiddlewares 设置中间件
func (r *RuntimeCore) setupMiddlewares(engine *gin.Engine) {
	// CORS中间件
	engine.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "*")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// 日志中间件
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"},
	}))

	// 恢复中间件
	engine.Use(gin.Recovery())
}

// registerSystemRoutes 注册系统路由
func (r *RuntimeCore) registerSystemRoutes() {
	// 这里可以添加系统级别的路由
}

// registerPluginRoutes 注册插件路由
func (r *RuntimeCore) registerPluginRoutes() {
	for name := range r.pluginManager.GetAllPlugins() {
		if routes := r.pluginManager.GetAPIRouter(name); len(routes) > 0 {
			r.routerManager.RegisterPluginRoutes(name, routes)
		}
	}
}

// registerPipelineRoutes 注册管道路由
func (r *RuntimeCore) registerPipelineRoutes() {
	for name, p := range r.pipelines {
		r.routerManager.RegisterPipelineRoutes(name, p)
	}
}

// registerSystemAPIs 注册系统API
func (r *RuntimeCore) registerSystemAPIs(engine *gin.Engine) {
	// 健康检查端点
	engine.GET("/health", func(c *gin.Context) {
		uptime := time.Since(r.startupTime).Seconds()

		pluginInfo := make(map[string]interface{})
		//if r.pluginManager != nil {
		//	for name, plugin := range r.pluginManager.GetAllPlugins() {
		//		pluginInfo[name] = map[string]interface{}{
		//			"state":   r.pluginManager.GetPluginState(name),
		//			"version": plugin.GetVersion(),
		//		}
		//	}
		//}

		pipelineNames := make([]string, 0, len(r.pipelines))
		for name := range r.pipelines {
			pipelineNames = append(pipelineNames, name)
		}

		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"uptime":      uptime,
			"plugins":     pluginInfo,
			"initialized": r.isInitialized.Load(),
			"pipelines":   pipelineNames,
		})
	})
}

// GetConfig 获取应用配置
func (r *RuntimeCore) GetConfig() *config.config {
	return r.config
}

// GetPluginManager 获取插件管理器
func (r *RuntimeCore) GetPluginManager() plugins.IPluginManager {
	return r.pluginManager
}

// GetPipelines 获取所有管道
func (r *RuntimeCore) GetPipelines() map[string]pipelines.IPipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]pipelines.IPipeline)
	for k, v := range r.pipelines {
		result[k] = v
	}
	return result
}
