package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	. "xfusion.com/tmatrix/runtime/pkg/config/source"
)

var (
	managerInstance *Manager
	managerOnce     sync.Once
)

// ConfigChangeEvent 配置变更事件
type ConfigChangeEvent struct {
	Source    string         `json:"source"`
	Timestamp time.Time      `json:"timestamp"`
	Config    *RuntimeConfig `json:"config"`
}

// ConfigChangeHandler 配置变更处理函数
type ConfigChangeHandler func(*ConfigChangeEvent)

// UnsubscribeFunc 取消订阅函数
type UnsubscribeFunc func()

// Manager 运行时配置管理器
type Manager struct {
	configPath string
	source     Source[RuntimeConfig]
	validator  *ConfigValidator

	// 当前配置
	config     *RuntimeConfig
	configLock sync.RWMutex

	// 变更通知
	changeHandlers map[int]ConfigChangeHandler // 使用map存储，key为ID
	nextHandlerID  int                         // 指向下一个HandlerID
	handlerLock    sync.RWMutex
	changeChan     chan *ConfigChangeEvent // 订阅事件缓存通道

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// GetManager 获取配置管理器单例
func GetManager(rootCtx context.Context, configPath string) *Manager {
	managerOnce.Do(func() {
		var err error
		managerInstance, err = NewManager(rootCtx, configPath)
		if err != nil {
			logger.Errorf("failed to create config manager, %+v", err)
			panic(fmt.Sprintf("failed to create config manager: %+v", err))
		}
	})
	return managerInstance
}

// NewManager 创建新的配置管理器
func NewManager(rootCtx context.Context, configPath string) (*Manager, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	// 创建配置源
	source := NewFileSource[RuntimeConfig]("runtime", configPath)
	validator := NewConfigValidator()

	ctx, cancel := context.WithCancel(rootCtx)

	manager := &Manager{
		configPath:     configPath,
		source:         source,
		validator:      validator,
		nextHandlerID:  1,
		changeHandlers: make(map[int]ConfigChangeHandler),
		changeChan:     make(chan *ConfigChangeEvent, 10),
		ctx:            ctx,
		cancel:         cancel,
	}

	// 初始加载配置
	if err := manager.loadConfig(); err != nil {
		cancel()
		logger.Errorf("failed to load initial config: %+v", err)
		return nil, fmt.Errorf("failed to load initial config: %+v", err)
	}

	// 启动配置监控
	if err := manager.startWatching(); err != nil {
		cancel()
		logger.Errorf("failed to start config watching: %+v", err)
		return nil, fmt.Errorf("failed to start config watching: %+v", err)
	}

	// 启动事件处理协程
	manager.wg.Add(1)
	go manager.eventLoop()

	logger.Infof("config manager initialized, config_path: %s", configPath)
	return manager, nil
}

// GetConfig 获取当前配置（只读副本）
func (m *Manager) GetConfig() *RuntimeConfig {
	m.configLock.RLock()
	defer m.configLock.RUnlock()

	if m.closed {
		logger.Error("manager is closed")
		return nil
	}

	// 返回深拷贝以确保线程安全
	return m.deepCopyConfig(m.config)
}

// Reload 手动重新加载配置
func (m *Manager) Reload() (*RuntimeConfig, error) {
	if m.closed {
		return nil, fmt.Errorf("manager is closed")
	}

	if err := m.loadConfig(); err != nil {
		return nil, err
	}

	return m.GetConfig(), nil
}

// Subscribe 订阅配置变更事件
func (m *Manager) Subscribe(handler ConfigChangeHandler) UnsubscribeFunc {
	if m.closed {
		logger.Error("cannot subscribe to closed manager")
		return func() {} // 返回空的取消函数
	}

	m.handlerLock.Lock()
	defer m.handlerLock.Unlock()

	// 分配唯一ID
	handlerID := m.nextHandlerID
	m.nextHandlerID++

	// 存储Handler
	m.changeHandlers[handlerID] = handler
	logger.Debugf("config change handler subscribed, handlers_count: %d", len(m.changeHandlers))

	// 返回取消函数
	return func() {
		m.unsubscribeByID(handlerID)
	}
}

// 内部方法：通过ID取消订阅
func (m *Manager) unsubscribeByID(handlerID int) {
	if m.closed {
		return
	}

	m.handlerLock.Lock()
	defer m.handlerLock.Unlock()

	if _, exists := m.changeHandlers[handlerID]; exists {
		delete(m.changeHandlers, handlerID)
		logger.Infof("config change handler unsubscribed, id: %d, handlers_count: %d",
			handlerID, len(m.changeHandlers))
	}
}

// Close 关闭配置管理器
func (m *Manager) Close() error {
	m.configLock.Lock()
	defer m.configLock.Unlock()

	if m.closed {
		return nil
	}

	logger.Info("closing config manager")

	// 停止配置监控
	if err := m.source.Stop(); err != nil {
		logger.Errorf("failed to stop config source, err: %+v", err)
	}

	// 取消上下文，通知所有协程退出
	m.cancel()

	// 关闭事件通道
	close(m.changeChan)

	// 等待所有协程结束
	m.wg.Wait()

	m.closed = true
	logger.Info("config manager closed")
	return nil
}

// loadConfig 加载配置
func (m *Manager) loadConfig() error {
	// 从配置源加载
	newConfig, err := m.source.Load(m.ctx)
	if err != nil {
		logger.Errorf("failed to load config from source, err: %+v", err)
		return fmt.Errorf("failed to load config: %+v", err)
	}

	// 验证配置
	if err := m.validator.Validate(newConfig); err != nil {
		logger.Errorf("config validation failed, err: %+v", err)
		return fmt.Errorf("config validation failed: %+v", err)
	}

	// 更新当前配置
	m.configLock.Lock()
	oldConfig := m.config
	m.config = newConfig
	m.configLock.Unlock()

	// 如果不是首次加载，发送变更事件
	if oldConfig != nil {
		event := &ConfigChangeEvent{
			Source:    "file",
			Timestamp: time.Now(),
			Config:    m.deepCopyConfig(newConfig),
		}

		select {
		case m.changeChan <- event:
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
			logger.Warn("config change event channel is full, dropping event")
		}
	}

	logger.Info("config loaded successfully")
	return nil
}

// startWatching 启动配置文件监控
func (m *Manager) startWatching() error {
	return m.source.Watch(m.ctx, func(newConfig *RuntimeConfig) {
		// 验证新配置
		if err := m.validator.Validate(newConfig); err != nil {
			logger.Errorf("new config validation failed, err: %+v", err)
			return
		}

		// 更新配置
		m.configLock.Lock()
		m.config = newConfig
		m.configLock.Unlock()

		// 发送变更事件
		event := &ConfigChangeEvent{
			Source:    "file_watch",
			Timestamp: time.Now(),
			Config:    m.deepCopyConfig(newConfig),
		}

		select {
		case m.changeChan <- event:
			logger.Info("config change detected and processed")
		case <-m.ctx.Done():
			return
		default:
			logger.Warn("config change event channel is full, dropping event")
		}
	})
}

// eventLoop 事件处理循环
func (m *Manager) eventLoop() {
	defer m.wg.Done()

	for {
		select {
		case event, ok := <-m.changeChan:
			if !ok {
				logger.Debug("config change channel closed, exiting event loop")
				return
			}

			// 分发事件给所有订阅者
			m.handlerLock.RLock()
			m.handlerLock.RUnlock()

			for id, handler := range m.changeHandlers {
				// 异步调用处理器，避免阻塞
				go func(handlerID int, h ConfigChangeHandler) {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("config change handler %d panicked, err: %+v", handlerID, r)
						}
					}()
					h(event)
				}(id, handler)
			}

		case <-m.ctx.Done():
			logger.Debug("context cancelled, exiting event loop")
			return
		}
	}
}

