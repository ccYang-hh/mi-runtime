package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

var _ IPluginManager = (*PluginManager)(nil)

type IPluginManager interface {
	// Start 启动PluginManager
	Start() error

	// Shutdown 终止PluginManager
	Shutdown() error

	// RegisterPlugins 注册Plugins
	RegisterPlugins(plugins []string) error

	// UnregisterPlugins 注销Plugins
	UnregisterPlugins(plugins []string) error

	// UpdatePluginRegistry 更新Plugin注册表
	UpdatePluginRegistry(plugins config.PluginRegistryConfig) error

	// InitializePlugins 初始化所有Plugin（非Daemon模式的Plugin）
	InitializePlugins() error

	// InitializeDaemonPlugins 初始化所有Daemon Plugins
	InitializeDaemonPlugins() error

	// GetPlugin 获取Plugin
	GetPlugin(pluginName string) Plugin

	// GetPluginState 获取Plugin状态
	GetPluginState(pluginName string) PluginState

	// GetAllPluginState 获取所有Plugin状态
	GetAllPluginState() map[string]PluginState

	// GetPluginAPIRouter 获取指定Plugin的Router
	GetPluginAPIRouter(pluginName string) *gin.RouterGroup

	// GetPipelineStages 插件提供的Stages
	GetPipelineStages(pluginName string) []pipelines.Stage

	// GetPipelineHooks 插件提供的Pipeline Hooks
	GetPipelineHooks(pluginName string) []pipelines.PipelineHook

	// GetStageHooks 插件提供的Stage Hooks
	GetStageHooks(pluginName string) []pipelines.StageHook

	// RegisterPluginFactory 注册PluginFactory
	RegisterPluginFactory(pluginType string, factory PluginFactory) error

	// RegisterPluginType 注册插件类型
	RegisterPluginType(pluginType string, pluginStruct interface{}) error

	// RegisterPluginConstructor 注册插件构造函数
	RegisterPluginConstructor(pluginType string, constructor PluginConstructor) error

	// LoadPluginFromFile 从文件动态加载插件
	LoadPluginFromFile(pluginPath string) error

	// ReloadPlugin 重新加载插件
	ReloadPlugin(pluginName string) error

	// GetRegisteredPluginTypes 查询已注册的Plugin Type
	GetRegisteredPluginTypes() []string
}

// PluginManager 插件管理器实现
type PluginManager struct {
	plugins        map[string]Plugin
	pluginStates   map[string]PluginState
	registryConfig config.PluginRegistryConfig

	// 依赖注入的组件
	registry    IPluginTypeRegistry
	initializer IPluginInitializer

	// 并发控制
	mu sync.RWMutex

	// 生命周期控制
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

// NewPluginManager 创建插件管理器
func NewPluginManager(runtimeCtx context.Context, config config.PluginRegistryConfig) *PluginManager {
	// 绑定Context
	ctx, cancel := context.WithCancel(runtimeCtx)

	// 插件注册表和初始化器
	registry := NewPluginTypeRegistry()
	initializer := NewPluginInitializer(registry)

	manager := &PluginManager{
		ctx:            ctx,
		cancel:         cancel,
		registry:       registry,
		initializer:    initializer,
		registryConfig: config,
		plugins:        make(map[string]Plugin),
		pluginStates:   make(map[string]PluginState),
	}

	return manager
}

// NewPluginManagerWithDependencies 使用自定义依赖创建插件管理器
func NewPluginManagerWithDependencies(
	runtimeCtx context.Context, config config.PluginRegistryConfig,
	registry IPluginTypeRegistry, initializer IPluginInitializer) *PluginManager {

	// 绑定Context
	ctx, cancel := context.WithCancel(runtimeCtx)

	// 设置注册表
	initializer.SetRegistry(registry)

	manager := &PluginManager{
		ctx:            ctx,
		cancel:         cancel,
		registry:       registry,
		initializer:    initializer,
		registryConfig: config,
		plugins:        make(map[string]Plugin),
		pluginStates:   make(map[string]PluginState),
	}

	return manager
}

func (pm *PluginManager) Start() error {

	if pm.started {
		logger.Warnf("PluginManager already started")
		return nil
	}

	pm.mu.Lock()
	pm.started = true
	pm.mu.Unlock()

	// 启动时，立即更新一次注册表
	if err := pm.UpdatePluginRegistry(pm.registryConfig); err != nil {
		logger.Errorf("update plugin registry err: %+v", err)
		return err
	}

	// 初始化Plugins
	if err := pm.InitializePlugins(); err != nil {
		logger.Errorf("initialize plugins err: %+v", err)
		return err
	}

	// 初始化Daemon Plugins
	if err := pm.InitializeDaemonPlugins(); err != nil {
		logger.Errorf("initialize daemon plugins err: %+v", err)
		return err
	}

	logger.Infof("PluginManager started with support for modes: %+v", pm.initializer.SupportedModes())
	return nil
}

func (pm *PluginManager) Shutdown() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		return nil
	}

	// 关闭所有插件
	var lastErr error
	for name, plugin := range pm.plugins {
		logger.Infof("Shutting down plugin %s", name)

		if err := plugin.Shutdown(pm.ctx); err != nil {
			logger.Errorf("Failed to shutdown plugin %s: %v", name, err)
			lastErr = err
			pm.setPluginState(name, PluginError)
		} else {
			pm.setPluginState(name, PluginStopped)
		}
	}

	pm.cancel()
	pm.started = false

	logger.Infof("PluginManager shutdown completed")

	if lastErr != nil {
		return fmt.Errorf("some plugins failed to shutdown: %w", lastErr)
	}

	return nil
}

