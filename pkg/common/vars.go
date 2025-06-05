package common

import "time"

// InferenceScene 推理场景
type InferenceScene string

const (
	PD_DISAGGREGATION InferenceScene = "PD_DISAGGREGATION" // PD分离
	MULTI_INSTANCE    InferenceScene = "MULTI_INSTANCE"    // 多实例（非PD分离）
	SINGLE            InferenceScene = "SINGLE"            // 单实例
)

// RequestType 请求类型
type RequestType string

const (
	FIRST_TIME RequestType = "FIRST_TIME"
	HISTORY    RequestType = "HISTORY"
	RAG        RequestType = "RAG"
)

// RouteSource 路由源类型
type RouteSource int8

const (
	RouteSourceSystem RouteSource = iota
	RouteSourcePlugin
	RouteSourcePipeline
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
	ExecutionModeDAG ExecutionMode = "dag"
)
