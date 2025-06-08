package plugins

import (
	"context"
	"plugin"
	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/config"
)

// PluginState 插件状态
type PluginState string

const (
	// PluginInitialized 插件已初始化
	PluginInitialized PluginState = "Initialized"

	// PluginRunning 插件运行中
	PluginRunning PluginState = "Running"

	// PluginStopped 插件已停止
	PluginStopped PluginState = "Stopped"

	// PluginError 插件出错
	PluginError PluginState = "Error"
)

// PluginConstructor 插件构造函数类型
type PluginConstructor func(params map[string]interface{}) (Plugin, error)

// PluginFactory 插件工厂接口
type PluginFactory interface {
	// CreatePlugin 创建插件实例
	CreatePlugin(params map[string]interface{}) (Plugin, error)

	// GetPluginType 获取插件类型
	GetPluginType() string

	// GetSupportedModes 获取支持的初始化模式
	GetSupportedModes() []common.PluginInitMode
}

// IPluginInitializer 插件初始化器接口
type IPluginInitializer interface {
	// Initialize 根据模式和配置初始化插件
	Initialize(ctx context.Context, initMode common.PluginInitMode, pluginInfo *config.PluginInfo) (Plugin, error)

	// SupportedModes 获取支持的初始化模式
	SupportedModes() []common.PluginInitMode

	// SetRegistry 设置类型注册表
	SetRegistry(registry IPluginTypeRegistry)
}

// IPluginPool 插件对象池接口
type IPluginPool interface {
	Get(params map[string]interface{}) (Plugin, error)
	Put(plugin Plugin)
	Size() int
	MaxSize() int
}

// IPluginTypeRegistry 插件类型注册表接口
type IPluginTypeRegistry interface {
	// RegisterFactory 注册插件工厂
	RegisterFactory(pluginType string, factory PluginFactory) error
	GetFactory(pluginType string) (PluginFactory, bool)

	// RegisterConstructor 注册构造函数
	RegisterConstructor(pluginType string, constructor PluginConstructor) error
	GetConstructor(pluginType string) (PluginConstructor, bool)

	// RegisterType 类型注册
	RegisterType(pluginType string, pluginStruct interface{}) error
	GetType(pluginType string) (interface{}, bool)

	// GetSingleton 单例管理
	GetSingleton(pluginType string) (Plugin, bool)
	SetSingleton(pluginType string, plugin Plugin)

	// GetPool 对象池管理
	GetPool(pluginType string) (IPluginPool, bool)
	SetPool(pluginType string, pool IPluginPool)

	// LoadPluginFromFile 动态插件管理
	LoadPluginFromFile(pluginPath string) error
	GetLoadedPlugin(pluginPath string) (*plugin.Plugin, bool)

	// GetRegisteredTypes 查询
	GetRegisteredTypes() []string
}

// IConstructorRegistry 构造函数注册表接口
type IConstructorRegistry interface {
	RegisterGlobalConstructor(name string, constructor PluginConstructor) error
	GetGlobalConstructor(name string) (PluginConstructor, bool)
	RegisterPackageConstructors(packagePath string, constructors map[string]PluginConstructor) error
	GetPackageConstructors(packagePath string) (map[string]PluginConstructor, bool)
	GetAllConstructors() map[string]PluginConstructor
	GetPackageConstructor(packagePath, constructorName string) (PluginConstructor, bool)
}
