package plugins

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"plugin"
	"reflect"
	"strings"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
)

// PluginInitializer 默认插件初始化器实现
type PluginInitializer struct {
	registry                  IPluginTypeRegistry
	globalConstructorRegistry IConstructorRegistry
}

// NewPluginInitializer 创建插件初始化器
func NewPluginInitializer(registry IPluginTypeRegistry) IPluginInitializer {
	return &PluginInitializer{
		registry:                  registry,
		globalConstructorRegistry: GlobalConstructorRegistry,
	}
}

func (i *PluginInitializer) Initialize(ctx context.Context, initMode common.PluginInitMode, pluginInfo *config.PluginInfo) (Plugin, error) {
	switch initMode {
	case common.InitModeNew:
		return i.initializeByNew(ctx, pluginInfo)
	case common.InitModeFactory:
		return i.initializeByFactory(ctx, pluginInfo)
	case common.InitModeReflect:
		return i.initializeByReflect(ctx, pluginInfo)
	case common.InitModePlugin:
		return i.initializeByPlugin(ctx, pluginInfo)
	case common.InitModeSingleton:
		return i.initializeBySingleton(ctx, pluginInfo)
	case common.InitModePool:
		return i.initializeByPool(ctx, pluginInfo)
	default:
		return nil, fmt.Errorf("unsupported init mode: %s", initMode)
	}
}

func (i *PluginInitializer) SupportedModes() []common.PluginInitMode {
	return []common.PluginInitMode{
		common.InitModeNew,
		common.InitModeFactory,
		common.InitModeReflect,
		common.InitModePlugin,
		common.InitModeSingleton,
		common.InitModePool,
	}
}

func (i *PluginInitializer) SetRegistry(registry IPluginTypeRegistry) {
	i.registry = registry
}

// initializeByNew 通过反射new创建插件
func (i *PluginInitializer) initializeByNew(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	pluginStruct, exists := i.registry.GetType(pluginInfo.PluginType)
	if !exists {
		logger.Errorf("plugin type(%s) not exists", pluginInfo.PluginType)
		return nil, fmt.Errorf("plugin type %s not registered for new mode", pluginInfo.PluginType)
	}

	t := reflect.TypeOf(pluginStruct)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 创建实例
	instance := reflect.New(t).Interface()

	// 类型断言
	newPlugin, ok := instance.(Plugin)
	if !ok {
		logger.Errorf("plugin type %s does not implement Plugin interface", pluginInfo.PluginType)
		return nil, fmt.Errorf("plugin type %s does not implement Plugin interface", pluginInfo.PluginType)
	}

	if err := newPlugin.Initialize(ctx, pluginInfo); err != nil {
		logger.Errorf("plugin type %s initialize failed: %+v", pluginInfo.PluginType, err)
		return nil, err
	}

	logger.Debugf("Created plugin %s using new mode", pluginInfo.PluginType)
	return newPlugin, nil
}

// initializeByFactory 通过工厂函数创建插件
func (i *PluginInitializer) initializeByFactory(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	factory, exists := i.registry.GetFactory(pluginInfo.PluginType)
	if !exists {
		return nil, fmt.Errorf("plugin factory for type %s not registered", pluginInfo.PluginType)
	}

	factoryPlugin, err := factory.CreatePlugin(pluginInfo.InitParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin using factory: %w", err)
	}

	logger.Debugf("Created plugin %s using factory mode", pluginInfo.PluginType)
	return factoryPlugin, nil
}

// initializeByReflect 通过反射动态调用构造函数
func (i *PluginInitializer) initializeByReflect(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	// 步骤1：优先尝试已注册的构造函数
	if constructor, exists := i.registry.GetConstructor(pluginInfo.PluginType); exists {
		reflectPlugin, err := constructor(pluginInfo.InitParams)
		if err != nil {
			return nil, fmt.Errorf("failed to create plugin using registered constructor: %w", err)
		}
		logger.Debugf("Created plugin %s using registered constructor", pluginInfo.PluginType)
		return reflectPlugin, nil
	}

	// 步骤2：尝试全局构造函数查找
	if pluginInfo.ConstructorName != "" {
		if reflectPlugin, err := i.findConstructorInLoadedPackages(pluginInfo); err == nil {
			logger.Debugf("Created plugin %s using global constructor", pluginInfo.PluginType)
			return reflectPlugin, nil
		}
	}

	// 步骤3：真正的反射模式：通过包路径和构造函数名动态调用
	if pluginInfo.PackagePath != "" && pluginInfo.ConstructorName != "" {
		return i.initializeByDynamicReflect(ctx, pluginInfo)
	}

	return nil, fmt.Errorf("no suitable method found for reflect mode initialization of plugin %s", pluginInfo.PluginType)
}

