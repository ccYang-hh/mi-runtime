package core

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/api"
	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/components/pools"
	"xfusion.com/tmatrix/runtime/pkg/config"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/discovery"
	"xfusion.com/tmatrix/runtime/pkg/metrics"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// RuntimeCore 运行时核心
// 负责协调系统的所有组件，包括插件、管道、API等
type RuntimeCore struct {
	// 上下文管理
	ctx            context.Context
	contextManager *rcx.ContextManager

	// 配置管理
	config                       *config.RuntimeConfig
	configManager                *config.Manager
	unsubscribeConfigManagerFunc config.UnsubscribeFunc

	// 业务流构建
	pluginManager   plugins.IPluginManager
	pipelineBuilder IPipelineBuilder
	routerManager   *RouterManager

	// 服务组件
	engine           *gin.Engine
	httpServer       *http.Server
	serviceDiscovery *discovery.EndpointService
	monitor          metrics.IMetricsMonitor

	// 状态管理
	readyPipelines map[string]pipelines.IPipeline
	isInitialized  atomic.Bool
	startupTime    time.Time

	// 并发控制
	wg sync.WaitGroup
	mu sync.RWMutex
}

// NewRuntimeCore 创建运行时核心
func NewRuntimeCore(configPath string) (*RuntimeCore, error) {
	contextManager := rcx.NewContextManager()
	runtimeCtx, _ := contextManager.GetRootContext()

	r := &RuntimeCore{
		ctx:            runtimeCtx,
		contextManager: contextManager,
		readyPipelines: make(map[string]pipelines.IPipeline),
	}

	// 初始化基础组件
	if err := r.initialize(configPath); err != nil {
		r.contextManager.Shutdown()
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

	logger.Infof("starting runtime %s %s", r.config.AppName, r.config.AppVersion)
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

	defer r.contextManager.Shutdown()
	defer r.isInitialized.Store(false)

	var err error

	// 逆序执行Shutdown
	// 关闭HTTP服务器
	if r.httpServer != nil {
		if err = r.httpServer.Shutdown(ctx); err != nil {
			logger.Errorf("failed to shutdown http server: %+v", err)
		}
	}

	// 关闭路由管理器
	if r.routerManager != nil {
		if err = r.routerManager.Shutdown(); err != nil {
			logger.Errorf("failed to close service discovery: %+v", err)
		}
	}

	// 关闭监控服务
	if r.monitor != nil {
		if err = r.monitor.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown runtime monitor: %+v", err)
		}
	}

	// 关闭服务发现
	if r.serviceDiscovery != nil {
		if err = r.serviceDiscovery.Close(); err != nil {
			logger.Errorf("failed to close service discovery: %+v", err)
		}
	}

	// 关闭服务发现
	if r.pipelineBuilder != nil {
		if err = r.pipelineBuilder.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown pipeline builder: %+v", err)
		}
	}

	// 关闭插件管理器
	if r.pluginManager != nil {
		if err = r.pluginManager.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown plugin manager: %+v", err)
		}
	}

	// 关闭配置管理器
	r.unsubscribeConfigManagerFunc()
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

	// 初始化配置管理器
	r.configManager, err = config.NewManager(r.ctx, configPath)
	if err != nil {
		logger.Errorf("failed to create config manager: %+v", err)
		return fmt.Errorf("failed to create config manager: %w", err)
	}

	// 订阅ConfigManager
	r.unsubscribeConfigManagerFunc = r.configManager.Subscribe(r.onConfigChange)

	// 获取应用配置
	r.config = r.configManager.GetConfig()

	return nil
}

func (r *RuntimeCore) onConfigChange(event *config.ConfigChangeEvent) {
	// 1.通知PluginManager
	runtimeConfig := event.Config
	if err := r.pluginManager.UpdatePluginRegistry(*runtimeConfig.PluginRegistry); err != nil {
		logger.Errorf("update plugin manager registry failed, err: %+v", err)
	}
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
	r.pluginManager = plugins.NewPluginManager(r.ctx, *r.config.PluginRegistry)
	//if err != nil {
	//	logger.Errorf("failed to create plugin manager: %+v", err)
	//	return fmt.Errorf("failed to create plugin manager: %w", err)
	//}

	// 注册PluginFactory
	//r.pluginManager.RegisterPluginFactory("example", func() Plugin[IPluginConfig] {
	//	return NewExamplePlugin()
	//})
	//
	//r.pluginManager.RegisterPluginFactory("example-daemon", func() Plugin[IPluginConfig] {
	//	return NewExampleDaemonPlugin()
	//})

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
	r.pipelineBuilder = NewPipelineBuilder(r.pluginManager, builderConfig)

	// 构建所有管道
	var err error
	r.readyPipelines, err = r.pipelineBuilder.BuildPipelines()
	if err != nil {
		logger.Errorf("failed to build pipelines: %+v", err)
		return fmt.Errorf("failed to build pipelines: %w", err)
	}

	logger.Infof("built %d pipelines successfully", len(r.readyPipelines))
	return nil
}

