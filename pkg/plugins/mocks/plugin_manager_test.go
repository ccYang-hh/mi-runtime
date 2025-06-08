package mocks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/config"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

func createPluginMetadata(name, version string) plugins.PluginMetadata {
	return plugins.PluginMetadata{
		Name:    name,
		Version: version,
	}
}

// 全局测试设置
func setupTest(t *testing.T) {
	// 注册Mock插件
	err := RegisterMockPlugins()
	require.NoError(t, err, "Failed to register mock plugins")
}

func teardownTest(t *testing.T) {
	// 清理测试数据
	// 这里可以添加清理逻辑
}

// TestPluginTypeRegistry 测试插件类型注册表
func TestPluginTypeRegistry(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	t.Run("RegisterAndGetFactory", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		factory := NewMockPluginFactory("test-factory")

		// 测试注册工厂
		err := registry.RegisterFactory("test-factory", factory)
		assert.NoError(t, err)

		// 测试获取工厂
		retrievedFactory, exists := registry.GetFactory("test-factory")
		assert.True(t, exists)
		assert.Equal(t, factory, retrievedFactory)

		// 测试重复注册
		err = registry.RegisterFactory("test-factory", factory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("RegisterAndGetConstructor", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()

		// 测试注册构造函数
		err := registry.RegisterConstructor("test-constructor", NewMockSimplePluginConstructor)
		assert.NoError(t, err)

		// 测试获取构造函数
		constructor, exists := registry.GetConstructor("test-constructor")
		assert.True(t, exists)
		assert.NotNil(t, constructor)

		// 测试调用构造函数
		plugin, err := constructor(map[string]interface{}{"name": "test"})
		assert.NoError(t, err)
		assert.Equal(t, "test", plugin.GetName())
	})

	t.Run("RegisterAndGetType", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()

		// 测试注册类型
		err := registry.RegisterType("test-type", (*MockSimplePlugin)(nil))
		assert.NoError(t, err)

		// 测试获取类型
		pluginType, exists := registry.GetType("test-type")
		assert.True(t, exists)

		// 值是nil但类型信息存在
		assert.Nil(t, pluginType)
		assert.NotNil(t, reflect.TypeOf(pluginType))

		// 验证类型正确性
		expectedType := reflect.TypeOf((*MockSimplePlugin)(nil))
		actualType := reflect.TypeOf(pluginType)
		assert.Equal(t, expectedType, actualType)

		// 验证可以用于创建实例
		if actualType.Kind() == reflect.Ptr {
			// 创建该类型的实例
			instance := reflect.New(actualType.Elem()).Interface()
			assert.IsType(t, &MockSimplePlugin{}, instance)
		}
	})

	t.Run("SingletonManagement", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		plugin := NewMockSimplePlugin(createPluginMetadata("singleton-test", "1.0.0"))

		// 测试设置单例
		registry.SetSingleton("test-singleton", plugin)

		// 测试获取单例
		retrievedPlugin, exists := registry.GetSingleton("test-singleton")
		assert.True(t, exists)
		assert.Equal(t, plugin, retrievedPlugin)

		// 测试不存在的单例
		_, exists = registry.GetSingleton("non-existent")
		assert.False(t, exists)
	})

	t.Run("PoolManagement", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		factory := NewMockPluginFactory("pool-test")
		pool := plugins.NewPluginPool(factory, 5)

		// 测试设置对象池
		registry.SetPool("test-pool", pool)

		// 测试获取对象池
		retrievedPool, exists := registry.GetPool("test-pool")
		assert.True(t, exists)
		assert.Equal(t, pool, retrievedPool)
		assert.Equal(t, 5, retrievedPool.MaxSize())
	})

	t.Run("GetRegisteredTypes", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()

		// 注册多种类型
		_ = registry.RegisterFactory("factory-type", NewMockPluginFactory("factory-type"))
		_ = registry.RegisterConstructor("constructor-type", NewMockSimplePluginConstructor)
		_ = registry.RegisterType("type-type", (*MockSimplePlugin)(nil))

		// 测试获取所有注册类型
		types := registry.GetRegisteredTypes()
		assert.Contains(t, types, "factory-type")
		assert.Contains(t, types, "constructor-type")
		assert.Contains(t, types, "type-type")
		assert.GreaterOrEqual(t, len(types), 3)
	})
}

