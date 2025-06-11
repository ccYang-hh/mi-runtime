package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// MockSimpleConfig Mock简单配置
type MockSimpleConfig struct {
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Timeout  int      `json:"timeout"`
	Features []string `json:"features"`
}

func NewMockSimpleConfig() *MockSimpleConfig {
	return &MockSimpleConfig{
		Name:     "mocks",
		Enabled:  true,
		Timeout:  5,
		Features: []string{"mock_plugin"},
	}
}

func (c *MockSimpleConfig) GetPluginName() string {
	return c.Name
}

func (c *MockSimpleConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}

func (c *MockSimpleConfig) UpdateFromMap(configMap map[string]interface{}) error {

	if name, ok := configMap["name"].(string); ok {
		c.Name = name
	}
	if enabled, ok := configMap["enabled"].(bool); ok {
		c.Enabled = enabled
	}
	if timeout, ok := configMap["timeout"].(int); ok {
		c.Timeout = timeout
	}

	return nil
}

func (c *MockSimpleConfig) IsEnabled() bool {
	return c.Enabled
}

func (c *MockSimpleConfig) Clone() plugins.IPluginConfig {
	return &MockSimpleConfig{
		Name:     c.Name,
		Enabled:  c.Enabled,
		Timeout:  c.Timeout,
		Features: c.Features,
	}
}

// MockSimplePlugin Mock简单插件
type MockSimplePlugin struct {
	*plugins.BasePlugin
	callCount int
}

func NewMockSimplePlugin(pluginInfo *config.PluginInfo) *MockSimplePlugin {
	return &MockSimplePlugin{
		BasePlugin: plugins.NewBasePlugin(pluginInfo),
	}
}

// Initialize 覆写BasePlugin Initialize
// 覆写是因为防止InitModeNew模式下，报空指针，因为基于Type进行实例化后的Plugin为零值Plugin
func (p *MockSimplePlugin) Initialize(ctx context.Context, pluginInfo *config.PluginInfo) error {

	// NewByType得到的Plugin，其BasePlugin为零值，这里若为零值，可显式初始化
	if p.BasePlugin == nil {
		p.BasePlugin = plugins.NewBasePlugin(pluginInfo)
	}

	p.callCount = 0

	return p.BasePlugin.DoInitialize(ctx, pluginInfo, p.createConfig)
}

func (p *MockSimplePlugin) createConfig(configDir ...string) (plugins.IPluginConfig, error) {
	return &MockSimpleConfig{
		Name:     "mocks",
		Enabled:  true,
		Timeout:  5,
		Features: []string{"mock_plugin"},
	}, nil
}

func (p *MockSimplePlugin) IncrementCallCount() {
	p.RLock()
	defer p.RUnlock()
	p.callCount++
}

func (p *MockSimplePlugin) GetCallCount() int {
	p.RLock()
	defer p.RUnlock()
	return p.callCount
}

// MockComplexPlugin Mock复杂插件（支持Daemon模式）
type MockComplexPlugin struct {
	*MockSimplePlugin
	instanceID string
	customData map[string]interface{}
}

func NewMockComplexPlugin(
	instanceID string,
	pluginInfo *config.PluginInfo) *MockComplexPlugin {
	return &MockComplexPlugin{
		MockSimplePlugin: NewMockSimplePlugin(pluginInfo),
		instanceID:       instanceID,
		customData:       make(map[string]interface{}),
	}
}

// Initialize 覆写BasePlugin Initialize
// 覆写是因为防止InitModeNew模式下，报空指针，因为基于Type进行实例化后的Plugin为零值Plugin
func (p *MockComplexPlugin) Initialize(ctx context.Context, pluginInfo *config.PluginInfo) error {

	// NewByType得到的Plugin，其BasePlugin为零值，这里若为零值，可显式初始化
	if p.BasePlugin == nil {
		p.BasePlugin = plugins.NewBasePlugin(pluginInfo)
	}

	if pluginInfo.InitParams != nil {
		if instanceID, ok := pluginInfo.InitParams["instance_id"]; ok {
			p.instanceID = instanceID.(string)
		}

		if customData, ok := pluginInfo.InitParams["custom_data"]; ok {
			p.customData = customData.(map[string]interface{})
		}
	}

	return p.BasePlugin.DoInitialize(ctx, pluginInfo, p.createConfig)
}

