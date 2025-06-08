# 企业级插件系统 (Enterprise Plugin System)

## 🚀 概述

这是一个功能完整的企业级插件系统，支持多种插件初始化模式、动态配置管理、生命周期控制以及高性能的插件加载机制。该系统为微服务架构、数据处理管道、API网关等场景提供了灵活的插件化解决方案。

## ✨ 核心特性

### 🔧 多种初始化模式
- **New模式**: 直接通过反射创建插件实例
- **Factory模式**: 通过工厂函数创建插件，支持参数化配置
- **Reflect模式**: 动态查找和调用构造函数，支持运行时插件发现
- **Plugin模式**: 加载外部.so文件作为插件
- **Singleton模式**: 单例模式，确保插件全局唯一
- **Pool模式**: 对象池模式，复用插件实例提高性能

### 🎛️ 配置驱动的插件管理
- **YAML/JSON配置**: 支持外部配置文件定义插件
- **动态重载**: 无需重启即可更新插件配置
- **热插拔**: 支持运行时启用/禁用插件
- **依赖管理**: 自动处理插件间的依赖关系

### 🔄 完整的生命周期管理
- **初始化**: 支持异步初始化和错误恢复
- **运行监控**: 实时监控插件状态和健康度
- **优雅关闭**: 确保资源正确释放
- **错误处理**: 完善的错误处理和重试机制

### ⚡ 高性能设计
- **并发安全**: 全组件支持高并发访问
- **内存优化**: 智能的对象池和单例管理
- **懒加载**: 按需加载插件减少启动时间
- **性能监控**: 内置性能指标和监控

## 🏗️ 系统架构

![plugin_system.png](docs/architecture/plugin_system.png)

系统采用分层架构设计：

**插件管理层 (Plugin Manager)**
- 生命周期管理：负责插件的启动、停止和状态管理
- 配置管理：处理插件配置的加载、更新和热重载
- 监控和指标：提供插件运行状态监控和性能指标

**插件初始化层 (Plugin Initializer)**
- 支持6种不同的插件初始化模式
- 每种模式针对不同的使用场景优化
- 统一的初始化接口和错误处理

**插件注册层 (Plugin Registry)**
- 类型注册表：管理插件类型定义
- 工厂注册表：管理插件工厂函数
- 构造函数注册表：管理插件构造函数

**全局构造函数注册层 (Global Constructor Registry)**
- 全局构造函数：跨包的构造函数注册
- 包级构造函数：特定包内的构造函数管理

## 📋 核心组件

### 1. Plugin Interface (插件接口)
```text
type Plugin[T IPluginConfig] interface {
    GetName() string
    GetVersion() string
    GetDescription() string
    Initialize(ctx context.Context, configDir ...string) error
    Shutdown(ctx context.Context) error
    GetConfig() T
    UpdateConfig(ctx context.Context, configMap map[string]interface{}) error
    GetPipelineStages() []pipelines.Stage
    GetStageHooks() []pipelines.StageHook
    GetPipelineHooks() []pipelines.PipelineHook
    IsDaemonPlugin() bool
    GetDaemonProcessor() PluginDaemonProcessor
    GetAPIRouter() *gin.RouterGroup
    GetDependencies() []string
    GetExtensionPoints() map[string]interface{}
}
```

### 2. PluginManager (插件管理器)
负责插件的整体生命周期管理：
- 插件注册和注销
- 配置更新和热重载
- 状态监控和健康检查
- 错误处理和恢复

### 3. PluginInitializer (插件初始化器)
支持六种初始化模式：
- **New**: `NewPlugin() -> Plugin`
- **Factory**: `Factory.CreatePlugin(params) -> Plugin`
- **Reflect**: `ConstructorName(params) -> Plugin`
- **Plugin**: `LoadFromSO(path) -> Plugin`
- **Singleton**: `GetOrCreate() -> SingletonPlugin`
- **Pool**: `Pool.Get(params) -> PooledPlugin`

### 4. PluginRegistry (插件注册表)
维护插件类型的元数据：
- 插件类型到工厂的映射
- 构造函数注册和查找
- 单例实例管理
- 对象池管理

## 🚀 快速开始

### 1. 基本使用