func (pm *PluginManager) RegisterPlugins(pluginNames []string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, name := range pluginNames {
		// 已注册，跳过
		if _, exists := pm.plugins[name]; exists {
			logger.Warnf("Plugin %s already registered", name)
			continue
		}

		// 从配置获取插件信息
		pluginInfo, exists := pm.registryConfig.Plugins[name]
		if !exists {
			logger.Errorf("No config found for plugin %s", name)
			continue
		}

		// 使用初始化器创建插件实例
		plugin, err := pm.initializer.Initialize(pm.ctx, pluginInfo.InitMode, pluginInfo)
		if err != nil {
			logger.Errorf("Failed to initialize plugin %s: %v", name, err)
			return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
		}

		pm.plugins[name] = plugin
		pm.setPluginState(name, PluginInitialized)

		logger.Infof("Plugin %s registered successfully using %s mode", name, pluginInfo.InitMode)
	}

	return nil
}

func (pm *PluginManager) UnregisterPlugins(pluginNames []string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, name := range pluginNames {
		plugin, exists := pm.plugins[name]
		if !exists {
			logger.Warnf("Plugin %s not found for unregistration", name)
			continue
		}

		// 关闭插件
		if err := plugin.Shutdown(pm.ctx); err != nil {
			logger.Errorf("Failed to shutdown plugin %s during unregistration: %v", name, err)
			pm.setPluginState(name, PluginError)
		} else {
			delete(pm.plugins, name)
			delete(pm.pluginStates, name)
			logger.Infof("Plugin %s unregistered successfully", name)
		}
	}

	return nil
}

