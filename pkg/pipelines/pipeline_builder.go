package pipelines

import (
	"fmt"
	"xfusion.com/tmatrix/runtime/pkg/common"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

type IPipelineBuilder interface {
	BuildPipelines() (map[string]IPipeline, error)
}

// PipelineBuilder ...
type PipelineBuilder struct {
	pluginManager plugins.IPluginManager
	config        *PipelineBuilderConfig
	pipelines     map[string]IPipeline
}

// NewPipelineBuilder 创建管道构建器
func NewPipelineBuilder(pluginManager plugins.IPluginManager, builderConfig *PipelineBuilderConfig) *PipelineBuilder {
	return &PipelineBuilder{
		pluginManager: pluginManager,
		config:        builderConfig,
		pipelines:     make(map[string]IPipeline),
	}
}

// createErrorHandler 创建错误处理器
func (pb *PipelineBuilder) createErrorHandler(handlerName string) func(*rcx.RequestContext, string, error) error {
	// TODO: 支持动态配置Error Handler
	logger.Warnf("dynamic error handler not implemented yet for: %s", handlerName)

	return func(ctx *rcx.RequestContext, pipelineName string, err error) error {
		logger.Errorf("pipeline %s error: %v", pipelineName, err)
		// 默认错误处理逻辑
		return nil
	}
}

// BuildPipeline 构建单个管道
func (pb *PipelineBuilder) BuildPipeline(config *config.PipelineConfig) (*Pipeline, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		logger.Errorf("pipeline config validation failed for %s: %v", config.PipelineName, err)
		return nil, err
	}

	// 设置执行器配置
	executorConfig := &ExecutorConfig{
		Mode:           config.Mode,
		MaxConcurrency: config.MaxConcurrency,
		FailFast:       false,
	}

	// 创建执行器
	executor := NewExecutor(executorConfig)

	// 创建管道
	pipeline := NewPipeline(config.PipelineName, executor)

	// 注册插件提供的阶段
	var stageNames []string
	var stageGroups [][]string
	for _, row := range config.Plugins {

		if pluginName, ok := row.(string); ok {
			// 获取Plugin提供的Stages
			stages := pb.pluginManager.GetPipelineStages(pluginName)
			for _, stage := range stages {
				if err := pipeline.RegisterStage(stage); err != nil {
					logger.Errorf("failed to register stage %s from plugin %s: %v", stage.Name(), plugin, err)
					return nil, err
				}
			}

			for _, stageName := range stages {
				stageNames = append(stageNames, stageName)
			}

			// 获取并添加管道钩子
			pipelineHooks := pb.pluginManager.GetPipelineHooks(pluginName)
			for _, hook := range pipelineHooks {
				pipeline.AddPipelineHook(hook)
			}

			// 获取并添加阶段钩子
			stageHooks := pb.pluginManager.GetStageHooks(pluginName)
			for _, hook := range stageHooks {
				pipeline.AddStageHook(hook)
			}
		}

		if pluginGroup, ok := row.([]string); ok {
			singleGroupStages := make([]string, 0)
			for _, pluginName := range pluginGroup {
				// 获取Plugin提供的Stages
				stages := pb.pluginManager.GetPipelineStages(pluginName)
				for _, stage := range stages {
					if err := pipeline.RegisterStage(stage); err != nil {
						logger.Errorf("failed to register stage %s from plugin %s: %v", stage.Name(), plugin, err)
						return nil, err
					}
				}

				for _, stageName := range stages {
					singleGroupStages = append(singleGroupStages, stageName)
				}

				// 获取并添加管道钩子
				pipelineHooks := pb.pluginManager.GetPipelineHooks(pluginName)
				for _, hook := range pipelineHooks {
					pipeline.AddPipelineHook(hook)
				}

				// 获取并添加阶段钩子
				stageHooks := pb.pluginManager.GetStageHooks(pluginName)
				for _, hook := range stageHooks {
					pipeline.AddStageHook(hook)
				}
			}
			stageGroups = append(stageGroups, singleGroupStages)
		}
	}

	// 配置阶段执行顺序
	switch config.Mode {
	case common.ExecutionModeSequential:
		if err := pipeline.AddStageSequential(stageNames); err != nil {
			logger.Errorf("failed to add sequential stages to pipeline %s: %v",
				config.PipelineName, err)
			return nil, err
		}

	case common.ExecutionModeParallel:
		if err := pipeline.AddStageParallel(stageGroups); err != nil {
			logger.Errorf("failed to add parallel stages to pipeline %s: %v",
				config.PipelineName, err)
			return nil, err
		}

	default:
		logger.Warnf("unsupported execution mode %s for pipeline %s, using sequential",
			config.Mode, config.PipelineName)

		// 尝试作为串行处理
		stageNames, err := config.GetStagesAsSequential()
		if err != nil {
			return nil, fmt.Errorf("failed to parse stages for pipeline %s: %w",
				config.PipelineName, err)
		}

		if err := pipeline.AddStageSequential(stageNames); err != nil {
			return nil, fmt.Errorf("failed to add stages to pipeline %s: %w",
				config.PipelineName, err)
		}
	}

	// 设置错误处理器
	if config.ErrorHandler != "" {
		handler := pb.createErrorHandler(config.ErrorHandler)
		pipeline.SetErrorHandler(handler)
	}

	// 添加路由
	for _, route := range config.Routes {
		pipeline.AddRoute(route)
	}

	// 验证管道
	if err := pipeline.Validate(); err != nil {
		logger.Errorf("pipeline %s validation failed: %v", config.PipelineName, err)
		return nil, err
	}

	logger.Infof("pipeline %s built successfully", config.PipelineName)
	return pipeline, nil
}
