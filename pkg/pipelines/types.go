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