// UpdatePluginRegistry 当配置文件更新时，热更新Runtime的Plugin配置，包括加载新的Plugin，或卸载Plugin
// TODO
//
//	1.Plugin的新增或卸载应同步热更新Pipeline，当前还未实现！！
func (pm *PluginManager) UpdatePluginRegistry(config config.PluginRegistryConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		logger.Warnf("plugin manager not started yet")
		return nil
	}

	oldConfig := pm.registryConfig
	pm.registryConfig = config

	// 处理插件启用/禁用状态变更
	for pluginName, pluginInfo := range config.Plugins {
		if !pluginInfo.Enabled {
			// 如果插件被禁用，且当前已注册，则注销
			if _, exists := pm.plugins[pluginName]; exists {
				logger.Infof("Disabling plugin %s", pluginName)
				if err := pm.unregisterSinglePlugin(pluginName); err != nil {
					logger.Errorf("Failed to disable plugin %s: %+v", pluginName, err)
				}
			}
			continue
		}

		// 如果插件被启用，且当前未注册，则注册
		if _, exists := pm.plugins[pluginName]; !exists {
			logger.Infof("Enabling plugin %s using %s mode", pluginName, pluginInfo.InitMode)
			if err := pm.registerSinglePlugin(pluginName, pluginInfo); err != nil {
				logger.Errorf("Failed to enable plugin %s: %+v", pluginName, err)
				continue
			}
		}

		// 检查已注册插件的关键配置是否变更
		if oldPluginInfo, exists := oldConfig.Plugins[pluginName]; exists {
			if pm.shouldReloadPlugin(oldPluginInfo, pluginInfo) {
				logger.Infof("Plugin config changed for %s, reloading", pluginName)
				if err := pm.reloadSinglePlugin(pluginName, pluginInfo); err != nil {
					logger.Errorf("Failed to reload plugin %s: %+v", pluginName, err)
					continue
				}
			}
		}
	}

	// 处理已删除的插件
	for pluginName := range oldConfig.Plugins {
		if _, exists := config.Plugins[pluginName]; !exists {
			logger.Infof("Plugin %s removed from config, unregistering", pluginName)
			if err := pm.unregisterSinglePlugin(pluginName); err != nil {
				logger.Errorf("Failed to unregister removed plugin %s: %+v", pluginName, err)
			}
		}
	}

	logger.Infof("Plugin registry updated successfully")
	return nil
}

// shouldReloadPlugin 判断是否需要重新加载插件
func (pm *PluginManager) shouldReloadPlugin(oldInfo, newInfo *config.PluginInfo) bool {
	return oldInfo.ConfigPath != newInfo.ConfigPath ||
		oldInfo.InitMode != newInfo.InitMode ||
		oldInfo.PluginPath != newInfo.PluginPath ||
		oldInfo.ConstructorName != newInfo.ConstructorName ||
		oldInfo.PackagePath != newInfo.PackagePath ||
		oldInfo.StructName != newInfo.StructName
}

func (pm *PluginManager) registerSinglePlugin(pluginName string, info *config.PluginInfo) error {
	// 从工厂创建插件实例
	p, err := pm.initializer.Initialize(pm.ctx, info.InitMode, info)
	if err != nil {
		return fmt.Errorf("failed to initialize plugin instance: %w", err)
	}

	pm.plugins[pluginName] = p
	pm.setPluginState(pluginName, PluginInitialized)

	return nil
}

