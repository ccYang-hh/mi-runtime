package plugins

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config/source"
)

// IPluginConfig 插件配置接口
// 所有插件配置类的基础接口，无论是复杂配置还是简单配置
type IPluginConfig interface {
	// GetPluginName 获取插件名称
	GetPluginName() string
	// Validate 验证配置有效性
	Validate() error
	// Clone 深拷贝配置
	Clone() IPluginConfig
}

// BasePluginConfig 基础插件配置实现
type BasePluginConfig struct {
	pluginName string
}

// NewBasePluginConfig 创建基础插件配置
func NewBasePluginConfig(pluginName string) *BasePluginConfig {
	return &BasePluginConfig{
		pluginName: pluginName,
	}
}

func (c *BasePluginConfig) GetPluginName() string {
	return c.pluginName
}

func (c *BasePluginConfig) Validate() error {
	if c.pluginName == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	return nil
}

func (c *BasePluginConfig) Clone() IPluginConfig {
	return &BasePluginConfig{
		pluginName: c.pluginName,
	}
}

// PluginConfig 简单插件配置
// 适用于不需要外部配置文件的插件
type PluginConfig struct {
	*BasePluginConfig
}

// NewPluginConfig 创建简单插件配置
func NewPluginConfig(pluginName string) *PluginConfig {
	return &PluginConfig{
		BasePluginConfig: NewBasePluginConfig(pluginName),
	}
}

func (c *PluginConfig) Clone() IPluginConfig {
	return &PluginConfig{
		BasePluginConfig: c.BasePluginConfig.Clone().(*BasePluginConfig),
	}
}

// FileSourcePluginConfig 文件插件配置
// 需要接入外部配置源的插件配置使用此类，支持强类型配置
type FileSourcePluginConfig[T any] struct {
	*BasePluginConfig

	configDir    string
	configSource source.Source[T]
	configData   T
	defaultData  T

	// 并发控制
	mu        sync.RWMutex
	callbacks []func(T)

	// 监听控制
	ctx      context.Context
	cancel   context.CancelFunc
	watching bool
}

// NewFileSourcePluginConfig 创建泛型文件插件配置
func NewFileSourcePluginConfig[T any](
	runtimeCtx context.Context, pluginName string,
	defaultData T, configDir ...string,
) (*FileSourcePluginConfig[T], error) {
	dir := "./configs"
	if len(configDir) > 0 && configDir[0] != "" {
		dir = configDir[0]
	}

	// 基于runtimeCtx构造上下文，Runtime作为所有插件和组件的运行基础，
	// 当Runtime异常退出后，所有插件都应该退出
	ctx, cancel := context.WithCancel(runtimeCtx)

	config := &FileSourcePluginConfig[T]{
		BasePluginConfig: NewBasePluginConfig(pluginName),
		configDir:        dir,
		defaultData:      defaultData,
		configData:       defaultData, // 初始使用默认配置
		callbacks:        make([]func(T), 0),
		ctx:              ctx,
		cancel:           cancel,
	}

	// 查找并创建配置源
	configPath := config.findConfigFile()
	sourceID := fmt.Sprintf("plugin/%s", pluginName)
	config.configSource = source.NewFileSource[T](sourceID, configPath)

	// 加载初始配置
	if err := config.loadConfig(); err != nil {
		logger.Errorf("Failed to load initial config for plugin %s: %+v", pluginName, err)
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	return config, nil
}

// findConfigFile 查找配置文件路径
func (c *FileSourcePluginConfig[T]) findConfigFile() string {
	extensions := []string{"yaml", "yml", "json"}

	// 返回第一个匹配的文件路径，让 FileSource 自己处理文件存在性检查
	for _, ext := range extensions {
		path := filepath.Join(c.configDir, c.GetPluginName()+"."+ext)
		return path
	}

	return ""
}

// loadConfig 加载当前配置
func (c *FileSourcePluginConfig[T]) loadConfig() error {
	configData, err := c.configSource.Load(c.ctx)
	if err != nil {
		logger.Errorf("Failed to load config for plugin %s: %v", c.GetPluginName(), err)
		return fmt.Errorf("failed to load config: %w", err)
	}

	c.mu.Lock()
	if configData != nil {
		c.configData = *configData
	} else {
		// 如果加载失败，使用默认配置
		c.configData = c.defaultData
	}
	c.mu.Unlock()

	logger.Debugf("Loaded config for plugin %s", c.GetPluginName())
	return nil
}

// GetConfig 获取配置数据
func (c *FileSourcePluginConfig[T]) GetConfig() T {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.configData
}

// SetConfig 设置配置数据
func (c *FileSourcePluginConfig[T]) SetConfig(data T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.configData = data
}

// GetDefaultConfig 获取默认配置
func (c *FileSourcePluginConfig[T]) GetDefaultConfig() T {
	return c.defaultData
}

// ResetToDefault 重置为默认配置
func (c *FileSourcePluginConfig[T]) ResetToDefault() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.configData = c.defaultData
}