// createPipelineBuilderConfig 创建管道构建器配置
func (r *RuntimeCore) createPipelineBuilderConfig() *PipelineBuilderConfig {
	// 默认管道配置
	var pipelinePlugins = make([]interface{}, 0)
	pipelinePlugins = append(pipelinePlugins, "request_analyzer", "vllm_router", "request_processor")
	defaultPipeline := &config.PipelineConfig{
		PipelineName: "default",
		Plugins:      pipelinePlugins,
		Routes: []common.RouteInfo{
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

	return &PipelineBuilderConfig{
		Pipelines: registerPipelines,
		Default:   defaultPipeline,
	}
}

// setupRoutes 设置所有路由
func (r *RuntimeCore) setupRoutes() error {
	// 创建Dispatcher
	dispatcher := NewRequestDispatcher(r.ctx, r.contextManager, pools.WorkerPoolConfig{
		Name:            "pipeline_dispatcher",
		MinWorkers:      10,
		MaxWorkers:      500,
		QueueSize:       1000,
		MonitorInterval: 30 * time.Second,
	}, 0)

	// 创建routerManager
	r.routerManager = NewRouterManager(r.engine, dispatcher, RouterManagerConfig{})

	// 初始化routerManager
	if err := r.routerManager.Initialize(); err != nil {
		return err
	}

	// 注册模块级的系统路由
	r.registerSystemRoutes()

	// 注册插件路由
	r.registerPluginRoutes()

	// 注册管道路由
	r.registerPipelineRoutes()

	// 注册非模块级的系统路由
	r.registerSystemAPIs()

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
		discoveryConfig.ETCD = &discovery.ETCDConfig{
			Endpoints: []string{
				fmt.Sprintf("%s:%s", r.config.ETCD.Host, strconv.Itoa(r.config.ETCD.Port)),
			},
			Prefix: "/tmatrix/runtime/endpoints",
		}
	case discovery.ServiceDiscoveryTypeK8S:
		// TODO
		//  Support K8S
	default:
		logger.Errorf("unsupported service discovery type: %s", r.config.ServiceDiscovery)
		return fmt.Errorf("unsupported service discovery type: %s", r.config.ServiceDiscovery)
	}

	// 2.构造ServiceDiscovery实例
	serviceDiscovery, err := discovery.NewServiceDiscovery(discoveryConfig)
	if err != nil {
		logger.Errorf("failed to init service discovery: %+v", err)
		return err
	}

	// 3.构造Endpoint Service
	r.serviceDiscovery = discovery.NewEndpointService(serviceDiscovery)

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
	r.engine = gin.New()

	// 添加中间件
	r.setupMiddlewares(r.engine)

	// 应用路由
	if err := r.setupRoutes(); err != nil {
		return err
	}

	// 创建HTTP服务器
	r.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", r.config.Host, r.config.Port),
		Handler: r.engine,
	}

	// 启动服务监听
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
	providers := []api.RouteProvider{
		api.NewServiceDiscoveryProvider(r.serviceDiscovery),
	}

	for _, provider := range providers {
		err := r.routerManager.RegisterSystemProvider(provider)
		if err != nil {
			logger.Errorf("register system provider err, %+v", err)
		}
	}
}

// registerPluginRoutes 注册插件路由
func (r *RuntimeCore) registerPluginRoutes() {
	// TODO
}

// registerPipelineRoutes 注册管道路由
func (r *RuntimeCore) registerPipelineRoutes() {
	for _, p := range r.readyPipelines {
		err := r.routerManager.RegisterPipelineProvider(p)
		if err != nil {
			logger.Errorf("register pipeline router err, %+v", err)
		}
	}
}

// registerSystemAPIs 注册系统API
func (r *RuntimeCore) registerSystemAPIs() {
	// 健康检查端点
	r.engine.GET("/health", func(c *gin.Context) {
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

		pipelineNames := make([]string, 0, len(r.readyPipelines))
		for name := range r.readyPipelines {
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
func (r *RuntimeCore) GetConfig() *config.RuntimeConfig {
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
	for k, v := range r.readyPipelines {
		result[k] = v
	}
	return result
}