// deepCopyConfig 深拷贝配置对象
func (m *Manager) deepCopyConfig(config *RuntimeConfig) *RuntimeConfig {
	if config == nil {
		return nil
	}

	// 创建新的配置对象
	newConfig := &RuntimeConfig{
		AppName:          config.AppName,
		AppVersion:       config.AppVersion,
		AppDescription:   config.AppDescription,
		Host:             config.Host,
		Port:             config.Port,
		ServiceDiscovery: config.ServiceDiscovery,
		MaxBatchSize:     config.MaxBatchSize,
		RequestTimeout:   config.RequestTimeout,
		EnableMonitor:    config.EnableMonitor,
		MonitorConfig:    config.MonitorConfig,
		EnableRouter:     config.EnableRouter,
		EnableAuth:       config.EnableAuth,
	}

	// 深拷贝切片
	if config.CorsOrigins != nil {
		newConfig.CorsOrigins = make([]string, len(config.CorsOrigins))
		copy(newConfig.CorsOrigins, config.CorsOrigins)
	}

	// 深拷贝插件注册表
	if config.PluginRegistry != nil {
		newConfig.PluginRegistry = &PluginRegistryConfig{
			AutoDiscovery: config.PluginRegistry.AutoDiscovery,
			Plugins:       make(map[string]*PluginInfo),
		}

		if config.PluginRegistry.DiscoveryPaths != nil {
			newConfig.PluginRegistry.DiscoveryPaths = make([]string, len(config.PluginRegistry.DiscoveryPaths))
			copy(newConfig.PluginRegistry.DiscoveryPaths, config.PluginRegistry.DiscoveryPaths)
		}

		for name, plugin := range config.PluginRegistry.Plugins {
			newConfig.PluginRegistry.Plugins[name] = &PluginInfo{
				Enabled:    plugin.Enabled,
				Priority:   plugin.Priority,
				ConfigPath: plugin.ConfigPath,
			}
		}
	}

	// 深拷贝管道配置
	if config.Pipelines != nil {
		newConfig.Pipelines = make([]*PipelineConfig, len(config.Pipelines))
		for i, pipeline := range config.Pipelines {
			newPipeline := &PipelineConfig{
				PipelineName:   pipeline.PipelineName,
				Mode:           pipeline.Mode,
				MaxConcurrency: pipeline.MaxConcurrency,
			}

			if pipeline.Plugins != nil {
				newPipeline.Plugins = make([]interface{}, len(pipeline.Plugins))
				copy(newPipeline.Plugins, pipeline.Plugins)
			}

			if pipeline.Routes != nil {
				newPipeline.Routes = make([]common.RouteInfo, len(pipeline.Routes))
				for j, route := range pipeline.Routes {
					newPipeline.Routes[j] = common.RouteInfo{
						Path:   route.Path,
						Method: route.Method,
					}
				}
			}

			newConfig.Pipelines[i] = newPipeline
		}
	}

	// 深拷贝ETCD配置
	if config.ETCD != nil {
		newConfig.ETCD = &ETCDConfig{
			Host: config.ETCD.Host,
			Port: config.ETCD.Port,
		}
	}

	// 深拷贝认证配置
	if config.AuthConfig != nil {
		newConfig.AuthConfig = make(map[string]any)
		for k, v := range config.AuthConfig {
			newConfig.AuthConfig[k] = v
		}
	}

	return newConfig
}