// StartFileWatcher 开始监控配置变化
func (c *FileSourcePluginConfig[T]) StartFileWatcher() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.watching {
		logger.Warnf("File watcher already started for plugin %s", c.GetPluginName())
		return nil
	}

	err := c.configSource.Watch(c.ctx, c.onConfigChange)
	if err != nil {
		logger.Errorf("Failed to start file watcher for plugin %s: %v", c.GetPluginName(), err)
		return fmt.Errorf("failed to start file watcher: %w", err)
	}

	c.watching = true
	logger.Debugf("File watcher started for plugin %s", c.GetPluginName())

	return nil
}

// StopFileWatcher 停止监控配置变化
func (c *FileSourcePluginConfig[T]) StopFileWatcher() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.watching {
		return
	}

	if err := c.configSource.Stop(); err != nil {
		logger.Errorf("Failed to stop file watcher for plugin %s: %v", c.GetPluginName(), err)
	}

	c.watching = false
	logger.Debugf("File watcher stopped for plugin %s", c.GetPluginName())
}

// onConfigChange 配置变更处理回调
func (c *FileSourcePluginConfig[T]) onConfigChange(newConfig *T) {
	if newConfig == nil {
		logger.Warnf("Received nil config for plugin %s", c.GetPluginName())
		return
	}

	c.mu.Lock()
	c.configData = *newConfig
	// 复制回调列表，避免回调期间修改列表引发问题
	callbacks := make([]func(T), len(c.callbacks))
	copy(callbacks, c.callbacks)
	c.mu.Unlock()

	logger.Debugf("Config changed for plugin %s", c.GetPluginName())

	// 并发执行所有回调
	var wg sync.WaitGroup
	for _, callback := range callbacks {
		wg.Add(1)
		go func(cb func(T)) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("Panic in config change callback for plugin %s: %v", c.GetPluginName(), r)
				}
			}()

			cb(*newConfig)
		}(callback)
	}

	wg.Wait()
}

// AddConfigChangeCallback 添加配置变化回调
func (c *FileSourcePluginConfig[T]) AddConfigChangeCallback(callback func(T)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.callbacks = append(c.callbacks, callback)
}

// RemoveAllCallbacks 移除所有回调（由于Go函数比较限制，提供清空所有回调的方法）
func (c *FileSourcePluginConfig[T]) RemoveAllCallbacks() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.callbacks = c.callbacks[:0]
}

// UpdateFromMap 从字典更新配置属性（兼容接口，用于运行时动态更新）
func (c *FileSourcePluginConfig[T]) UpdateFromMap(configMap map[string]interface{}) error {
	// TODO 这里可以使用 mapstructure 或 JSON 进行类型转换
	// 为了简化，暂时记录警告
	logger.Warnf("UpdateFromMap called on FileSourcePluginConfig for plugin %s, consider using SetConfig instead", c.GetPluginName())
	return nil
}

func (c *FileSourcePluginConfig[T]) Clone() IPluginConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 创建新的配置实例
	newConfig, err := NewFileSourcePluginConfig(c.ctx, c.GetPluginName(), c.defaultData, c.configDir)
	if err != nil {
		logger.Errorf("Failed to clone FileSourcePluginConfig: %v", err)
		return nil
	}

	// 复制当前配置数据
	newConfig.configData = c.configData

	return newConfig
}

// Shutdown 优雅关闭配置管理器
func (c *FileSourcePluginConfig[T]) Shutdown() {
	c.StopFileWatcher()
	c.cancel()
}

// IsWatching 检查是否正在监听文件变化
func (c *FileSourcePluginConfig[T]) IsWatching() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.watching
}

// GetConfigDir 获取配置目录
func (c *FileSourcePluginConfig[T]) GetConfigDir() string {
	return c.configDir
}