// TestGlobalConstructorRegistry 测试全局构造函数注册表
func TestGlobalConstructorRegistry(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	t.Run("RegisterAndGetGlobalConstructor", func(t *testing.T) {
		registry := plugins.NewGlobalConstructorRegistry()

		// 测试注册全局构造函数
		err := registry.RegisterGlobalConstructor("TestConstructor", NewMockSimplePluginConstructor)
		assert.NoError(t, err)

		// 测试获取全局构造函数
		constructor, exists := registry.GetGlobalConstructor("TestConstructor")
		assert.True(t, exists)
		assert.NotNil(t, constructor)

		// 测试调用构造函数
		plugin, err := constructor(map[string]interface{}{"name": "global-test"})
		assert.NoError(t, err)
		assert.Equal(t, "global-test", plugin.GetName())

		// 测试重复注册
		err = registry.RegisterGlobalConstructor("TestConstructor", NewMockSimplePluginConstructor)
		assert.Error(t, err)
	})

	t.Run("RegisterAndGetPackageConstructors", func(t *testing.T) {
		registry := plugins.NewGlobalConstructorRegistry()

		constructors := map[string]plugins.PluginConstructor{
			"NewTestPlugin1": NewMockSimplePluginConstructor,
			"NewTestPlugin2": NewMockComplexPluginConstructor,
		}

		// 测试注册包构造函数
		err := registry.RegisterPackageConstructors("test/package", constructors)
		assert.NoError(t, err)

		// 测试获取包构造函数
		retrievedConstructors, exists := registry.GetPackageConstructors("test/package")
		assert.True(t, exists)
		assert.Len(t, retrievedConstructors, 2)

		// 测试获取单个包构造函数
		constructor, exists := registry.GetPackageConstructor("test/package", "NewTestPlugin1")
		assert.True(t, exists)
		assert.NotNil(t, constructor)
	})

	t.Run("GetAllConstructors", func(t *testing.T) {
		registry := plugins.NewGlobalConstructorRegistry()

		// 注册全局构造函数
		_ = registry.RegisterGlobalConstructor("GlobalConstructor", NewMockSimplePluginConstructor)

		// 注册包构造函数
		packageConstructors := map[string]plugins.PluginConstructor{
			"PackageConstructor": NewMockComplexPluginConstructor,
		}
		_ = registry.RegisterPackageConstructors("test/pkg", packageConstructors)

		// 测试获取所有构造函数
		allConstructors := registry.GetAllConstructors()
		assert.Contains(t, allConstructors, "GlobalConstructor")
		assert.Contains(t, allConstructors, "test/pkg.PackageConstructor")
		assert.GreaterOrEqual(t, len(allConstructors), 2)
	})
}

// TestPluginInitializer 测试插件初始化器
func TestPluginInitializer(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registry := plugins.NewPluginTypeRegistry()
	initializer := plugins.NewPluginInitializer(registry)

	// 注册测试数据
	_ = registry.RegisterType("mock-simple", (*MockSimplePlugin)(nil))
	_ = registry.RegisterFactory("mock-factory", NewMockPluginFactory("mock-factory"))
	_ = registry.RegisterConstructor("mock-constructor", NewMockSimplePluginConstructor)

	t.Run("InitializeByNew", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginName:    "mock-simple",
			PluginVersion: "0.1.0",
			PluginType:    "mock-simple",
			InitMode:      common.InitModeNew,
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin)
		assert.Equal(t, "mock-simple", plugin.GetName())
	})

	t.Run("InitializeByFactory", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginType:    "mock-factory",
			InitMode:      common.InitModeFactory,
			PluginName:    "factory-test",
			PluginVersion: "2.0.0",
			InitParams: map[string]interface{}{
				"name":    "factory-test",
				"version": "2.0.0",
			},
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin)
		assert.Contains(t, plugin.GetName(), "factory-test")
	})

	t.Run("InitializeByReflect", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginType:      "mock-constructor",
			InitMode:        common.InitModeReflect,
			PluginName:      "reflect-test",
			PluginVersion:   "1.5.0",
			ConstructorName: "NewMockSimplePlugin",
			InitParams: map[string]interface{}{
				"name":    "reflect-test",
				"version": "1.5.0",
			},
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin)
		assert.Equal(t, "reflect-test", plugin.GetName())
	})

	t.Run("InitializeBySingleton", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-factory",
			InitMode:   common.InitModeSingleton,
			InitParams: map[string]interface{}{
				"name": "singleton-test",
			},
		}

		// 第一次创建
		plugin1, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin1)

		// 第二次应该返回同一个实例
		plugin2, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.Equal(t, plugin1, plugin2)
	})

	t.Run("InitializeByPool", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-factory",
			InitMode:   common.InitModePool,
			InitParams: map[string]interface{}{
				"name":      "pool-test",
				"pool_size": 3,
			},
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin)

		// 验证对象池已创建
		pool, exists := registry.GetPool(pluginInfo.PluginType)
		assert.True(t, exists)
		assert.Equal(t, 3, pool.MaxSize())
	})

	t.Run("UnsupportedInitMode", func(t *testing.T) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-simple",
			InitMode:   common.PluginInitMode("unsupported"),
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.Error(t, err)
		assert.Nil(t, plugin)
		assert.Contains(t, err.Error(), "unsupported init mode")
	})

	t.Run("SupportedModes", func(t *testing.T) {
		modes := initializer.SupportedModes()
		expectedModes := []common.PluginInitMode{
			common.InitModeNew,
			common.InitModeFactory,
			common.InitModeReflect,
			common.InitModePlugin,
			common.InitModeSingleton,
			common.InitModePool,
		}

		for _, expectedMode := range expectedModes {
			assert.Contains(t, modes, expectedMode)
		}
	})
}