func (pm *PluginManager) unregisterSinglePlugin(pluginName string) error {
	plugin, exists := pm.plugins[pluginName]
	if !exists {
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	if err := plugin.Shutdown(pm.ctx); err != nil {
		pm.setPluginState(pluginName, PluginError)
		return err
	}

	delete(pm.plugins, pluginName)
	delete(pm.pluginStates, pluginName)

	return nil
}

func (pm *PluginManager) reloadSinglePlugin(pluginName string, info *config.PluginInfo) error {
	// 先关闭旧插件
	if err := pm.unregisterSinglePlugin(pluginName); err != nil {
		return fmt.Errorf("failed to unregister old plugin: %w", err)
	}

	// 注册新插件
	return pm.registerSinglePlugin(pluginName, info)
}

func (pm *PluginManager) InitializePlugins() error {
	pm.mu.RLock()
	regularPlugins := make([]string, 0)
	pluginConfigs := make(map[string]*config.PluginInfo)

	for name, plugin := range pm.plugins {
		if !plugin.IsDaemonPlugin() {
			regularPlugins = append(regularPlugins, name)
			if info, exists := pm.registryConfig.Plugins[name]; exists {
				pluginConfigs[name] = info
			}
		}
	}
	pm.mu.RUnlock()

	for _, name := range regularPlugins {
		plugin := pm.plugins[name]

		// 获取插件配置信息
		pluginInfo, exists := pluginConfigs[name]
		if !exists {
			logger.Warnf("No config found for plugin %s, using default", name)
		}

		logger.Infof("Initializing regular plugin %s", name)

		if err := plugin.Initialize(pm.ctx, pluginInfo); err != nil {
			logger.Errorf("Failed to initialize plugin %s: %+v", name, err)
			pm.setPluginState(name, PluginError)
			return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
		}

		pm.setPluginState(name, PluginRunning)
		logger.Infof("Plugin %s initialized successfully", name)
	}

	logger.Infof("All regular plugins initialized successfully")
	return nil
}

func (pm *PluginManager) InitializeDaemonPlugins() error {
	pm.mu.RLock()
	daemonPlugins := make([]string, 0)
	pluginConfigs := make(map[string]*config.PluginInfo)

	for name, plugin := range pm.plugins {
		if plugin.IsDaemonPlugin() {
			daemonPlugins = append(daemonPlugins, name)
			if info, exists := pm.registryConfig.Plugins[name]; exists {
				pluginConfigs[name] = info
			}
		}
	}
	pm.mu.RUnlock()

	for _, name := range daemonPlugins {
		plugin := pm.plugins[name]

		// 获取插件配置信息
		pluginInfo, exists := pluginConfigs[name]
		if !exists {
			logger.Warnf("No config found for daemon plugin %s, using default", name)
		}

		// 获取插件配置目录
		//configDirs := pm.getPluginConfigDirs(name, pluginInfo)

		logger.Infof("Initializing daemon plugin %s", name)

		if err := plugin.Initialize(pm.ctx, pluginInfo); err != nil {
			logger.Errorf("Failed to initialize daemon plugin %s: %+v", name, err)
			pm.setPluginState(name, PluginError)
			return fmt.Errorf("failed to initialize daemon plugin %s: %w", name, err)
		}

		// 启动Daemon处理器
		processor := plugin.GetDaemonProcessor()
		if processor != nil {
			logger.Infof("Starting daemon processor for plugin %s", name)

			if err := processor.Start(pm.ctx); err != nil {
				logger.Errorf("Failed to start daemon processor for plugin %s: %+v", name, err)
				pm.setPluginState(name, PluginError)
				return fmt.Errorf("failed to start daemon processor for plugin %s: %w", name, err)
			}

			// 异步监控Daemon状态
			go pm.monitorDaemonPlugin(name, plugin, processor)
		} else {
			logger.Warnf("Daemon plugin %s has no daemon processor", name)
		}

		pm.setPluginState(name, PluginRunning)
		logger.Infof("Daemon plugin %s initialized and started successfully", name)
	}

	logger.Infof("All daemon plugins initialized successfully")
	return nil
}

func (pm *PluginManager) monitorDaemonPlugin(pluginName string, plugin Plugin,
	processor PluginDaemonProcessor) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			if !processor.IsRunning() {
				logger.Errorf("Daemon processor for plugin %s is not running", pluginName)
				pm.setPluginState(pluginName, PluginError)

				// 自动重启
				logger.Infof("Attempting to restart daemon processor for plugin %s", pluginName)
				if err := processor.Start(pm.ctx); err != nil {
					logger.Errorf("Failed to restart daemon processor for plugin %s: %+v", pluginName, err)
				} else {
					logger.Infof("Successfully restarted daemon processor for plugin %s", pluginName)
					pm.setPluginState(pluginName, PluginRunning)
				}
			}
		}
	}
}

// getPluginConfigDirs 获取插件配置目录
// 如果ConfigPath为空，则返回空切片，表示插件使用内部静态配置
func (pm *PluginManager) getPluginConfigDirs(pluginName string, pluginInfo *config.PluginInfo) []string {
	if pluginInfo.ConfigPath == "" {
		// ConfigPath为空表示插件使用内部静态配置，不需要外部配置目录
		logger.Debugf("Plugin %s uses internal static configuration", pluginName)
		return []string{}
	}

	// ConfigPath不为空表示插件需要外部文件配置源
	logger.Debugf("Plugin %s uses external file configuration from: %s", pluginName, pluginInfo.ConfigPath)
	return []string{pluginInfo.ConfigPath}
}

func (pm *PluginManager) GetPlugin(pluginName string) Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.plugins[pluginName]
}

func (pm *PluginManager) GetPluginState(pluginName string) PluginState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if state, exists := pm.pluginStates[pluginName]; exists {
		return state
	}

	return PluginStopped
}

func (pm *PluginManager) GetAllPluginState() map[string]PluginState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]PluginState)
	for name, state := range pm.pluginStates {
		result[name] = state
	}

	return result
}

func (pm *PluginManager) GetPluginAPIRouter(pluginName string) *gin.RouterGroup {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if plugin, exists := pm.plugins[pluginName]; exists {
		return plugin.GetAPIRouter()
	}

	return nil
}

