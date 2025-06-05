package pipelines

import (
	"xfusion.com/tmatrix/runtime/pkg/config"
)

// PipelineBuilderConfig 构建器配置
type PipelineBuilderConfig struct {
	// Pipelines 配置文件注册的Pipeline
	Pipelines []*config.PipelineConfig `json:"pipelines"`

	// Default 默认Pipeline
	Default *config.PipelineConfig `json:"default"`
}

// StageGroup 阶段组 - 用于并行模式的分组执行
type StageGroup []Stage

// StageGroups 多个阶段组 - 用于并行模式的多层执行
type StageGroups []StageGroup
