package main

import (
	"context"
	"fmt"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// SimpleMathConfig 简单数学插件配置
type SimpleMathConfig struct {
	PluginName string `json:"plugin_name"`
	Enabled    bool   `json:"enabled"`
	Precision  int    `json:"precision"`
}

func (c *SimpleMathConfig) GetPluginName() string {
	return c.PluginName
}

func (c *SimpleMathConfig) Validate() error {
	if c.PluginName == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if c.Precision < 0 {
		return fmt.Errorf("precision cannot be negative")
	}
	return nil
}

func (c *SimpleMathConfig) IsEnabled() bool {
	return c.Enabled
}

func (c *SimpleMathConfig) Clone() plugins.IPluginConfig {
	return &SimpleMathConfig{
		PluginName: c.PluginName,
		Enabled:    c.Enabled,
		Precision:  c.Precision,
	}
}

func (c *SimpleMathConfig) UpdateFromMap(configMap map[string]interface{}) error {

	if precision, ok := configMap["precision"].(int); ok {
		c.Precision = precision
	}

	return nil
}

// SimpleMathPlugin 简单数学插件
type SimpleMathPlugin struct {
	*plugins.BasePlugin
	mu             sync.RWMutex
	operationCount int64
	lastResult     float64
}

// NewPlugin 外部插件的标准构造函数
func NewPlugin(params map[string]interface{}) (*SimpleMathPlugin, error) {
	name := "math-plugin"
	if n, ok := params["name"].(string); ok {
		name = n
	}

	pluginInfo := &config.PluginInfo{
		PluginName:    name,
		PluginVersion: "0.1.0",
	}

	return &SimpleMathPlugin{
		BasePlugin: plugins.NewBasePlugin(pluginInfo),
	}, nil
}

func (p *SimpleMathPlugin) Initialize(ctx context.Context, pluginInfo *config.PluginInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.operationCount = 0
	p.lastResult = 0.0

	_ = p.BasePlugin.DoInitialize(ctx, pluginInfo, p.createConfig)

	return nil
}

// createConfig 创建配置实例 - 需要由具体插件实现重写
func (p *SimpleMathPlugin) createConfig(configDir ...string) (plugins.IPluginConfig, error) {
	return &SimpleMathConfig{
		PluginName: p.GetName(),
		Enabled:    true,
		Precision:  100,
	}, nil
}

func (p *SimpleMathPlugin) GetExtensionPoints() map[string]interface{} {
	return map[string]interface{}{
		"Add":               p.Add,
		"GetOperationCount": p.GetOperationCount,
		"GetLastResult":     p.GetLastResult,
		"Reset":             p.Reset,
	}
}

// Add 简单的加法方法
func (p *SimpleMathPlugin) Add(a, b float64) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := a + b
	p.operationCount++
	p.lastResult = result

	return result
}

// GetOperationCount 获取操作次数（全局变量）
func (p *SimpleMathPlugin) GetOperationCount() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.operationCount
}

// GetLastResult 获取最后一次结果（全局变量）
func (p *SimpleMathPlugin) GetLastResult() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastResult
}

// Reset 重置全局变量
func (p *SimpleMathPlugin) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.operationCount = 0
	p.lastResult = 0.0
}

// RegisterPluginTypes 注册插件类型（可选的自动注册函数）
// 该函数在.so中暴露，用于PluginManager自动识别和注册
func RegisterPluginTypes(registry plugins.IPluginTypeRegistry) {
	// 这个函数会在插件加载时自动调用
	_ = registry.RegisterType("simple-math-plugin", (*SimpleMathPlugin)(nil))
}

// RegisterGlobalConstructors 注册全局构造函数（可选的自动注册函数）
// 该函数在.so中暴露，用于PluginManager自动识别和注册
func RegisterGlobalConstructors(registry plugins.IConstructorRegistry) {
	// 这个函数会在插件加载时自动调用
	_ = registry.RegisterGlobalConstructor("NewSimpleMathPlugin",
		func(params map[string]interface{}) (plugins.Plugin, error) {
			return NewPlugin(params)
		})
}

// main 函数（.so文件不需要main函数，但为了编译检查保留）
func main() {
	// 这个main函数在编译为.so文件时不会被使用
}
