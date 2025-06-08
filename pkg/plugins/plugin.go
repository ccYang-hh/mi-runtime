package plugins

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"xfusion.com/tmatrix/runtime/pkg/config"

	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

var _ Plugin = (*BasePlugin)(nil)

// CreatePluginConfig 构造Plugin配置
// IPluginConfig支持：1）静态配置   2）基于外部配置源
type CreatePluginConfig func(configDir ...string) (IPluginConfig, error)

// Plugin 插件接口
// 插件是系统扩展的主要机制，可以提供新的功能或修改现有功能
// 使用Go泛型来提供类型安全的配置管理
type Plugin interface {
	// GetName 获取插件名称
	GetName() string

	// GetVersion 获取插件版本
	GetVersion() string

	// GetDescription 获取插件描述
	GetDescription() string

	// Initialize 初始化插件
	Initialize(ctx context.Context, pluginInfo *config.PluginInfo) error

	// Shutdown 关闭插件
	Shutdown(ctx context.Context) error

	// GetConfig 获取插件配置
	GetConfig() IPluginConfig

	// UpdateConfig 更新插件配置
	UpdateConfig(configMap map[string]interface{}) error

	// GetPipelineStages 获取插件提供的管道阶段
	GetPipelineStages() []pipelines.Stage

	// GetStageHooks 获取插件提供的阶段钩子
	GetStageHooks() []pipelines.StageHook

	// GetPipelineHooks 获取插件提供的管道钩子
	GetPipelineHooks() []pipelines.PipelineHook

	// IsDaemonPlugin 是否是Daemon Plugin
	IsDaemonPlugin() bool

	// GetDaemonProcessor 获取插件的DaemonProcessor
	GetDaemonProcessor() PluginDaemonProcessor

	// GetAPIRouter 获取插件提供的API路由
	GetAPIRouter() *gin.RouterGroup

	// GetDependencies 获取插件依赖
	GetDependencies() []string

	// GetExtensionPoints 获取插件提供的扩展点
	GetExtensionPoints() map[string]interface{}
}

// BasePlugin 基础插件实现
type BasePlugin struct {
	name        string
	version     string
	description string

	config      IPluginConfig
	initialized bool
	mu          sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// Daemon相关
	isDaemon        bool
	daemonProcessor PluginDaemonProcessor
}

// PluginMetadata 插件元数据结构
type PluginMetadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	IsDaemon    bool   `json:"is_daemon"`
}

// NewBasePlugin 创建基础插件
func NewBasePlugin(metadata PluginMetadata) *BasePlugin {
	if metadata.Name == "" || metadata.Version == "" {
		panic("plugin name and version cannot be empty")
	}

	return &BasePlugin{
		name:        metadata.Name,
		version:     metadata.Version,
		description: metadata.Description,
		isDaemon:    metadata.IsDaemon,
	}
}

func (p *BasePlugin) GetName() string {
	return p.name
}

func (p *BasePlugin) GetVersion() string {
	return p.version
}

func (p *BasePlugin) GetDescription() string {
	return p.description
}

func (p *BasePlugin) IsDaemonPlugin() bool {
	return p.isDaemon
}

func (p *BasePlugin) GetDaemonProcessor() PluginDaemonProcessor {
	return p.daemonProcessor
}

// SetDaemonProcessor 设置Daemon处理器（由子类调用）
func (p *BasePlugin) SetDaemonProcessor(processor PluginDaemonProcessor) {
	p.daemonProcessor = processor
}

// Initialize ...
// p.createConfig需要由实现类覆写！！！
func (p *BasePlugin) Initialize(ctx context.Context, pluginInfo *config.PluginInfo) error {
	return p.DoInitialize(ctx, pluginInfo, p.createConfig)
}

// DoInitialize 初始化Plugin
func (p *BasePlugin) DoInitialize(ctx context.Context, pluginInfo *config.PluginInfo, createConfig CreatePluginConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		logger.Warnf("Plugin %s already initialized", p.name)
		return nil
	}

	// 创建插件上下文
	p.ctx, p.cancel = context.WithCancel(ctx)

	// 创建配置实例
	pluginConfig, err := createConfig(pluginInfo.ConfigPath)
	if err != nil {
		logger.Errorf("Failed to create config for plugin %s: %v", p.name, err)
		return fmt.Errorf("failed to create config: %w", err)
	}

	p.config = pluginConfig

	// 如果配置支持文件监听，启动监听
	if watcher, ok := any(pluginConfig).(interface{ StartFileWatcher() error }); ok {
		if err := watcher.StartFileWatcher(); err != nil {
			logger.Errorf("Failed to start file watcher for plugin %s: %+v", p.name, err)
			// 不返回错误，因为文件监听失败不应该阻止插件初始化
		}
	}

	p.initialized = true

	logger.Infof("Plugin %s v%s initialized successfully", p.name, p.version)
	return nil
}

