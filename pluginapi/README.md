
# Plugin插件系统

## 架构

* Runtime基于Plugins构筑Pipeline
* Plugin基于pluginapi提供的抽象接口实现业务逻辑
* External Plugins需要引入pluginapi包实现完整逻辑后方可对接到Runtime中

```text
runtime <- internal_plugins <- pluginapi

runtime <- external_plugins <- pluginapi
```


## 🚨 备注

当前Plugin插件系统与Runtime存在耦合，并且将至少保持耦合关系至6.30版本前！

**因此,当前暂不支持外部Plugin对接Runtime!**

预期的解耦方式为:

* 独立pluginapi包,封装plugin相关的抽象定义,如IPluginConfig, Plugin\[T 如IPluginConfig\]等
* 由于Plugin依赖了Pipelines, 解决方案为:
  * 将Pipelines相关的抽象接口连同Plugins一同封装进pluginapi(同时支持ExternalPlugin, ExternalPipeline)
  * 只封装Plugins相关抽象定义, 其接口具体依赖的Pipelines资源通过依赖注入方式解决,如(该方式不支持ExternalPipeline,大概率弃用):
    ```textmate
    // pluginapi/interface.go
    package pluginapi
    
    type Plugin[T IPluginConfig] interface {
    GetName() string
    GetVersion() string
    Initialize(ctx context.Context, configDir ...string) error

    // 使用回调模式而不是返回具体类型
    RegisterPipelineStages(registry StageRegistry) error
    RegisterStageHooks(registry HookRegistry) error
    RegisterPipelineHooks(registry PipelineHookRegistry) error
    
    // 其他方法...
    }

    // 注册器接口
    type StageRegistry interface {
    RegisterStage(name string, executor StageExecutor)
    }
    
    type StageExecutor interface {
    Execute(ctx context.Context, data interface{}) (interface{}, error)
    }
    
    type HookRegistry interface {
    RegisterStageHook(stageName string, hook StageHookExecutor)
    }    
    ```