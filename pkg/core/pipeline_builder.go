package core

import (
	"fmt"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/config"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
	"xfusion.com/tmatrix/runtime/pkg/plugins"
)

// PipelineBuilderConfig 构建器配置
type PipelineBuilderConfig struct {
	// Pipelines 配置文件注册的Pipeline
	Pipelines []*config.PipelineConfig `json:"pipelines" yaml:"pipelines"`

	// Default 默认Pipeline
	Default *config.PipelineConfig `json:"default" yaml:"default"`
}

type IPipelineBuilder interface {
	// BuildPipelines 构建Pipelines
	BuildPipelines() (map[string]pipelines.IPipeline, error)

	// ListPipelines 列出所有已构建的管道
	ListPipelines() map[string]pipelines.IPipeline

	// GetPipeline 获取指定Pipeline
	GetPipeline(pipelineName string) pipelines.IPipeline

	// Initialize 初始化
	Initialize() error

	// Shutdown 关闭
	Shutdown() error

	// Stats 获取构建器统计信息
	Stats() map[string]interface{}
}

// PipelineBuilder ...
type PipelineBuilder struct {
	pluginManager plugins.IPluginManager
	config        *PipelineBuilderConfig
	pipelines     map[string]pipelines.IPipeline
}

// NewPipelineBuilder 创建管道构建器
func NewPipelineBuilder(pluginManager plugins.IPluginManager, builderConfig *PipelineBuilderConfig) *PipelineBuilder {
	return &PipelineBuilder{
		pluginManager: pluginManager,
		config:        builderConfig,
		pipelines:     make(map[string]pipelines.IPipeline),
	}
}

// createErrorHandler 创建错误处理器
func (pb *PipelineBuilder) createErrorHandler() func(*rcx.RequestContext, string, error) error {
	// TODO: 支持动态配置Error Handler
	logger.Warnf("dynamic error handler not implemented yet")

	return func(ctx *rcx.RequestContext, pipelineName string, err error) error {
		logger.Errorf("pipeline %s error: %v", pipelineName, err)
		// 默认错误处理逻辑
		return fmt.Errorf("pipeline %s error: %v", pipelineName, err)
	}
}