func (p *BasePlugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		return nil
	}

	// 如果是Daemon插件，停止Daemon处理器
	if p.isDaemon && p.daemonProcessor != nil {
		if err := p.daemonProcessor.Stop(ctx); err != nil {
			logger.Errorf("Failed to stop daemon processor for plugin %s: %v", p.name, err)
		}
	}

	// 停止文件监听
	if watcher, ok := any(p.config).(interface{ StopFileWatcher() }); ok {
		watcher.StopFileWatcher()
	}

	// 如果配置支持关闭，执行关闭操作
	if shutdownable, ok := any(p.config).(interface{ Shutdown() }); ok {
		shutdownable.Shutdown()
	}

	// 取消插件上下文
	if p.cancel != nil {
		p.cancel()
	}

	p.initialized = false
	p.config = *new(IPluginConfig) // 重置为零值

	logger.Infof("Plugin %s v%s shutdown successfully", p.name, p.version)
	return nil
}

func (p *BasePlugin) GetConfig() IPluginConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.config
}

func (p *BasePlugin) UpdateConfig(configMap map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		logger.Errorf("Plugin %s not initialized, cannot update config", p.name)
		return fmt.Errorf("plugin not initialized")
	}

	// 如果配置支持从map更新，执行更新操作
	if updatable, ok := any(p.config).(interface {
		UpdateFromMap(map[string]interface{}) error
	}); ok {
		if err := updatable.UpdateFromMap(configMap); err != nil {
			logger.Errorf("Failed to update config for plugin %s: %v", p.name, err)
			return fmt.Errorf("failed to update config: %w", err)
		}
	}

	logger.Debugf("Config updated for plugin %s", p.name)
	return nil
}

// createConfig 创建配置实例 - 需要由具体插件实现重写
func (p *BasePlugin) createConfig(configDir ...string) (IPluginConfig, error) {
	var pluginConfig IPluginConfig
	configType := reflect.TypeOf(pluginConfig)

	// 处理指针类型
	if configType.Kind() == reflect.Ptr {
		configType = configType.Elem()
	}

	// 创建配置实例
	configValue := reflect.New(configType)
	configInterface := configValue.Interface().(IPluginConfig)

	// 根据配置类型进行初始化
	switch c := any(configInterface).(type) {
	case *PluginConfig:
		*c = *NewPluginConfig(p.name)
	default:
		// 对于FileSourcePluginConfig类型，需要子类重写createConfig方法
		logger.Warnf("Unknown config type for plugin %s, using default configuration", p.name)
	}

	return configInterface, nil
}

// GetPipelineStages 默认实现（子类可以重写）
func (p *BasePlugin) GetPipelineStages() []pipelines.Stage {
	if p.isDaemon {
		return []pipelines.Stage{} // Daemon插件不提供管道阶段
	}
	return []pipelines.Stage{}
}

func (p *BasePlugin) GetStageHooks() []pipelines.StageHook {
	if p.isDaemon {
		return []pipelines.StageHook{} // Daemon插件不提供阶段钩子
	}
	return []pipelines.StageHook{}
}

func (p *BasePlugin) GetPipelineHooks() []pipelines.PipelineHook {
	if p.isDaemon {
		return []pipelines.PipelineHook{} // Daemon插件不提供管道钩子
	}
	return []pipelines.PipelineHook{}
}

func (p *BasePlugin) GetAPIRouter() *gin.RouterGroup {
	return nil
}

func (p *BasePlugin) GetDependencies() []string {
	return []string{}
}

func (p *BasePlugin) GetExtensionPoints() map[string]interface{} {
	return make(map[string]interface{})
}

func (p *BasePlugin) String() string {
	return fmt.Sprintf("Plugin(%s v%s)", p.name, p.version)
}

// IsInitialized 检查插件是否已初始化
func (p *BasePlugin) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.initialized
}

// GetContext 获取插件上下文
func (p *BasePlugin) GetContext() context.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.ctx == nil {
		return context.Background()
	}

	return p.ctx
}

func (p *BasePlugin) RLock() {
	p.mu.RLock()
}

func (p *BasePlugin) RUnlock() {
	p.mu.RUnlock()
}

func (p *BasePlugin) Lock() {
	p.mu.Lock()
}

func (p *BasePlugin) Unlock() {
	p.mu.Unlock()
}