func (p *MockComplexPlugin) createConfig(configDir ...string) (plugins.IPluginConfig, error) {
	return &MockSimpleConfig{
		Name:     "mocks",
		Enabled:  true,
		Timeout:  5,
		Features: []string{"mock_plugin"},
	}, nil
}

func (p *MockComplexPlugin) GetInstanceID() string {
	return p.instanceID
}

func (p *MockComplexPlugin) SetCustomData(key string, value interface{}) {
	p.RLock()
	defer p.RUnlock()
	p.customData[key] = value
}

func (p *MockComplexPlugin) GetCustomData(key string) (interface{}, bool) {
	p.RLock()
	defer p.RUnlock()
	value, exists := p.customData[key]
	return value, exists
}

// MockDaemonProcessor Mock Daemon处理器
type MockDaemonProcessor struct {
	*plugins.BaseDaemonProcessor
	processCount int
	errorMode    bool
	mu           sync.RWMutex
}

func NewMockDaemonProcessor(name string) *MockDaemonProcessor {
	return &MockDaemonProcessor{
		BaseDaemonProcessor: plugins.NewBaseDaemonProcessor(name),
	}
}

func (d *MockDaemonProcessor) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.errorMode {
		return fmt.Errorf("daemon start error (error mode)")
	}

	if err := d.BaseDaemonProcessor.Start(ctx); err != nil {
		logger.Errorf("Daemon Processor start error: %+v", err)
		return err
	}

	// 启动后台处理
	go d.backgroundProcess(ctx)

	return nil
}

func (d *MockDaemonProcessor) backgroundProcess(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.Lock()
			if !d.IsRunning() {
				d.mu.Unlock()
				return
			}
			d.processCount++
			d.mu.Unlock()
		}
	}
}

func (d *MockDaemonProcessor) SetErrorMode(errorMode bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.errorMode = errorMode
}

func (d *MockDaemonProcessor) GetProcessCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.processCount
}

// MockPluginFactory Mock插件工厂
type MockPluginFactory struct {
	pluginType  string
	createCount int
	errorMode   bool
	mu          sync.RWMutex
}

func NewMockPluginFactory(pluginType string) *MockPluginFactory {
	return &MockPluginFactory{
		pluginType: pluginType,
	}
}

func (f *MockPluginFactory) CreatePlugin(params map[string]interface{}) (plugins.Plugin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.createCount++

	if f.errorMode {
		return nil, fmt.Errorf("factory create error (error mode)")
	}

	// 从参数中获取配置
	name := fmt.Sprintf("%s-%d", f.pluginType, f.createCount)
	if n, ok := params["name"].(string); ok {
		name = n
	}

	version := "1.0.0"
	if v, ok := params["version"].(string); ok {
		version = v
	}

	instanceID := fmt.Sprintf("instance-%d-%d", time.Now().UnixNano(), f.createCount)
	if id, ok := params["instance_id"].(string); ok {
		instanceID = id
	}

	pluginInfo := &config.PluginInfo{
		PluginName:    name,
		PluginVersion: version,
	}

	// 根据参数决定创建的插件类型
	if isDaemon, ok := params["is_daemon"].(bool); ok && isDaemon {
		plugin := NewMockComplexPlugin(instanceID, pluginInfo)
		processor := NewMockDaemonProcessor(fmt.Sprintf("%s-daemon", name))
		plugin.SetDaemonProcessor(processor)
		return plugin, nil
	}

	pluginInfo.IsDaemon = false
	if instanceID != "" {
		plugin := NewMockComplexPlugin(instanceID, pluginInfo)
		return plugin, nil
	}

	return NewMockSimplePlugin(pluginInfo), nil
}

func (f *MockPluginFactory) GetPluginType() string {
	return f.pluginType
}

func (f *MockPluginFactory) GetSupportedModes() []common.PluginInitMode {
	return []common.PluginInitMode{
		common.InitModeFactory,
		common.InitModeSingleton,
		common.InitModePool,
	}
}

func (f *MockPluginFactory) SetErrorMode(errorMode bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorMode = errorMode
}

func (f *MockPluginFactory) GetCreateCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.createCount
}

// Mock构造函数