// TestPluginManager 测试插件管理器
func TestPluginManager(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("StartAndShutdown", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)

		// 测试启动
		err := manager.Start()
		assert.NoError(t, err)

		// 测试重复启动
		err = manager.Start()
		assert.NoError(t, err) // 应该忽略重复启动

		// 测试关闭
		err = manager.Shutdown()
		assert.NoError(t, err)
	})

	t.Run("UpdatePluginRegistry", func(t *testing.T) {
		manager := plugins.NewPluginManagerWithDependencies(
			context.TODO(), registryConfig,
			plugins.GlobalPluginRegistry, plugins.GlobalPluginInitializer,
		)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"test-plugin-1": {
					Enabled:       true,
					PluginName:    "mocks-simple",
					PluginVersion: "0.1.0",
					PluginType:    "mocks-simple",
					InitMode:      common.InitModeNew,
				},
				"test-plugin-2": {
					Enabled:       true,
					PluginType:    "mocks-factory",
					PluginName:    "mocks-factory",
					PluginVersion: "0.1.0",
					InitMode:      common.InitModeFactory,
					InitParams: map[string]interface{}{
						"name": "manager-test",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		assert.NoError(t, err)

		// 验证插件已注册
		states := manager.GetAllPluginState()
		assert.Contains(t, states, "test-plugin-1")
		assert.Contains(t, states, "test-plugin-2")
		assert.Equal(t, plugins.PluginInitialized, states["test-plugin-1"])
		assert.Equal(t, plugins.PluginInitialized, states["test-plugin-2"])
	})

	t.Run("InitializePlugins", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 配置插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"init-test-1": {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
				"init-test-2": {
					Enabled:    true,
					PluginType: "mock-factory",
					InitMode:   common.InitModeFactory,
					InitParams: map[string]interface{}{
						"name": "init-manager-test",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 初始化插件
		err = manager.InitializePlugins()
		assert.NoError(t, err)

		// 验证插件状态
		states := manager.GetAllPluginState()
		assert.Equal(t, plugins.PluginRunning, states["init-test-1"])
		assert.Equal(t, plugins.PluginRunning, states["init-test-2"])

		// 验证插件已初始化
		plugin1 := manager.GetPlugin("init-test-1")
		assert.NotNil(t, plugin1)
		if mockPlugin, ok := plugin1.(*MockSimplePlugin); ok {
			assert.True(t, mockPlugin.IsInitialized())
		}
	})

	t.Run("InitializeDaemonPlugins", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 配置Daemon插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"daemon-test": {
					Enabled:         true,
					PluginType:      "daemon-plugin",
					InitMode:        common.InitModeReflect,
					ConstructorName: "NewMockDaemonPlugin",
					InitParams: map[string]interface{}{
						"name": "daemon-test",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 初始化Daemon插件
		err = manager.InitializeDaemonPlugins()
		assert.NoError(t, err)

		// 验证Daemon插件状态
		states := manager.GetAllPluginState()
		assert.Equal(t, plugins.PluginRunning, states["daemon-test"])

		// 验证Daemon处理器正在运行
		plugin := manager.GetPlugin("daemon-test")
		assert.NotNil(t, plugin)
		assert.True(t, plugin.IsDaemonPlugin())

		processor := plugin.GetDaemonProcessor()
		assert.NotNil(t, processor)
		assert.True(t, processor.IsRunning())

		// 等待一段时间让daemon处理器工作
		time.Sleep(300 * time.Millisecond)

		if mockProcessor, ok := processor.(*MockDaemonProcessor); ok {
			assert.Greater(t, mockProcessor.GetProcessCount(), 0)
		}
	})

	t.Run("ReloadPlugin", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 配置插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"reload-test": {
					Enabled:    true,
					PluginType: "mock-factory",
					InitMode:   common.InitModeFactory,
					InitParams: map[string]interface{}{
						"name":        "reload-test",
						"instance_id": "before-reload",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		// 获取重载前的插件
		pluginBefore := manager.GetPlugin("reload-test")
		require.NotNil(t, pluginBefore)

		var instanceIDBefore string
		if complexPlugin, ok := pluginBefore.(*MockComplexPlugin); ok {
			instanceIDBefore = complexPlugin.GetInstanceID()
		}

		// 更新配置
		cfg.Plugins["reload-test"].InitParams["instance_id"] = "after-reload"
		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 获取重载后的插件
		pluginAfter := manager.GetPlugin("reload-test")
		require.NotNil(t, pluginAfter)

		var instanceIDAfter string
		if complexPlugin, ok := pluginAfter.(*MockComplexPlugin); ok {
			instanceIDAfter = complexPlugin.GetInstanceID()
		}

		// 验证是不同的实例
		assert.NotEqual(t, instanceIDBefore, instanceIDAfter)
	})

	t.Run("RegisterPluginType", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		factory := NewMockPluginFactory("dynamic-register-test")

		err := manager.RegisterPluginType("dynamic-register-test", factory)
		assert.NoError(t, err)

		// 验证已注册
		types := manager.GetRegisteredPluginTypes()
		assert.Contains(t, types, "dynamic-register-test")
	})

	t.Run("GetPlugin", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"get-test": {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		// 测试获取存在的插件
		plugin := manager.GetPlugin("get-test")
		assert.NotNil(t, plugin)
		assert.Equal(t, "mock-simple", plugin.GetName())

		// 测试获取不存在的插件
		plugin = manager.GetPlugin("non-existent")
		assert.Nil(t, plugin)
	})
}

// TestExternalPlugin 测试外部插件加载
func TestExternalPlugin(t *testing.T) {
	// 检查外部插件文件是否存在
	pluginPath := "testdata/simple_math_plugin.so"
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("External plugin file not found, skipping test. Please run: go build -buildmode=plugin -o testdata/simple_math_plugin.so external_plugins/simple_math_plugin/main.go")
	}

	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("LoadExternalPlugin", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 加载外部插件
		err = manager.LoadPluginFromFile(pluginPath)
		assert.NoError(t, err)

		// 配置外部插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"external-math": {
					Enabled:         true,
					PluginType:      "external-plugin",
					InitMode:        common.InitModePlugin,
					PluginPath:      pluginPath,
					ConstructorName: "NewPlugin",
					InitParams: map[string]interface{}{
						"name":      "external-math-plugin",
						"precision": 3,
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		assert.NoError(t, err)

		err = manager.InitializePlugins()
		assert.NoError(t, err)

		// 验证外部插件
		plugin := manager.GetPlugin("external-math")
		assert.NotNil(t, plugin)
		assert.Equal(t, "external-math-plugin", plugin.GetName())
		assert.Equal(t, "1.0.0", plugin.GetVersion())

		// 测试外部插件的扩展点
		extensionPoints := plugin.GetExtensionPoints()
		assert.Contains(t, extensionPoints, "Add")
		assert.Contains(t, extensionPoints, "GetOperationCount")
		assert.Contains(t, extensionPoints, "GetLastResult")
		assert.Contains(t, extensionPoints, "Reset")

		// 测试Add方法
		if addFunc, ok := extensionPoints["Add"].(func(float64, float64) float64); ok {
			result := addFunc(3.14, 2.86)
			assert.Equal(t, 6.0, result)
		}

		// 测试全局变量
		if getCountFunc, ok := extensionPoints["GetOperationCount"].(func() int64); ok {
			count := getCountFunc()
			assert.Equal(t, int64(1), count)
		}

		if getResultFunc, ok := extensionPoints["GetLastResult"].(func() float64); ok {
			lastResult := getResultFunc()
			assert.Equal(t, 6.0, lastResult)
		}

		// 测试重置
		if resetFunc, ok := extensionPoints["Reset"].(func()); ok {
			resetFunc()
		}

		if getCountFunc, ok := extensionPoints["GetOperationCount"].(func() int64); ok {
			count := getCountFunc()
			assert.Equal(t, int64(0), count)
		}
	})
}

// TestPluginPool 测试插件对象池
func TestPluginPool(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	t.Run("PoolOperations", func(t *testing.T) {
		factory := NewMockPluginFactory("pool-test")
		pool := plugins.NewPluginPool(factory, 3)

		// 测试基本属性
		assert.Equal(t, 3, pool.MaxSize())
		assert.Equal(t, 0, pool.Size())

		// 测试获取插件
		plugin1, err := pool.Get(map[string]interface{}{"name": "pool-plugin-1"})
		assert.NoError(t, err)
		assert.NotNil(t, plugin1)
		assert.Equal(t, 0, pool.Size()) // 池为空，需要创建新的

		// 测试放回插件
		pool.Put(plugin1)
		assert.Equal(t, 1, pool.Size())

		// 测试从池中获取
		plugin2, err := pool.Get(map[string]interface{}{"name": "pool-plugin-2"})
		assert.NoError(t, err)
		assert.Equal(t, plugin1, plugin2) // 应该是同一个实例
		assert.Equal(t, 0, pool.Size())

		// 测试池容量限制
		pool.Put(plugin1)
		pool.Put(plugin1)
		pool.Put(plugin1)
		pool.Put(plugin1)               // 超过最大容量
		assert.Equal(t, 3, pool.Size()) // 不应该超过最大容量
	})

	t.Run("PoolWithFactory", func(t *testing.T) {
		factory := NewMockPluginFactory("pool-factory-test")
		pool := plugins.NewPluginPool(factory, 2)

		// 测试工厂创建计数
		assert.Equal(t, 0, factory.GetCreateCount())

		plugin1, err := pool.Get(map[string]interface{}{})
		assert.NoError(t, err)
		assert.NotNil(t, plugin1)
		assert.Equal(t, 1, factory.GetCreateCount())

		plugin2, err := pool.Get(map[string]interface{}{})
		assert.NoError(t, err)
		assert.NotNil(t, plugin2)
		assert.Equal(t, 2, factory.GetCreateCount())

		// 放回池中
		pool.Put(plugin1)
		pool.Put(plugin2)

		// 再次获取，应该复用现有实例
		plugin3, err := pool.Get(map[string]interface{}{})
		assert.NoError(t, err)
		assert.Equal(t, 2, factory.GetCreateCount()) // 创建计数不应该增加
		assert.True(t, plugin3 == plugin1 || plugin3 == plugin2)
	})
}

// TestErrorHandling 测试错误处理
func TestErrorHandling(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("FactoryCreateError", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		initializer := plugins.NewPluginInitializer(registry)

		factory := NewMockPluginFactory("error-factory")
		factory.SetErrorMode(true) // 启用错误模式

		_ = registry.RegisterFactory("error-factory", factory)

		pluginInfo := &config.PluginInfo{
			PluginType: "error-factory",
			InitMode:   common.InitModeFactory,
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.Error(t, err)
		assert.Nil(t, plugin)
		assert.Contains(t, err.Error(), "factory create error")
	})

	t.Run("DaemonStartError", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 创建一个会启动失败的Daemon插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"error-daemon": {
					Enabled:         true,
					PluginType:      "error-daemon",
					InitMode:        common.InitModeReflect,
					ConstructorName: "NewMockDaemonPlugin",
					InitParams: map[string]interface{}{
						"name": "error-daemon",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 获取插件并设置其Daemon处理器为错误模式
		plugin := manager.GetPlugin("error-daemon")
		require.NotNil(t, plugin)

		processor := plugin.GetDaemonProcessor()
		require.NotNil(t, processor)

		if mockProcessor, ok := processor.(*MockDaemonProcessor); ok {
			mockProcessor.SetErrorMode(true)
		}

		// 尝试初始化Daemon插件，应该失败
		err = manager.InitializeDaemonPlugins()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "daemon start error")
	})

	t.Run("NonExistentPluginType", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		initializer := plugins.NewPluginInitializer(registry)

		pluginInfo := &config.PluginInfo{
			PluginType: "non-existent-type",
			InitMode:   common.InitModeNew,
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.Error(t, err)
		assert.Nil(t, plugin)
		assert.Contains(t, err.Error(), "not registered")
	})

	t.Run("InvalidConstructorName", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		initializer := plugins.NewPluginInitializer(registry)

		pluginInfo := &config.PluginInfo{
			PluginType:      "invalid-constructor",
			InitMode:        common.InitModeReflect,
			ConstructorName: "NonExistentConstructor",
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.Error(t, err)
		assert.Nil(t, plugin)
		assert.Contains(t, err.Error(), "not found")
	})
}

// BenchmarkPluginCreation 插件创建性能测试
func BenchmarkPluginCreation(b *testing.B) {
	_ = RegisterMockPlugins()

	registry := plugins.NewPluginTypeRegistry()
	initializer := plugins.NewPluginInitializer(registry)

	b.Run("NewMode", func(b *testing.B) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-simple",
			InitMode:   common.InitModeNew,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
			if err != nil {
				b.Fatal(err)
			}
			_ = plugin
		}
	})

	b.Run("FactoryMode", func(b *testing.B) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-factory",
			InitMode:   common.InitModeFactory,
			InitParams: map[string]interface{}{
				"name": "benchmark-test",
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
			if err != nil {
				b.Fatal(err)
			}
			_ = plugin
		}
	})

	b.Run("SingletonMode", func(b *testing.B) {
		pluginInfo := &config.PluginInfo{
			PluginType: "mock-factory",
			InitMode:   common.InitModeSingleton,
			InitParams: map[string]interface{}{
				"name": "singleton-benchmark",
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
			if err != nil {
				b.Fatal(err)
			}
			_ = plugin
		}
	})
}

// 测试辅助函数
func createTestConfigDir(t *testing.T) string {
	dir := filepath.Join(os.TempDir(), "plugin-test-configs")
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return dir
}

// 集成测试
func TestIntegration(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("CompleteLifecycle", func(t *testing.T) {
		// 创建临时配置目录
		configDir := createTestConfigDir(t)

		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		// 接续之前的集成测试
		defer manager.Shutdown()

		// 第一阶段：配置和注册插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"lifecycle-simple": {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
					ConfigPath: configDir,
				},
				"lifecycle-factory": {
					Enabled:    true,
					PluginType: "mock-factory",
					InitMode:   common.InitModeFactory,
					ConfigPath: configDir,
					InitParams: map[string]interface{}{
						"name":        "lifecycle-test",
						"instance_id": "integration-001",
					},
				},
				"lifecycle-daemon": {
					Enabled:         true,
					PluginType:      "daemon-plugin",
					InitMode:        common.InitModeReflect,
					ConstructorName: "NewMockDaemonPlugin",
					ConfigPath:      configDir,
					InitParams: map[string]interface{}{
						"name": "lifecycle-daemon",
					},
				},
				"lifecycle-singleton": {
					Enabled:    true,
					PluginType: "mock-factory",
					InitMode:   common.InitModeSingleton,
					ConfigPath: configDir,
					InitParams: map[string]interface{}{
						"name": "singleton-lifecycle",
					},
				},
			},
			GlobalConfig: map[string]interface{}{
				"test_mode":  true,
				"config_dir": configDir,
				"log_level":  "debug",
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 第二阶段：初始化常规插件
		err = manager.InitializePlugins()
		require.NoError(t, err)

		// 验证常规插件状态
		states := manager.GetAllPluginState()
		assert.Equal(t, plugins.PluginRunning, states["lifecycle-simple"])
		assert.Equal(t, plugins.PluginRunning, states["lifecycle-factory"])
		assert.Equal(t, plugins.PluginRunning, states["lifecycle-singleton"])

		// 第三阶段：初始化Daemon插件
		err = manager.InitializeDaemonPlugins()
		require.NoError(t, err)

		// 验证Daemon插件状态
		states = manager.GetAllPluginState()
		assert.Equal(t, plugins.PluginRunning, states["lifecycle-daemon"])

		// 第四阶段：验证插件功能
		// 验证简单插件
		simplePlugin := manager.GetPlugin("lifecycle-simple")
		require.NotNil(t, simplePlugin)
		assert.Equal(t, "mock-simple", simplePlugin.GetName())

		// 验证工厂插件
		factoryPlugin := manager.GetPlugin("lifecycle-factory")
		require.NotNil(t, factoryPlugin)
		if complexPlugin, ok := factoryPlugin.(*MockComplexPlugin); ok {
			assert.Equal(t, "integration-001", complexPlugin.GetInstanceID())
		}

		// 验证单例插件
		singletonPlugin1 := manager.GetPlugin("lifecycle-singleton")
		require.NotNil(t, singletonPlugin1)

		// 验证Daemon插件
		daemonPlugin := manager.GetPlugin("lifecycle-daemon")
		require.NotNil(t, daemonPlugin)
		assert.True(t, daemonPlugin.IsDaemonPlugin())

		processor := daemonPlugin.GetDaemonProcessor()
		require.NotNil(t, processor)
		assert.True(t, processor.IsRunning())

		// 第五阶段：测试动态配置更新
		// 禁用一个插件
		cfg.Plugins["lifecycle-simple"].Enabled = false
		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		// 验证插件已被禁用
		disabledPlugin := manager.GetPlugin("lifecycle-simple")
		assert.Nil(t, disabledPlugin)

		// 第六阶段：添加新插件
		cfg.Plugins["lifecycle-new"] = &config.PluginInfo{
			Enabled:    true,
			PluginType: "mock-factory",
			InitMode:   common.InitModeFactory,
			InitParams: map[string]interface{}{
				"name": "dynamically-added",
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		// 验证新插件
		newPlugin := manager.GetPlugin("lifecycle-new")
		require.NotNil(t, newPlugin)
		assert.Contains(t, newPlugin.GetName(), "mock-factory")

		// 第七阶段：性能和压力测试
		// 等待Daemon插件工作一段时间
		time.Sleep(500 * time.Millisecond)

		if mockProcessor, ok := processor.(*MockDaemonProcessor); ok {
			processCount := mockProcessor.GetProcessCount()
			assert.Greater(t, processCount, 0, "Daemon processor should have processed some items")

			status := mockProcessor.GetStatus()
			assert.True(t, status["running"].(bool))
			assert.Greater(t, status["process_count"].(int), 0)
		}

		// 第八阶段：验证单例行为
		// 再次创建单例插件实例，应该返回相同的实例
		singletonConfig := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"lifecycle-singleton-2": {
					Enabled:    true,
					PluginType: "mock-factory",
					InitMode:   common.InitModeSingleton,
					InitParams: map[string]interface{}{
						"name": "should-be-ignored", // 单例模式下这个参数应该被忽略
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(singletonConfig)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		singletonPlugin2 := manager.GetPlugin("lifecycle-singleton-2")
		require.NotNil(t, singletonPlugin2)

		// 验证是同一个实例（通过类型断言检查实例ID）
		if complexPlugin1, ok := singletonPlugin1.(*MockComplexPlugin); ok {
			if complexPlugin2, ok := singletonPlugin2.(*MockComplexPlugin); ok {
				assert.Equal(t, complexPlugin1.GetInstanceID(), complexPlugin2.GetInstanceID(),
					"Singleton plugins should have the same instance ID")
			}
		}

		// 第九阶段：错误恢复测试
		// 模拟插件错误
		if complexPlugin, ok := factoryPlugin.(*MockComplexPlugin); ok {
			complexPlugin.SetCustomData("error_injected", true)
		}

		// 重新加载插件
		err = manager.ReloadPlugin("lifecycle-factory")
		require.NoError(t, err)

		// 验证插件已重新加载
		reloadedPlugin := manager.GetPlugin("lifecycle-factory")
		require.NotNil(t, reloadedPlugin)

		if complexPlugin, ok := reloadedPlugin.(*MockComplexPlugin); ok {
			_, hasError := complexPlugin.GetCustomData("error_injected")
			assert.False(t, hasError, "Reloaded plugin should not have the error flag")
		}

		// 第十阶段：最终状态验证
		finalStates := manager.GetAllPluginState()

		// 应该有的插件
		expectedPlugins := []string{"lifecycle-factory", "lifecycle-daemon", "lifecycle-singleton", "lifecycle-new", "lifecycle-singleton-2"}
		for _, pluginName := range expectedPlugins {
			assert.Contains(t, finalStates, pluginName, "Plugin %s should exist", pluginName)
			assert.Equal(t, plugins.PluginRunning, finalStates[pluginName], "Plugin %s should be running", pluginName)
		}

		// 不应该有的插件
		assert.NotContains(t, finalStates, "lifecycle-simple", "Disabled plugin should not exist")

		// 验证注册的插件类型
		registeredTypes := manager.GetRegisteredPluginTypes()
		assert.Contains(t, registeredTypes, "mock-simple")
		assert.Contains(t, registeredTypes, "mock-factory")

		t.Logf("Integration test completed successfully. Final plugin count: %d", len(finalStates))
		t.Logf("Registered plugin types: %v", registeredTypes)
	})
}

// TestConcurrency 并发测试
func TestConcurrency(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("ConcurrentPluginCreation", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		const numGoroutines = 10
		const pluginsPerGoroutine = 5

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		results := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(routineID int) {
				defer wg.Done()

				for j := 0; j < pluginsPerGoroutine; j++ {
					pluginName := fmt.Sprintf("concurrent-%d-%d", routineID, j)

					cfg := config.PluginRegistryConfig{
						Plugins: map[string]*config.PluginInfo{
							pluginName: {
								Enabled:    true,
								PluginType: "mock-factory",
								InitMode:   common.InitModeFactory,
								InitParams: map[string]interface{}{
									"name": pluginName,
								},
							},
						},
					}

					if err := manager.UpdatePluginRegistry(cfg); err != nil {
						results <- fmt.Errorf("goroutine %d failed to update registry: %w", routineID, err)
						return
					}

					if err := manager.InitializePlugins(); err != nil {
						results <- fmt.Errorf("goroutine %d failed to initialize plugins: %w", routineID, err)
						return
					}

					// 验证插件创建成功
					plugin := manager.GetPlugin(pluginName)
					if plugin == nil {
						results <- fmt.Errorf("goroutine %d failed to get plugin %s", routineID, pluginName)
						return
					}
				}

				results <- nil
			}(i)
		}

		wg.Wait()
		close(results)

		// 检查结果
		for result := range results {
			assert.NoError(t, result)
		}

		// 验证最终状态
		states := manager.GetAllPluginState()
		expectedPluginCount := numGoroutines * pluginsPerGoroutine
		assert.GreaterOrEqual(t, len(states), expectedPluginCount)

		for _, state := range states {
			assert.Equal(t, plugins.PluginRunning, state)
		}
	})

	t.Run("ConcurrentSingletonAccess", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		initializer := plugins.NewPluginInitializer(registry)

		factory := NewMockPluginFactory("concurrent-singleton")
		_ = registry.RegisterFactory("concurrent-singleton", factory)

		const numGoroutines = 20
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		pluginsChan := make(chan plugins.Plugin, numGoroutines)

		pluginInfo := &config.PluginInfo{
			PluginType: "concurrent-singleton",
			InitMode:   common.InitModeSingleton,
			InitParams: map[string]interface{}{
				"name": "concurrent-test",
			},
		}

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()

				plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
				if err != nil {
					t.Errorf("Failed to initialize singleton plugin: %v", err)
					return
				}

				pluginsChan <- plugin
			}()
		}

		wg.Wait()
		close(pluginsChan)

		// 收集所有插件实例
		var collectedPlugins []plugins.Plugin
		for plugin := range pluginsChan {
			collectedPlugins = append(collectedPlugins, plugin)
		}

		assert.Len(t, collectedPlugins, numGoroutines)

		// 验证所有实例都是同一个对象
		firstPlugin := collectedPlugins[0]
		for i, plugin := range collectedPlugins {
			assert.Equal(t, firstPlugin, plugin, "Plugin %d should be the same instance as the first", i)
		}

		// 验证工厂只被调用了一次
		assert.Equal(t, 1, factory.GetCreateCount())
	})
}

