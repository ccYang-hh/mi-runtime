package common

import "time"

// InferenceScene 推理场景
type InferenceScene string

const (
	// InferenceScenePD PD分离
	InferenceScenePD InferenceScene = "pd_disaggregation"

	// InferenceSceneMultiInstance 多实例（非PD分离）
	InferenceSceneMultiInstance InferenceScene = "multi_instance"

	// InferenceSceneSingle 单实例
	InferenceSceneSingle InferenceScene = "single"
)

// RequestType 请求类型
type RequestType string

const (
	// RequestTypeFirstTime 首次请求
	RequestTypeFirstTime RequestType = "first_time"

	// RequestTypeHistory 多轮对话
	RequestTypeHistory RequestType = "history"

	// RequestTypeRAG RAG
	RequestTypeRAG RequestType = "RAG"
)

// RouteSource 路由源类型
type RouteSource string

const (
	RouteSourceSystem   RouteSource = "system"
	RouteSourcePlugin   RouteSource = "plugin"
	RouteSourcePipeline RouteSource = "pipeline"
)

// RouteInfo 路由基础信息
type RouteInfo struct {
	Path   string `mapstructure:"path" json:"path" validate:"required"`
	Method string `mapstructure:"method" json:"method" validate:"required,oneof=GET POST PUT DELETE PATCH"`
}

// RegisteredRoute 已注册的路由信息
type RegisteredRoute struct {
	Name         string      `json:"name"`
	GroupPath    string      `json:"group_path"`
	Source       RouteSource `json:"source"`
	Priority     int         `json:"priority"`
	RegisteredAt time.Time   `json:"registered_at"`
}

// ExecutionMode 基于Pipeline的执行模式
type ExecutionMode string

const (
	// ExecutionModeSequential 串行执行模式
	// 配置: stages: [a, b, c]
	// 执行: a -> b -> c (严格顺序)
	ExecutionModeSequential ExecutionMode = "sequential"

	// ExecutionModeParallel 并行执行模式
	// 配置: stages: [[a,b,c], [d,e], [f,g]]
	// 执行: [a,b,c]并行 -> 等待全部完成 -> [d,e]并行 -> 等待全部完成 -> [f,g]并行
	ExecutionModeParallel ExecutionMode = "parallel"

	// ExecutionModeDAG DAG执行模式 (TODO: 未来版本支持)
	// 配置: 基于依赖关系的有向无环图
	// 执行: 完全灵活调度，单个stage粒度的并行优化
	ExecutionModeDAG ExecutionMode = "DAG"
)

// PluginInitMode 插件初始化模式
type PluginInitMode string

const (
	// InitModeNew 通过反射new结构体
	InitModeNew PluginInitMode = "new"

	// InitModeFactory 通过预注册的工厂函数
	InitModeFactory PluginInitMode = "factory"

	// InitModeReflect 通过反射调用构造函数
	InitModeReflect PluginInitMode = "reflect"

	// InitModePlugin 通过Go plugin包加载.so文件
	InitModePlugin PluginInitMode = "plugin"

	// InitModeSingleton 单例模式
	InitModeSingleton PluginInitMode = "singleton"

	// InitModePool 对象池模式
	InitModePool PluginInitMode = "pool"
)
