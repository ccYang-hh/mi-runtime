package plugins

import (
	"fmt"
	"plugin"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// PluginTypeRegistry 默认插件类型注册表实现
type PluginTypeRegistry struct {
	factories     map[string]PluginFactory
	constructors  map[string]PluginConstructor
	types         map[string]interface{}
	singletons    map[string]Plugin
	pools         map[string]IPluginPool
	loadedPlugins map[string]*plugin.Plugin
	mu            sync.RWMutex
}

// NewPluginTypeRegistry 创建插件类型注册表
func NewPluginTypeRegistry() IPluginTypeRegistry {
	return &PluginTypeRegistry{
		factories:     make(map[string]PluginFactory),
		constructors:  make(map[string]PluginConstructor),
		types:         make(map[string]interface{}),
		singletons:    make(map[string]Plugin),
		pools:         make(map[string]IPluginPool),
		loadedPlugins: make(map[string]*plugin.Plugin),
	}
}

func (r *PluginTypeRegistry) RegisterFactory(pluginType string, factory PluginFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[pluginType]; exists {
		return fmt.Errorf("plugin factory for type %s already registered", pluginType)
	}

	r.factories[pluginType] = factory
	logger.Infof("Plugin factory registered for type: %s", pluginType)
	return nil
}

func (r *PluginTypeRegistry) GetFactory(pluginType string) (PluginFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.factories[pluginType]
	return factory, exists
}

func (r *PluginTypeRegistry) RegisterConstructor(pluginType string, constructor PluginConstructor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.constructors[pluginType]; exists {
		return fmt.Errorf("plugin constructor for type %s already registered", pluginType)
	}

	r.constructors[pluginType] = constructor
	logger.Infof("Plugin constructor registered for type: %s", pluginType)
	return nil
}

func (r *PluginTypeRegistry) GetConstructor(pluginType string) (PluginConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, exists := r.constructors[pluginType]
	return constructor, exists
}

func (r *PluginTypeRegistry) RegisterType(pluginType string, pluginStruct interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.types[pluginType] = pluginStruct
	logger.Infof("Plugin type registered: %s", pluginType)
	return nil
}

func (r *PluginTypeRegistry) GetType(pluginType string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pluginTypeVal, exists := r.types[pluginType]
	return pluginTypeVal, exists
}

func (r *PluginTypeRegistry) GetSingleton(pluginType string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	singleton, exists := r.singletons[pluginType]
	return singleton, exists
}

func (r *PluginTypeRegistry) SetSingleton(pluginType string, plugin Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.singletons[pluginType] = plugin
}

func (r *PluginTypeRegistry) GetPool(pluginType string) (IPluginPool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pool, exists := r.pools[pluginType]
	return pool, exists
}

func (r *PluginTypeRegistry) SetPool(pluginType string, pool IPluginPool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pools[pluginType] = pool
}

func (r *PluginTypeRegistry) LoadPluginFromFile(pluginPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.loadedPlugins[pluginPath]; exists {
		logger.Warnf("Plugin already loaded from %s", pluginPath)
		return nil
	}

	p, err := plugin.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to load plugin from %s: %w", pluginPath, err)
	}

	r.loadedPlugins[pluginPath] = p

	// 尝试自动注册插件类型
	if sym, err := p.Lookup("RegisterPluginTypes"); err == nil {
		if registerFunc, ok := sym.(func(IPluginTypeRegistry)); ok {
			registerFunc(r)
		}
	}

	// 尝试自动注册全局构造函数
	if sym, err := p.Lookup("RegisterGlobalConstructors"); err == nil {
		if registerFunc, ok := sym.(func(IConstructorRegistry)); ok {
			registerFunc(GlobalConstructorRegistry)
		}
	}

	logger.Infof("Successfully loaded plugin from %s", pluginPath)
	return nil
}

func (r *PluginTypeRegistry) GetLoadedPlugin(pluginPath string) (*plugin.Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.loadedPlugins[pluginPath]
	return p, exists
}

func (r *PluginTypeRegistry) GetRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeSet := make(map[string]struct{})

	for t := range r.factories {
		typeSet[t] = struct{}{}
	}
	for t := range r.constructors {
		typeSet[t] = struct{}{}
	}
	for t := range r.types {
		typeSet[t] = struct{}{}
	}

	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}

	return types
}

// PluginPool 默认插件对象池实现
type PluginPool struct {
	plugins []Plugin
	factory PluginFactory
	maxSize int
	mu      sync.Mutex
}

func NewPluginPool(factory PluginFactory, maxSize int) IPluginPool {
	return &PluginPool{
		plugins: make([]Plugin, 0, maxSize),
		factory: factory,
		maxSize: maxSize,
	}
}

func (p *PluginPool) Get(params map[string]interface{}) (Plugin, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.plugins) > 0 {
		pluginInPool := p.plugins[len(p.plugins)-1]
		p.plugins = p.plugins[:len(p.plugins)-1]
		return pluginInPool, nil
	}

	return p.factory.CreatePlugin(params)
}

func (p *PluginPool) Put(plugin Plugin) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.plugins) < p.maxSize {
		p.plugins = append(p.plugins, plugin)
	}
}

func (p *PluginPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.plugins)
}

func (p *PluginPool) MaxSize() int {
	return p.maxSize
}

// GlobalPluginRegistry 全局注册表实例
var GlobalPluginRegistry = NewPluginTypeRegistry()

// RegisterPluginFactory 注册PluginFactory
func RegisterPluginFactory(pluginType string, factory PluginFactory) error {
	return GlobalPluginRegistry.RegisterFactory(pluginType, factory)
}

// RegisterPluginConstructor 注册PluginConstructor
func RegisterPluginConstructor(pluginType string, constructor PluginConstructor) error {
	return GlobalPluginRegistry.RegisterConstructor(pluginType, constructor)
}

// RegisterPluginType 注册PluginType
func RegisterPluginType(pluginType string, pluginStruct interface{}) error {
	return GlobalPluginRegistry.RegisterType(pluginType, pluginStruct)
}