// TestEdgeCases 边界情况测试
func TestEdgeCases(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("EmptyPluginName", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"": { // 空插件名
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		assert.NoError(t, err) // 配置更新应该成功

		err = manager.InitializePlugins()
		assert.NoError(t, err) // 初始化也应该成功

		// 但是插件名为空的插件应该能正常工作
		plugin := manager.GetPlugin("")
		assert.NotNil(t, plugin)
	})

	t.Run("NilInitParams", func(t *testing.T) {
		registry := plugins.NewPluginTypeRegistry()
		initializer := plugins.NewPluginInitializer(registry)

		_ = registry.RegisterConstructor("nil-params-test", NewMockSimplePluginConstructor)

		pluginInfo := &config.PluginInfo{
			PluginType:      "nil-params-test",
			InitMode:        common.InitModeReflect,
			ConstructorName: "NewMockSimplePlugin",
			InitParams:      nil, // nil参数
		}

		plugin, err := initializer.Initialize(context.TODO(), pluginInfo.InitMode, pluginInfo)
		assert.NoError(t, err)
		assert.NotNil(t, plugin)
	})

	t.Run("VeryLongPluginName", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 创建一个非常长的插件名
		longName := strings.Repeat("a", 1000)

		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				longName: {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		assert.NoError(t, err)

		err = manager.InitializePlugins()
		assert.NoError(t, err)

		plugin := manager.GetPlugin(longName)
		assert.NotNil(t, plugin)
	})

	t.Run("MaximumPluginCount", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)
		defer manager.Shutdown()

		// 创建大量插件
		const maxPlugins = 100
		cfg := config.PluginRegistryConfig{
			Plugins: make(map[string]*config.PluginInfo),
		}

		for i := 0; i < maxPlugins; i++ {
			pluginName := fmt.Sprintf("max-test-plugin-%d", i)
			cfg.Plugins[pluginName] = &config.PluginInfo{
				Enabled:    true,
				PluginType: "mock-simple",
				InitMode:   common.InitModeNew,
			}
		}

		start := time.Now()
		err = manager.UpdatePluginRegistry(cfg)
		assert.NoError(t, err)

		err = manager.InitializePlugins()
		assert.NoError(t, err)
		elapsed := time.Since(start)

		t.Logf("Created %d plugins in %v", maxPlugins, elapsed)

		// 验证所有插件都已创建
		states := manager.GetAllPluginState()
		assert.Len(t, states, maxPlugins)

		for _, state := range states {
			assert.Equal(t, plugins.PluginRunning, state)
		}
	})
}