// findConstructorInLoadedPackages 在已加载的包中查找构造函数
func (i *PluginInitializer) findConstructorInLoadedPackages(pluginInfo *config.PluginInfo) (Plugin, error) {
	// 方法1：尝试全局构造函数映射
	if globalConstructor := i.findGlobalConstructor(pluginInfo.ConstructorName); globalConstructor != nil {
		constructorPlugin, err := globalConstructor(pluginInfo.InitParams)
		if err != nil {
			return nil, fmt.Errorf("failed to create plugin using global constructor: %w", err)
		}
		return constructorPlugin, nil
	}

	// 方法2：如果指定了包路径，尝试包级构造函数
	if pluginInfo.PackagePath != "" {
		if packageConstructor := i.findPackageConstructor(pluginInfo.PackagePath, pluginInfo.ConstructorName); packageConstructor != nil {
			constructorPlugin, err := packageConstructor(pluginInfo.InitParams)
			if err != nil {
				return nil, fmt.Errorf("failed to create plugin using package constructor: %w", err)
			}
			return constructorPlugin, nil
		}
	}

	// 方法3：尝试在所有已注册的构造函数中模糊查找
	if fuzzyConstructor := i.findFuzzyConstructor(pluginInfo.ConstructorName); fuzzyConstructor != nil {
		constructorPlugin, err := fuzzyConstructor(pluginInfo.InitParams)
		if err != nil {
			return nil, fmt.Errorf("failed to create plugin using fuzzy constructor: %w", err)
		}
		return constructorPlugin, nil
	}

	return nil, fmt.Errorf("constructor %s not found in loaded packages", pluginInfo.ConstructorName)
}

// findGlobalConstructor 查找全局构造函数
func (i *PluginInitializer) findGlobalConstructor(constructorName string) PluginConstructor {
	if constructor, exists := i.globalConstructorRegistry.GetGlobalConstructor(constructorName); exists {
		logger.Debugf("Found global constructor: %s", constructorName)
		return constructor
	}
	return nil
}

// findPackageConstructor 查找包级构造函数
func (i *PluginInitializer) findPackageConstructor(packagePath, constructorName string) PluginConstructor {
	if constructor, exists := i.globalConstructorRegistry.GetPackageConstructor(packagePath, constructorName); exists {
		logger.Debugf("Found package constructor: %s.%s", packagePath, constructorName)
		return constructor
	}
	return nil
}

// findFuzzyConstructor 模糊查找构造函数
func (i *PluginInitializer) findFuzzyConstructor(constructorName string) PluginConstructor {
	allConstructors := i.globalConstructorRegistry.GetAllConstructors()

	// 精确匹配
	if constructor, exists := allConstructors[constructorName]; exists {
		logger.Debugf("Found exact match constructor: %s", constructorName)
		return constructor
	}

	// 模糊匹配（后缀匹配）
	for fullName, constructor := range allConstructors {
		if strings.HasSuffix(fullName, "."+constructorName) {
			logger.Debugf("Found fuzzy match constructor: %s -> %s", constructorName, fullName)
			return constructor
		}
	}

	return nil
}

// initializeByDynamicReflect 动态反射初始化
func (i *PluginInitializer) initializeByDynamicReflect(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	// 方法1：尝试通过已加载的包查找函数
	if reflectPlugin, err := i.findConstructorInRuntimePackages(pluginInfo); err == nil {
		return reflectPlugin, nil
	}

	// 方法2：如果有对应的.so文件，尝试加载
	if pluginInfo.PluginPath != "" {
		return i.initializeByPlugin(ctx, pluginInfo)
	}

	// 方法3：尝试通过AST解析和编译（开发环境）
	return i.initializeByASTParsing(pluginInfo)
}