func (pm *PluginManager) GetPipelineStages(pluginName string) []pipelines.Stage {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if plugin, exists := pm.plugins[pluginName]; exists {
		return plugin.GetPipelineStages()
	}

	return []pipelines.Stage{}
}

func (pm *PluginManager) GetPipelineHooks(pluginName string) []pipelines.PipelineHook {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if plugin, exists := pm.plugins[pluginName]; exists {
		return plugin.GetPipelineHooks()
	}

	return []pipelines.PipelineHook{}
}

func (pm *PluginManager) GetStageHooks(pluginName string) []pipelines.StageHook {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if plugin, exists := pm.plugins[pluginName]; exists {
		return plugin.GetStageHooks()
	}

	return []pipelines.StageHook{}
}

// RegisterPluginFactory 注册PluginFactory
func (pm *PluginManager) RegisterPluginFactory(pluginType string, factory PluginFactory) error {
	return pm.registry.RegisterFactory(pluginType, factory)
}

// RegisterPluginType 注册PluginType
func (pm *PluginManager) RegisterPluginType(pluginType string, pluginStruct interface{}) error {
	return pm.registry.RegisterType(pluginType, pluginStruct)
}

// RegisterPluginConstructor 注册PluginConstructor
func (pm *PluginManager) RegisterPluginConstructor(pluginType string, constructor PluginConstructor) error {
	return pm.registry.RegisterConstructor(pluginType, constructor)
}

func (pm *PluginManager) LoadPluginFromFile(pluginPath string) error {
	return pm.registry.LoadPluginFromFile(pluginPath)
}

func (pm *PluginManager) ReloadPlugin(pluginName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginInfo, exists := pm.registryConfig.Plugins[pluginName]
	if !exists {
		logger.Errorf("Plugin %s does not exist", pluginName)
		return fmt.Errorf("no config found for plugin %s", pluginName)
	}

	return pm.reloadSinglePlugin(pluginName, pluginInfo)
}

// GetRegistry 获取类型注册表
func (pm *PluginManager) GetRegistry() IPluginTypeRegistry {
	return pm.registry
}

// GetInitializer 获取初始化器
func (pm *PluginManager) GetInitializer() IPluginInitializer {
	return pm.initializer
}

// GetRegisteredPluginTypes 获取已注册的插件类型
func (pm *PluginManager) GetRegisteredPluginTypes() []string {
	return pm.registry.GetRegisteredTypes()
}

// setPluginState 设置插件状态（内部方法）
func (pm *PluginManager) setPluginState(pluginName string, state PluginState) {
	pm.pluginStates[pluginName] = state
	logger.Debugf("Plugin %s state changed to %s", pluginName, state)
}

// GetRegistryConfig 获取注册表配置
func (pm *PluginManager) GetRegistryConfig() config.PluginRegistryConfig {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.registryConfig
}

// GetPluginCount 获取插件数量统计
func (pm *PluginManager) GetPluginCount() map[string]int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := map[string]int{
		"total":                len(pm.plugins),
		"regular":              0,
		"daemon":               0,
		"running":              0,
		"error":                0,
		"stopped":              0,
		"initialized":          0,
		"with_external_config": 0,
		"with_internal_config": 0,
	}

	for name, plugin := range pm.plugins {
		if plugin.IsDaemonPlugin() {
			result["daemon"]++
		} else {
			result["regular"]++
		}

		// 统计配置类型
		if pluginInfo, exists := pm.registryConfig.Plugins[name]; exists {
			if pluginInfo.ConfigPath != "" {
				result["with_external_config"]++
			} else {
				result["with_internal_config"]++
			}
		}
	}

	for _, state := range pm.pluginStates {
		switch state {
		case PluginRunning:
			result["running"]++
		case PluginError:
			result["error"]++
		case PluginStopped:
			result["stopped"]++
		case PluginInitialized:
			result["initialized"]++
		}
	}

	return result
}