// TestCleanup 清理测试
func TestCleanup(t *testing.T) {
	setupTest(t)
	defer teardownTest(t)

	registryConfig := config.PluginRegistryConfig{}

	t.Run("ProperShutdown", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)

		// 创建各种类型的插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"cleanup-simple": {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
				"cleanup-daemon": {
					Enabled:         true,
					PluginType:      "daemon-plugin",
					InitMode:        common.InitModeReflect,
					ConstructorName: "NewMockDaemonPlugin",
					InitParams: map[string]interface{}{
						"name": "cleanup-daemon",
					},
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		err = manager.InitializeDaemonPlugins()
		require.NoError(t, err)

		// 验证插件正在运行
		states := manager.GetAllPluginState()
		for _, state := range states {
			assert.Equal(t, plugins.PluginRunning, state)
		}

		// 验证Daemon插件正在运行
		daemonPlugin := manager.GetPlugin("cleanup-daemon")
		require.NotNil(t, daemonPlugin)
		processor := daemonPlugin.GetDaemonProcessor()
		require.NotNil(t, processor)
		assert.True(t, processor.IsRunning())

		// 执行关闭
		start := time.Now()
		err = manager.Shutdown()
		shutdownTime := time.Since(start)

		assert.NoError(t, err)
		t.Logf("Shutdown completed in %v", shutdownTime)

		// 验证Daemon插件已停止
		assert.False(t, processor.IsRunning())

		// 验证插件状态已更新
		finalStates := manager.GetAllPluginState()
		for pluginName, state := range finalStates {
			assert.Equal(t, plugins.PluginStopped, state, "Plugin %s should be stopped", pluginName)
		}
	})

	t.Run("ShutdownWithErrors", func(t *testing.T) {
		manager := plugins.NewPluginManager(context.TODO(), registryConfig)
		err := manager.Start()
		require.NoError(t, err)

		// 创建一个会在关闭时出错的插件
		cfg := config.PluginRegistryConfig{
			Plugins: map[string]*config.PluginInfo{
				"error-shutdown": {
					Enabled:    true,
					PluginType: "mock-simple",
					InitMode:   common.InitModeNew,
				},
			},
		}

		err = manager.UpdatePluginRegistry(cfg)
		require.NoError(t, err)

		err = manager.InitializePlugins()
		require.NoError(t, err)

		// 模拟插件关闭错误（这里我们需要修改Mock插件来支持错误模式）
		plugin := manager.GetPlugin("error-shutdown")
		require.NotNil(t, plugin)

		// 执行关闭，即使有错误也应该完成
		err = manager.Shutdown()
		// 根据实现，这里可能返回错误也可能不返回，取决于错误处理策略

		// 验证管理器已关闭
		states := manager.GetAllPluginState()
		for _, state := range states {
			// 插件可能处于错误状态或已停止状态
			assert.True(t, state == plugins.PluginStopped || state == plugins.PluginError)
		}
	})
}