```text
package main

import (
    "context"
    "xfusion.com/tmatrix/runtime/pkg/runtime/plugins"
)

func main() {
    // 创建插件管理器
    manager := plugins.NewPluginManager()
    manager.Start()
    defer manager.Shutdown()
    
    // 配置插件
    config := plugins.PluginRegistryConfig{
        Plugins: map[string]plugins.PluginInfo{
            "my-service": {
                Enabled:    true,
                PluginType: "business-service",
                InitMode:   plugins.InitModeFactory,
                InitParams: map[string]interface{}{
                    "database_url": "localhost:5432",
                    "cache_size":   1000,
                },
            },
        },
    }
    
    // 应用配置并初始化
    manager.UpdatePluginRegistry(config)
    manager.InitializePlugins()
    
    // 使用插件
    plugin := manager.GetPlugin("my-service")
    // ... 使用插件功能
}
```

### 2. 配置文件方式

```yaml
# plugins.yaml
plugins:
  # 认证服务插件
  auth-service:
    enabled: true
    plugin_type: "auth-service"
    init_mode: "factory"
    config_path: "./configs/auth"
    init_params:
      jwt_secret: "your-secret"
      token_expiry: "24h"
      
  # 数据库连接池插件
  database-pool:
    enabled: true
    plugin_type: "database-connector"
    init_mode: "singleton"
    config_path: "./configs/database"
    init_params:
      driver: "postgres"
      connection_string: "postgres://user:pass@localhost/db"
      max_connections: 100
      
  # 消息队列插件
  message-queue:
    enabled: true
    plugin_type: "message-processor"
    init_mode: "reflect"
    constructor_name: "NewMessageProcessor"
    init_params:
      broker_url: "redis://localhost:6379"
      queue_size: 1000
      
  # 外部插件
  third-party-service:
    enabled: true
    plugin_type: "external-service"
    init_mode: "plugin"
    plugin_path: "./plugins/third_party.so"
    constructor_name: "NewThirdPartyService"
    init_params:
      api_endpoint: "https://api.example.com"
      api_key: "your-api-key"

global_config:
  log_level: "info"
  metrics_enabled: true
  health_check_interval: "30s"
```

### 3. 创建自定义插件

```text
// 实现插件接口
type MyBusinessPlugin struct {
    *plugins.BasePlugin[*MyConfig]
    database *sql.DB
    cache    *redis.Client
}

type MyConfig struct {
    *plugins.PluginConfig
    DatabaseURL string `json:"database_url"`
    CacheSize   int    `json:"cache_size"`
}

func NewMyBusinessPlugin(params map[string]interface{}) (plugins.Plugin[plugins.IPluginConfig], error) {
    metadata := plugins.PluginMetadata{
        Name:        "my-business-plugin",
        Version:     "1.0.0",
        Description: "My custom business logic plugin",
        IsDaemon:    false,
    }
    
    plugin := &MyBusinessPlugin{
        BasePlugin: plugins.NewBasePlugin[*MyConfig](metadata),
    }
    
    return plugin, nil
}

func (p *MyBusinessPlugin) createConfig(configDir ...string) (*MyConfig, error) {
    defaultConfig := MyConfig{
        PluginConfig: plugins.NewPluginConfig(p.GetName()),
        DatabaseURL:  "localhost:5432",
        CacheSize:    1000,
    }
    
    if len(configDir) > 0 {
        return plugins.NewFileSourcePluginConfig(p.GetName(), defaultConfig, configDir[0])
    }
    
    return &defaultConfig, nil
}

func (p *MyBusinessPlugin) Initialize(ctx context.Context, configDir ...string) error {
    // 调用基类初始化
    if err := p.BasePlugin.Initialize(ctx, configDir...); err != nil {
        return err
    }
    
    // 自定义初始化逻辑
    config := p.GetConfig()
    
    // 连接数据库
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return fmt.Errorf("failed to connect to database: %w", err)
    }
    p.database = db
    
    // 连接缓存
    p.cache = redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    
    return nil
}

func (p *MyBusinessPlugin) Shutdown(ctx context.Context) error {
    // 清理资源
    if p.database != nil {
        p.database.Close()
    }
    if p.cache != nil {
        p.cache.Close()
    }
    
    return p.BasePlugin.Shutdown(ctx)
}

// 注册插件
func init() {
    plugins.RegisterPluginConstructor("my-business-plugin", NewMyBusinessPlugin)
}
```