// BuildPipeline 构建单个管道
func (pb *PipelineBuilder) BuildPipeline(config *config.PipelineConfig) (pipelines.IPipeline, error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		logger.Errorf("pipeline config validation failed for %s: %v", config.PipelineName, err)
		return nil, err
	}

	// 设置Executor Config
	executorConfig := &pipelines.ExecutorConfig{
		Mode:           config.Mode,
		MaxConcurrency: config.MaxConcurrency,
		FailFast:       false,
	}

	// 创建Executor
	executor := pipelines.NewExecutor(executorConfig)

	// 创建Pipeline
	pipeline := pipelines.NewPipeline(config.PipelineName, executor)

	// 注册插件提供的Stage、StageHook、PipelineHook
	var stageGroup pipelines.StageGroup
	var stageGroups pipelines.StageGroups
	for _, row := range config.Plugins {

		// 如果plugins是[]string类型
		if pluginName, ok := row.(string); ok {
			// 获取Plugin提供的Stages
			stages := pb.pluginManager.GetPipelineStages(pluginName)

			// 注册Stage
			for _, stage := range stages {
				if err := pipeline.RegisterStage(stage); err != nil {
					logger.Errorf("failed to register stage %s from plugin %s: %v", stage.Name(), pluginName, err)
					return nil, err
				}
			}

			for _, stage := range stages {
				stageGroup = append(stageGroup, stage)
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

		// 如果plugins是[][]string类型
		if pluginGroup, ok := row.([]string); ok {
			singleGroupStages := pipelines.StageGroup{}
			for _, pluginName := range pluginGroup {
				// 获取Plugin提供的Stages
				stages := pb.pluginManager.GetPipelineStages(pluginName)
				for _, stage := range stages {
					if err := pipeline.RegisterStage(stage); err != nil {
						logger.Errorf("failed to register stage %s from plugin %s: %v", stage.Name(), pluginName, err)
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
		if err := pipeline.AddStageSequential(stageGroup); err != nil {
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
		logger.Errorf("unsupported execution mode %s for pipeline %s, using sequential",
			config.Mode, config.PipelineName)

		// 尝试作为串行处理
		if err := pipeline.AddStageSequential(stageGroup); err != nil {
			return nil, fmt.Errorf("failed to add stages to pipeline %s: %w",
				config.PipelineName, err)
		}
	}

	// 设置错误处理器
	handler := pb.createErrorHandler()
	pipeline.SetErrorHandler(handler)

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

func (pb *PipelineBuilder) BuildPipelines() (map[string]pipelines.IPipeline, error) {
	// 构建通用Pipeline
	for _, pipelineConfig := range pb.config.Pipelines {
		if _, ok := pb.pipelines[pipelineConfig.PipelineName]; ok {
			logger.Warnf("pipeline %s has been built", pipelineConfig.PipelineName)
			continue
		}

		// 构建Pipeline
		pipeline, err := pb.BuildPipeline(pipelineConfig)
		if err != nil {
			logger.Errorf("build pipeline err: %+v", err)
			return nil, fmt.Errorf("build pipeline err: %+v", err)
		}

		pb.pipelines[pipeline.GetName()] = pipeline
	}

	// 构建系统默认Pipeline
	if pb.pipelines["default"] == nil {
		pipeline, err := pb.BuildPipeline(pb.config.Default)
		if err != nil {
			logger.Errorf("build pipeline err: %+v", err)
			return nil, fmt.Errorf("build pipeline err: %+v", err)
		}
		pb.pipelines[pipeline.GetName()] = pipeline
	}

	return pb.pipelines, nil
}

// ListPipelines 列出所有已构建的管道
func (pb *PipelineBuilder) ListPipelines() map[string]pipelines.IPipeline {
	return pb.pipelines
}

// GetPipeline 获取指定Pipeline
func (pb *PipelineBuilder) GetPipeline(pipelineName string) pipelines.IPipeline {
	return pb.pipelines[pipelineName]
}

// Initialize 初始化
func (pb *PipelineBuilder) Initialize() error {
	return pb.InitializePipelines()
}

// InitializePipelines 初始化所有管道
func (pb *PipelineBuilder) InitializePipelines() error {
	var errors []error

	for name, pipeline := range pb.pipelines {
		if err := pipeline.Initialize(); err != nil {
			logger.Errorf("failed to initialize pipeline %s: %v", name, err)
			errors = append(errors, fmt.Errorf("pipeline %s initialization failed: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("pipeline initialization completed with %d errors", len(errors))
	}

	logger.Infof("all %d pipelines initialized successfully", len(pb.pipelines))
	return nil
}

// Shutdown 终止Builder
func (pb *PipelineBuilder) Shutdown() error {
	return pb.ShutdownPipelines()
}

// ShutdownPipelines 关闭所有管道
func (pb *PipelineBuilder) ShutdownPipelines() error {
	var errors []error

	for name, pipeline := range pb.pipelines {
		if err := pipeline.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown pipeline %s: %v", name, err)
			errors = append(errors, fmt.Errorf("pipeline %s shutdown failed: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("pipeline shutdown completed with %d errors", len(errors))
	}

	logger.Infof("all %d pipelines shutdown successfully", len(pb.pipelines))
	return nil
}

// Stats 获取构建器统计信息
func (pb *PipelineBuilder) Stats() map[string]interface{} {
	pipelineStats := make(map[string]interface{})

	for name, pipeline := range pb.pipelines {
		pipelineStats[name] = pipeline.Stats()
	}

	return map[string]interface{}{
		"total_pipelines": len(pb.pipelines),
		"pipeline_names":  pb.ListPipelines(),
		"pipelines":       pipelineStats,
	}
}