// GetPluginDetails 获取插件详细信息
func (pm *PluginManager) GetPluginDetails() map[string]map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]map[string]interface{})

	for name, plugin := range pm.plugins {
		details := map[string]interface{}{
			"name":        plugin.GetName(),
			"version":     plugin.GetVersion(),
			"description": plugin.GetDescription(),
			"is_daemon":   plugin.IsDaemonPlugin(),
			"state":       pm.pluginStates[name],
		}

		// 添加配置信息
		if pluginInfo, exists := pm.registryConfig.Plugins[name]; exists {
			details["enabled"] = pluginInfo.Enabled
			details["priority"] = pluginInfo.Priority
			details["config_path"] = pluginInfo.ConfigPath
			details["init_mode"] = pluginInfo.InitMode
			details["has_external_config"] = pluginInfo.ConfigPath != ""
		}

		// 添加Daemon状态信息
		if plugin.IsDaemonPlugin() {
			processor := plugin.GetDaemonProcessor()
			if processor != nil {
				details["daemon_running"] = processor.IsRunning()
				details["daemon_status"] = processor.GetStatus()
			}
		}

		// 添加依赖信息
		dependencies := plugin.GetDependencies()
		if len(dependencies) > 0 {
			details["dependencies"] = dependencies
		}

		result[name] = details
	}

	return result
}

// ValidatePluginDependencies 验证插件依赖关系
func (pm *PluginManager) ValidatePluginDependencies() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for name, plugin := range pm.plugins {
		dependencies := plugin.GetDependencies()
		for _, dep := range dependencies {
			if _, exists := pm.plugins[dep]; !exists {
				return fmt.Errorf("plugin %s depends on %s which is not registered", name, dep)
			}
		}
	}

	return nil
}

// GetEnabledPlugins 获取已启用的插件列表
func (pm *PluginManager) GetEnabledPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var enabled []string
	for name, info := range pm.registryConfig.Plugins {
		if info.Enabled {
			enabled = append(enabled, name)
		}
	}

	return enabled
}

// GetRunningPlugins 获取正在运行的插件列表
func (pm *PluginManager) GetRunningPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var running []string
	for name, state := range pm.pluginStates {
		if state == PluginRunning {
			running = append(running, name)
		}
	}

	return running
}

// RestartPlugin 重启指定插件
func (pm *PluginManager) RestartPlugin(pluginName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	plugin, exists := pm.plugins[pluginName]
	if !exists {
		logger.Errorf("Plugin %s does not exist", pluginName)
		return fmt.Errorf("plugin %s not found", pluginName)
	}

	pluginInfo, configExists := pm.registryConfig.Plugins[pluginName]
	if !configExists {
		logger.Errorf("Plugin %s does not exist", pluginName)
		return fmt.Errorf("no config found for plugin %s", pluginName)
	}

	if !pluginInfo.Enabled {
		logger.Errorf("Plugin %s is disabled", pluginName)
		return fmt.Errorf("plugin %s is disabled", pluginName)
	}

	logger.Infof("Restarting plugin %s", pluginName)

	// 关闭插件
	if err := plugin.Shutdown(pm.ctx); err != nil {
		logger.Errorf("Failed to shutdown plugin %s during restart: %v", pluginName, err)
		pm.setPluginState(pluginName, PluginError)
		return fmt.Errorf("failed to shutdown plugin: %w", err)
	}

	// 重新初始化
	// configDirs := pm.getPluginConfigDirs(pluginName, pluginInfo)
	if err := plugin.Initialize(pm.ctx, pluginInfo); err != nil {
		logger.Errorf("Failed to reinitialize plugin %s: %v", pluginName, err)
		pm.setPluginState(pluginName, PluginError)
		return fmt.Errorf("failed to reinitialize plugin: %w", err)
	}

	// 如果是Daemon插件，启动处理器
	if plugin.IsDaemonPlugin() {
		processor := plugin.GetDaemonProcessor()
		if processor != nil {
			if err := processor.Start(pm.ctx); err != nil {
				logger.Errorf("Failed to start daemon processor for plugin %s: %v", pluginName, err)
				pm.setPluginState(pluginName, PluginError)
				return fmt.Errorf("failed to start daemon processor: %w", err)
			}
			go pm.monitorDaemonPlugin(pluginName, plugin, processor)
		}
	}

	pm.setPluginState(pluginName, PluginRunning)
	logger.Infof("Plugin %s restarted successfully", pluginName)

	return nil
}