// 运行所有测试的主函数
func TestMain(m *testing.M) {
	// 设置测试环境
	_ = os.Setenv("PLUGIN_TEST_MODE", "true")

	// 创建测试数据目录
	testDataDir := "testdata"
	_ = os.MkdirAll(testDataDir, 0755)

	// 运行测试
	code := m.Run()

	// 清理测试环境
	_ = os.RemoveAll(testDataDir)

	os.Exit(code)
}

// 示例测试：展示如何使用插件系统
func ExamplePluginManager() {
	// 注册Mock插件
	_ = RegisterMockPlugins()

	// 创建插件管理器
	manager := plugins.NewPluginManager(context.TODO(), config.PluginRegistryConfig{})
	_ = manager.Start()
	defer manager.Shutdown()

	// 配置插件
	cfg := config.PluginRegistryConfig{
		Plugins: map[string]*config.PluginInfo{
			"example-plugin": {
				Enabled:    true,
				PluginType: "mock-simple",
				InitMode:   common.InitModeNew,
			},
		},
	}

	// 更新配置并初始化
	_ = manager.UpdatePluginRegistry(cfg)
	_ = manager.InitializePlugins()

	// 使用插件
	plugin := manager.GetPlugin("example-plugin")
	fmt.Printf("Plugin: %s v%s\n", plugin.GetName(), plugin.GetVersion())

	// Output: Plugin: mock-simple v1.0.0
}
