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

// Manager 运行时配置管理器
type Manager struct {
	configPath string
	source     Source[RuntimeConfig]
	validator  *ConfigValidator

	// 当前配置
	config     *RuntimeConfig
	configLock sync.RWMutex

	// 变更通知
	changeHandlers []ConfigChangeHandler
	handlerLock    sync.RWMutex
	changeChan     chan *ConfigChangeEvent

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// GetManager 获取配置管理器单例
func GetManager(configPath string) *Manager {
	managerOnce.Do(func() {
		var err error
		managerInstance, err = NewManager(configPath)
		if err != nil {
			logger.Errorf("failed to create config manager, %+v", err)
			panic(fmt.Sprintf("failed to create config manager: %+v", err))
		}
	})
	return managerInstance
}

// NewManager 创建新的配置管理器
func NewManager(configPath string) (*Manager, error) {
	if configPath == "" {
		configPath = "config.yaml"
	}

	// 创建配置源
	source := NewFileSource[RuntimeConfig]("runtime", configPath)
	validator := NewConfigValidator()

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		configPath:     configPath,
		source:         source,
		validator:      validator,
		changeHandlers: make([]ConfigChangeHandler, 0),
		changeChan:     make(chan *ConfigChangeEvent, 10),
		ctx:            ctx,
		cancel:         cancel,
	}

	// 初始加载配置
	if err := manager.loadConfig(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load initial config: %+v", err)
	}

	// 启动配置监控
	if err := manager.startWatching(); err != nil {
		cancel()
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
func (m *Manager) Subscribe(handler ConfigChangeHandler) {
	if m.closed {
		logger.Error("cannot subscribe to closed manager")
		return
	}

	m.handlerLock.Lock()
	defer m.handlerLock.Unlock()

	m.changeHandlers = append(m.changeHandlers, handler)
	logger.Debugf("config change handler subscribed, handlers_count: %d", len(m.changeHandlers))
}

// Unsubscribe 取消订阅配置变更事件
func (m *Manager) Unsubscribe(handler ConfigChangeHandler) {
	if m.closed {
		return
	}

	m.handlerLock.Lock()
	defer m.handlerLock.Unlock()

	// 通过函数指针比较来移除处理器（Go的限制，这里简化处理）
	// 实际使用中建议返回取消函数或使用ID标识
	logger.Debugf("unsubscribe called, handlers_count: %d", len(m.changeHandlers))
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
			handlers := make([]ConfigChangeHandler, len(m.changeHandlers))
			copy(handlers, m.changeHandlers)
			m.handlerLock.RUnlock()

			for _, handler := range handlers {
				// 异步调用处理器，避免阻塞
				go func(h ConfigChangeHandler) {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("config change handler panicked, err: %+v", r)
						}
					}()
					h(event)
				}(handler)
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
				Module:     plugin.Module,
				Path:       plugin.Path,
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
				PipelineName: pipeline.PipelineName,
			}

			if pipeline.Plugins != nil {
				newPipeline.Plugins = make([]string, len(pipeline.Plugins))
				copy(newPipeline.Plugins, pipeline.Plugins)
			}

			if pipeline.Routes != nil {
				newPipeline.Routes = make([]*common.RouteInfo, len(pipeline.Routes))
				for j, route := range pipeline.Routes {
					newPipeline.Routes[j] = &common.RouteInfo{
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
