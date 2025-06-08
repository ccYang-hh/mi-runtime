package plugins

import (
	"fmt"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// DefaultGlobalConstructorRegistry 默认全局构造函数注册表实现
type DefaultGlobalConstructorRegistry struct {
	globalConstructors  map[string]PluginConstructor
	packageConstructors map[string]map[string]PluginConstructor
	mu                  sync.RWMutex
}

// NewGlobalConstructorRegistry 创建全局构造函数注册表
func NewGlobalConstructorRegistry() IConstructorRegistry {
	return &DefaultGlobalConstructorRegistry{
		globalConstructors:  make(map[string]PluginConstructor),
		packageConstructors: make(map[string]map[string]PluginConstructor),
	}
}

func (r *DefaultGlobalConstructorRegistry) RegisterGlobalConstructor(name string, constructor PluginConstructor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.globalConstructors[name]; exists {
		return fmt.Errorf("global constructor %s already registered", name)
	}

	r.globalConstructors[name] = constructor
	logger.Debugf("Global constructor registered: %s", name)
	return nil
}

func (r *DefaultGlobalConstructorRegistry) GetGlobalConstructor(name string) (PluginConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, exists := r.globalConstructors[name]
	return constructor, exists
}

func (r *DefaultGlobalConstructorRegistry) RegisterPackageConstructors(packagePath string, constructors map[string]PluginConstructor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.packageConstructors[packagePath] == nil {
		r.packageConstructors[packagePath] = make(map[string]PluginConstructor)
	}

	for name, constructor := range constructors {
		r.packageConstructors[packagePath][name] = constructor
		logger.Debugf("Package constructor registered: %s.%s", packagePath, name)
	}

	return nil
}

func (r *DefaultGlobalConstructorRegistry) GetPackageConstructors(packagePath string) (map[string]PluginConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructors, exists := r.packageConstructors[packagePath]
	return constructors, exists
}

func (r *DefaultGlobalConstructorRegistry) GetAllConstructors() map[string]PluginConstructor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]PluginConstructor)

	// 添加全局构造函数
	for name, constructor := range r.globalConstructors {
		result[name] = constructor
	}

	// 添加包构造函数（使用完整路径作为key）
	for packagePath, constructors := range r.packageConstructors {
		for name, constructor := range constructors {
			fullName := fmt.Sprintf("%s.%s", packagePath, name)
			result[fullName] = constructor
		}
	}

	return result
}

func (r *DefaultGlobalConstructorRegistry) GetPackageConstructor(packagePath, constructorName string) (PluginConstructor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if packageConstructors, exists := r.packageConstructors[packagePath]; exists {
		if constructor, exists := packageConstructors[constructorName]; exists {
			return constructor, true
		}
	}

	return nil, false
}

// GlobalConstructorRegistry 全局实例
var GlobalConstructorRegistry = NewGlobalConstructorRegistry()

// RegisterGlobalConstructor 便捷注册函数
func RegisterGlobalConstructor(name string, constructor PluginConstructor) error {
	return GlobalConstructorRegistry.RegisterGlobalConstructor(name, constructor)
}

func RegisterPackageConstructors(packagePath string, constructors map[string]PluginConstructor) error {
	return GlobalConstructorRegistry.RegisterPackageConstructors(packagePath, constructors)
}
