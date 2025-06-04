package pipelines

import (
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

type IPipelineBuilder interface {
	BuildPipelines() (map[string]IPipeline, error)
}

type PipelineBuilder struct{}

func NewPipelineBuilder(pluginManager plugins.IPluginManager,
	builderConfig *PipelineBuilderConfig) IPipelineBuilder {
	return &PipelineBuilder{}
}

func (pb *PipelineBuilder) BuildPipelines() (map[string]IPipeline, error) {
	pipelines := make(map[string]IPipeline)
	return pipelines, nil
}