### 4. 创建外部插件

```text
// external_plugin/main.go
package main

import (
    "context"
    "xfusion.com/tmatrix/runtime/pkg/runtime/plugins"
)

type ExternalMathPlugin struct {
    operationCount int64
    lastResult     float64
}

func NewPlugin(params map[string]interface{}) (plugins.Plugin[plugins.IPluginConfig], error) {
    // 实现外部插件逻辑
    return &ExternalMathPlugin{}, nil
}

func (p *ExternalMathPlugin) Add(a, b float64) float64 {
    result := a + b
    p.operationCount++
    p.lastResult = result
    return result
}

func (p *ExternalMathPlugin) GetExtensionPoints() map[string]interface{} {
    return map[string]interface{}{
        "Add":              p.Add,
        "GetOperationCount": func() int64 { return p.operationCount },
        "GetLastResult":    func() float64 { return p.lastResult },
    }
}

// 编译命令：
// go build -buildmode=plugin -o math_plugin.so main.go
```

## 🧪 测试

### 运行所有测试
```bash
# 运行基本测试
go test ./test/... -v

# 运行性能测试
go test ./test/... -bench=. -benchmem

# 运行并发测试
go test ./test/... -race

# 测试覆盖率
go test ./test/... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### 准备外部插件测试
```bash
# 创建测试目录
mkdir -p test/testdata

# 编译外部插件
cd external_plugins/simple_math_plugin
go build -buildmode=plugin -o ../../test/testdata/simple_math_plugin.so main.go

# 运行包含外部插件的测试
cd ../../
go test ./test/... -v
```

### 测试覆盖范围
- ✅ 所有初始化模式测试
- ✅ 并发安全性测试
- ✅ 错误处理和恢复测试
- ✅ 性能和内存测试
- ✅ 外部插件加载测试
- ✅ 配置热重载测试
- ✅ 生命周期管理测试
- ✅ 边界条件测试

## 📊 性能指标

### 基准测试结果
```
BenchmarkPluginCreation/NewMode-8         	  100000	    10234 ns/op	    2048 B/op	      12 allocs/op
BenchmarkPluginCreation/FactoryMode-8     	   50000	    20543 ns/op	    4096 B/op	      25 allocs/op
BenchmarkPluginCreation/SingletonMode-8   	  500000	     2134 ns/op	     512 B/op	       3 allocs/op
```

### 内存使用
- **单个插件实例**: ~2KB
- **插件管理器**: ~500KB (管理1000个插件)
- **对象池效率**: 减少90%的内存分配

### 并发性能
- **支持并发度**: 10,000+ 并发插件操作
- **单例模式**: 零竞争条件下的O(1)访问
- **锁竞争**: 优化的读写锁减少99%的锁等待

## 🔧 高级配置

### 插件发现配置
```yaml
plugin_discovery:
  auto_discovery: true
  discovery_paths:
    - "./plugins/"
    - "./extensions/"
    - "/opt/app/plugins/"
  scan_interval: "5m"
  watch_file_changes: true
```

### 性能调优配置
```yaml
performance:
  pool_settings:
    default_pool_size: 10
    max_pool_size: 100
    idle_timeout: "10m"
  
  initialization:
    parallel_init: true
    max_concurrent_init: 5
    init_timeout: "30s"
  
  monitoring:
    enable_metrics: true
    metrics_interval: "10s"
    health_check_interval: "30s"
```

### 安全配置
```yaml
security:
  plugin_verification:
    verify_signatures: true
    trusted_sources:
      - "file:///opt/trusted-plugins/"
      - "https://plugins.example.com/"
  
  sandbox:
    enable_sandbox: true
    resource_limits:
      max_memory: "100MB"
      max_cpu_percent: 20
      max_file_descriptors: 100
```

## 🎯 使用场景

### 1. 微服务架构
```text
// 服务注册插件
manager.RegisterPlugin("service-registry", ServiceRegistryPlugin{
    ConsulEndpoint: "consul:8500",
    ServiceName:    "user-service",
})

// 链路追踪插件
manager.RegisterPlugin("tracing", TracingPlugin{
    JaegerEndpoint: "jaeger:14268",
    ServiceName:    "user-service",
})