// findConstructorInRuntimePackages 在运行时包中查找构造函数
func (i *PluginInitializer) findConstructorInRuntimePackages(pluginInfo *config.PluginInfo) (Plugin, error) {
	// 这里实现通过包路径在运行时查找构造函数的逻辑
	// 由于Go的运行时限制，我们采用预注册+智能查找的方式

	packagePath := pluginInfo.PackagePath
	constructorName := pluginInfo.ConstructorName

	// 尝试智能拼接查找
	possibleKeys := []string{
		constructorName,
		fmt.Sprintf("%s.%s", packagePath, constructorName),
		fmt.Sprintf("%s/%s", packagePath, constructorName),
		strings.Replace(fmt.Sprintf("%s.%s", packagePath, constructorName), "/", ".", -1),
	}

	allConstructors := i.globalConstructorRegistry.GetAllConstructors()

	for _, key := range possibleKeys {
		if constructor, exists := allConstructors[key]; exists {
			constructorPlugin, err := constructor(pluginInfo.InitParams)
			if err != nil {
				return nil, fmt.Errorf("failed to create plugin using runtime constructor %s: %w", key, err)
			}
			logger.Debugf("Created plugin using runtime constructor: %s", key)
			return constructorPlugin, nil
		}
	}

	return nil, fmt.Errorf("constructor %s not found in runtime packages for path %s", constructorName, packagePath)
}

// initializeByASTParsing 通过AST解析查找构造函数（开发环境）
func (i *PluginInitializer) initializeByASTParsing(pluginInfo *config.PluginInfo) (Plugin, error) {
	if pluginInfo.PackagePath == "" {
		return nil, fmt.Errorf("package_path is required for AST parsing")
	}

	// 检查包路径是否存在
	if _, err := os.Stat(pluginInfo.PackagePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("package path %s does not exist", pluginInfo.PackagePath)
	}

	// 解析包
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pluginInfo.PackagePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package %s: %w", pluginInfo.PackagePath, err)
	}

	// 查找构造函数
	for pkgName, pkg := range pkgs {
		if constructor := i.findConstructorInAST(pkg, pluginInfo.ConstructorName); constructor != nil {
			logger.Debugf("Found constructor %s in AST of package %s", pluginInfo.ConstructorName, pkgName)

			// 生成动态加载建议
			soPath := i.generateDynamicPluginPath(pluginInfo.PackagePath, pkgName)
			return nil, fmt.Errorf("found constructor %s in AST, but cannot call directly. Compile to plugin: go build -buildmode=plugin -o %s %s",
				pluginInfo.ConstructorName, soPath, pluginInfo.PackagePath)
		}
	}

	return nil, fmt.Errorf("constructor %s not found in package %s", pluginInfo.ConstructorName, pluginInfo.PackagePath)
}

// findConstructorInAST 在AST中查找构造函数
func (i *PluginInitializer) findConstructorInAST(pkg *ast.Package, constructorName string) *ast.FuncDecl {
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			if funcDecl, ok := decl.(*ast.FuncDecl); ok {
				if funcDecl.Name.Name == constructorName {
					// 验证函数签名是否符合构造函数要求
					if i.isValidConstructorSignature(funcDecl) {
						return funcDecl
					}
				}
			}
		}
	}
	return nil
}

// isValidConstructorSignature 验证构造函数签名
func (i *PluginInitializer) isValidConstructorSignature(funcDecl *ast.FuncDecl) bool {
	if funcDecl.Type.Results == nil || len(funcDecl.Type.Results.List) != 2 {
		return false
	}

	// 检查返回值类型（简化检查）
	// 实际应该检查是否返回 (Plugin[IPluginConfig], error)
	return true
}

// generateDynamicPluginPath 生成动态插件路径
func (i *PluginInitializer) generateDynamicPluginPath(packagePath, pkgName string) string {
	return filepath.Join("./plugins", fmt.Sprintf("%s.so", pkgName))
}

// initializeByPlugin 通过Go plugin包动态加载创建插件
func (i *PluginInitializer) initializeByPlugin(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	if pluginInfo.PluginPath == "" {
		return nil, fmt.Errorf("plugin_path is required for plugin mode")
	}

	// 加载插件文件
	p, exists := i.registry.GetLoadedPlugin(pluginInfo.PluginPath)
	if !exists {
		if err := i.registry.LoadPluginFromFile(pluginInfo.PluginPath); err != nil {
			return nil, fmt.Errorf("failed to load plugin: %w", err)
		}

		var ok bool
		p, ok = i.registry.GetLoadedPlugin(pluginInfo.PluginPath)
		if !ok {
			return nil, fmt.Errorf("plugin not found after loading")
		}
	}

	return i.createFromLoadedPlugin(p, pluginInfo)
}