// NewMockSimplePluginConstructor Mock简单插件构造函数
func NewMockSimplePluginConstructor(params map[string]interface{}) (plugins.Plugin, error) {
	name := "mocks-simple"
	if n, ok := params["name"].(string); ok {
		name = n
	}

	version := "1.0.0"
	if v, ok := params["version"].(string); ok {
		version = v
	}

	pluginInfo := &config.PluginInfo{
		PluginName:    name,
		PluginVersion: version,
		IsDaemon:      false,
	}

	return NewMockSimplePlugin(pluginInfo), nil
}

// NewMockComplexPluginConstructor Mock复杂插件构造函数
func NewMockComplexPluginConstructor(params map[string]interface{}) (plugins.Plugin, error) {
	name := "mocks-complex"
	if n, ok := params["name"].(string); ok {
		name = n
	}

	version := "1.0.0"
	if v, ok := params["version"].(string); ok {
		version = v
	}

	pluginInfo := &config.PluginInfo{
		PluginName:    name,
		PluginVersion: version,
		IsDaemon:      false,
	}

	instanceID := fmt.Sprintf("instance-%d", time.Now().UnixNano())
	if id, ok := params["instance_id"].(string); ok {
		instanceID = id
	}

	return NewMockComplexPlugin(instanceID, pluginInfo), nil
}

// NewMockDaemonPluginConstructor Mock Daemon插件构造函数
func NewMockDaemonPluginConstructor(params map[string]interface{}) (plugins.Plugin, error) {
	name := "mocks-daemon"
	if n, ok := params["name"].(string); ok {
		name = n
	}

	version := "1.0.0"
	if v, ok := params["version"].(string); ok {
		version = v
	}

	pluginInfo := &config.PluginInfo{
		PluginName:    name,
		PluginVersion: version,
		IsDaemon:      true,
	}

	instanceID := fmt.Sprintf("daemon-instance-%d", time.Now().UnixNano())
	if id, ok := params["instance_id"].(string); ok {
		instanceID = id
	}

	plugin := NewMockComplexPlugin(instanceID, pluginInfo)
	processor := NewMockDaemonProcessor(fmt.Sprintf("%s-daemon", name))
	plugin.SetDaemonProcessor(processor)

	return plugin, nil
}

// RegisterMockPlugins 注册所有Mock插件
func RegisterMockPlugins() error {
	// 注册类型
	if err := plugins.RegisterPluginType("mocks-simple", (*MockSimplePlugin)(nil)); err != nil {
		return fmt.Errorf("failed to register mocks simple plugin type: %w", err)
	}

	if err := plugins.RegisterPluginType("mocks-complex", (*MockComplexPlugin)(nil)); err != nil {
		return fmt.Errorf("failed to register mocks complex plugin type: %w", err)
	}

	// 注册工厂
	if err := plugins.RegisterPluginFactory("mocks-factory", NewMockPluginFactory("mocks-factory")); err != nil {
		return fmt.Errorf("failed to register mocks factory: %w", err)
	}

	// 注册构造函数
	if err := plugins.RegisterPluginConstructor("mocks-constructor", NewMockSimplePluginConstructor); err != nil {
		return fmt.Errorf("failed to register mocks constructor: %w", err)
	}

	// 注册全局构造函数
	globalConstructors := map[string]plugins.PluginConstructor{
		"NewMockSimplePlugin":  NewMockSimplePluginConstructor,
		"NewMockComplexPlugin": NewMockComplexPluginConstructor,
		"NewMockDaemonPlugin":  NewMockDaemonPluginConstructor,
	}

	for name, constructor := range globalConstructors {
		if err := plugins.RegisterGlobalConstructor(name, constructor); err != nil {
			return fmt.Errorf("failed to register global constructor %s: %w", name, err)
		}
	}

	// 注册包级构造函数
	packageConstructors := map[string]map[string]plugins.PluginConstructor{
		"xfusion.com/tmatrix/runtime/pkg/plugins/mocks": {
			"NewMockSimplePlugin":  NewMockSimplePluginConstructor,
			"NewMockComplexPlugin": NewMockComplexPluginConstructor,
			"NewMockDaemonPlugin":  NewMockDaemonPluginConstructor,
		},
		"github.com/test/mocks": {
			"NewTestPlugin": NewMockSimplePluginConstructor,
		},
	}

	for packagePath, constructors := range packageConstructors {
		if err := plugins.RegisterPackageConstructors(packagePath, constructors); err != nil {
			return fmt.Errorf("failed to register package constructors for %s: %w", packagePath, err)
		}
	}

	return nil
}