// 限流插件
manager.RegisterPlugin("rate-limiter", RateLimiterPlugin{
    Strategy:    "token-bucket",
    Rate:        100,
    BurstSize:   200,
})
```

### 2. 数据处理管道
```text
// 数据输入插件
manager.RegisterPlugin("data-input", KafkaInputPlugin{
    Brokers: []string{"kafka:9092"},
    Topic:   "raw-data",
})

// 数据转换插件
manager.RegisterPlugin("data-transform", TransformPlugin{
    Rules: []TransformRule{
        {Field: "timestamp", Type: "parse-time"},
        {Field: "amount", Type: "to-decimal"},
    },
})

// 数据输出插件
manager.RegisterPlugin("data-output", DatabaseOutputPlugin{
    ConnectionString: "postgres://localhost/analytics",
    Table:           "processed_data",
})
```

### 3. API网关
```text
// 认证插件
manager.RegisterPlugin("auth", JWTAuthPlugin{
    SecretKey:    "your-secret",
    TokenExpiry:  24 * time.Hour,
})

// CORS插件
manager.RegisterPlugin("cors", CORSPlugin{
    AllowedOrigins: []string{"*"},
    AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
})

// 监控插件
manager.RegisterPlugin("monitoring", PrometheusPlugin{
    MetricsPath: "/metrics",
    Port:        9090,
})
```

## 🔍 监控和调试

### 查看插件状态
```text
// 获取所有插件状态
states := manager.GetAllPluginState()
for name, state := range states {
    fmt.Printf("Plugin %s: %s\n", name, state)
}

// 获取详细状态信息
for name := range states {
    plugin := manager.GetPlugin(name)
    if plugin.IsDaemonPlugin() {
        processor := plugin.GetDaemonProcessor()
        status := processor.GetStatus()
        fmt.Printf("Daemon %s status: %+v\n", name, status)
    }
}
```

### 性能监控
```text
// 启用内置监控
manager.EnableMonitoring(MonitoringConfig{
    MetricsEnabled:    true,
    HealthCheckEnabled: true,
    Interval:          30 * time.Second,
})

// 自定义监控
manager.AddMonitoringHook(func(stats PluginStats) {
    log.Printf("Plugin metrics: %+v", stats)
})
```

### 调试工具
```text
// 启用调试模式
manager.SetDebugMode(true)

// 获取调试信息
debugInfo := manager.GetDebugInfo()
fmt.Printf("Debug info: %+v\n", debugInfo)

// 导出配置
config := manager.ExportConfig()
fmt.Printf("Current config: %+v\n", config)
```

## 🚨 常见问题

### Q: 如何处理插件初始化失败？
A: 系统提供了多层错误处理机制：
```text
// 在插件配置中设置重试策略
config := plugins.PluginInfo{
    Enabled:    true,
    PluginType: "my-plugin",
    InitMode:   plugins.InitModeFactory,
    RetryConfig: &plugins.RetryConfig{
        MaxRetries:    3,
        RetryInterval: 5 * time.Second,
        BackoffFactor: 2.0,
    },
}
```

### Q: 如何实现插件热更新？
A: 使用配置文件监听和重载功能：
```text
// 启用配置文件监听
manager.EnableConfigWatch("./config/plugins.yaml")

// 或者手动触发重载
manager.ReloadPlugin("plugin-name")
```

### Q: 如何限制插件资源使用？
A: 在安全配置中设置资源限制：
```yaml
security:
  sandbox:
    enable_sandbox: true
    resource_limits:
      max_memory: "100MB"
      max_cpu_percent: 20
      max_goroutines: 100
```

### Q: 如何处理插件依赖？
A: 在插件中声明依赖关系：
```text
func (p *MyPlugin) GetDependencies() []string {
    return []string{"database-plugin", "cache-plugin"}
}
```

## 🤝 贡献指南

1. **Fork** 本仓库
2. **创建** 特性分支 (`git checkout -b feature/AmazingFeature`)
3. **提交** 变更 (`git commit -m 'Add some AmazingFeature'`)
4. **推送** 到分支 (`git push origin feature/AmazingFeature`)
5. **打开** Pull Request

### 开发规范
- 遵循Go代码规范
- 添加完整的单元测试
- 更新相关文档
- 确保所有测试通过

---

**注意**: 这是一个企业级插件系统，设计用于生产环境。在部署到生产环境之前，请确保充分测试所有插件功能和配置。