// createFromLoadedPlugin 从已加载的插件创建实例
func (i *PluginInitializer) createFromLoadedPlugin(p *plugin.Plugin, pluginInfo *config.PluginInfo) (Plugin, error) {
	// 查找构造函数
	constructorName := pluginInfo.ConstructorName
	if constructorName == "" {
		constructorName = "NewPlugin" // 默认构造函数名
	}

	sym, err := p.Lookup(constructorName)
	if err != nil {
		return nil, fmt.Errorf("%s function not found in plugin: %w", constructorName, err)
	}

	// 类型断言为函数
	newPluginFunc, ok := sym.(func(map[string]interface{}) (Plugin, error))
	if !ok {
		return nil, fmt.Errorf("%s function has incorrect signature", constructorName)
	}

	// 调用函数创建插件
	newPlugin, err := newPluginFunc(pluginInfo.InitParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin from loaded file: %w", err)
	}

	logger.Debugf("Created plugin %s using plugin mode from file", pluginInfo.PluginType)
	return newPlugin, nil
}

// initializeBySingleton 单例模式创建插件
func (i *PluginInitializer) initializeBySingleton(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	// 检查单例是否已存在
	if singleton, exists := i.registry.GetSingleton(pluginInfo.PluginType); exists {
		logger.Debugf("Returned existing singleton plugin %s", pluginInfo.PluginType)
		return singleton, nil
	}

	// 创建新的单例实例
	var singletonPlugin Plugin
	var err error

	// 优先尝试工厂模式
	if factory, exists := i.registry.GetFactory(pluginInfo.PluginType); exists {
		singletonPlugin, err = factory.CreatePlugin(pluginInfo.InitParams)
	} else if pluginStruct, exists := i.registry.GetType(pluginInfo.PluginType); exists {
		singletonPlugin, err = i.createFromType(pluginStruct)
	} else {
		return nil, fmt.Errorf("no factory or type registered for plugin %s", pluginInfo.PluginType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create singleton plugin: %w", err)
	}

	// 保存单例
	i.registry.SetSingleton(pluginInfo.PluginType, singletonPlugin)
	logger.Debugf("Created singleton plugin %s", pluginInfo.PluginType)

	return singletonPlugin, nil
}

// createFromType 从类型创建插件实例
func (i *PluginInitializer) createFromType(pluginStruct interface{}) (Plugin, error) {
	t := reflect.TypeOf(pluginStruct)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	instance := reflect.New(t).Interface()
	typePlugin, ok := instance.(Plugin)
	if !ok {
		return nil, fmt.Errorf("type does not implement Plugin interface")
	}

	return typePlugin, nil
}

// initializeByPool 对象池模式创建插件
func (i *PluginInitializer) initializeByPool(ctx context.Context, pluginInfo *config.PluginInfo) (Plugin, error) {
	// 检查对象池是否已存在
	if pool, exists := i.registry.GetPool(pluginInfo.PluginType); exists {
		return pool.Get(pluginInfo.InitParams)
	}

	// 创建新的对象池
	factory, exists := i.registry.GetFactory(pluginInfo.PluginType)
	if !exists {
		return nil, fmt.Errorf("no factory registered for plugin pool %s", pluginInfo.PluginType)
	}

	maxSize := 10 // 默认池大小
	if size, ok := pluginInfo.InitParams["pool_size"].(int); ok {
		maxSize = size
	}

	pool := NewPluginPool(factory, maxSize)
	i.registry.SetPool(pluginInfo.PluginType, pool)

	poolPlugin, err := pool.Get(pluginInfo.InitParams)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin from pool: %w", err)
	}

	logger.Debugf("Created plugin %s using pool mode", pluginInfo.PluginType)
	return poolPlugin, nil
}

// GlobalPluginInitializer 全局初始化器实例
var GlobalPluginInitializer = NewPluginInitializer(GlobalPluginRegistry)
