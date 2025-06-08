package pipelines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
)

type IPipeline interface {
	// GetName Pipeline名称
	GetName() string

	// Initialize 初始化Pipeline
	Initialize() error

	// Shutdown 终止Pipeline
	Shutdown() error

	// Stats Pipeline信息统计
	Stats() map[string]interface{}

	// Process 执行Pipeline
	Process(ctx context.Context, rc *rcx.RequestContext) (*rcx.RequestContext, error)

	// GetRoutes 获取Pipeline的前端路由
	GetRoutes() []common.RouteInfo
}

// Pipeline 请求处理管道
type Pipeline struct {
	name string

	// 阶段管理
	stages        []Stage          // 串行模式使用
	stageGroups   StageGroups      // 并行模式使用
	stageRegistry map[string]Stage // 阶段注册表

	// 钩子管理器
	hookManager *HookManager

	// 错误处理
	errorHandler func(*rcx.RequestContext, string, error) error

	// 路由配置
	routes []common.RouteInfo

	// 执行器
	executor *Executor

	// 状态管理
	validated   bool
	initialized bool
	shutdown    bool
	mu          sync.RWMutex
}

var _ IPipeline = (*Pipeline)(nil)

// NewPipeline 创建新的管道
func NewPipeline(name string, executor *Executor) *Pipeline {
	return &Pipeline{
		name:          name,
		stageRegistry: make(map[string]Stage),
		hookManager:   NewHookManager(),
		executor:      executor,
	}
}

// GetName 获取管道名称
func (p *Pipeline) GetName() string {
	return p.name
}

// GetRoutes 获取Pipeline的前端路由
func (p *Pipeline) GetRoutes() []common.RouteInfo {
	return p.routes
}

// RegisterStage 注册阶段到注册表
func (p *Pipeline) RegisterStage(stage Stage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.stageRegistry[stage.Name()]; exists {
		logger.Errorf("stage %s already registered", stage.Name())
		return fmt.Errorf("stage %s already registered", stage.Name())
	}

	p.stageRegistry[stage.Name()] = stage
	p.validated = false // 重置验证状态

	logger.Debugf("stage %s registered successfully", stage.Name())
	return nil
}

// GetStage 根据名称获取阶段
func (p *Pipeline) GetStage(name string) (Stage, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stage, exists := p.stageRegistry[name]
	return stage, exists
}

// AddStageSequential 添加串行阶段
// 使用方式: pipeline.AddStageSequential([]string{"a", "b", "c"})
func (p *Pipeline) AddStageSequential(stageGroup StageGroup) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 二次校验
	stages := make([]Stage, 0, len(stageGroup))
	for _, stageItem := range stageGroup {
		stage, exists := p.stageRegistry[stageItem.Name()]
		if !exists {
			logger.Errorf("stage %s not registered", stageItem.Name())
			return fmt.Errorf("stage %s not found in registry", stageItem.Name())
		}
		stages = append(stages, stage)
	}

	p.stages = stages
	p.stageGroups = nil // 清空并行配置
	p.executor.config.Mode = common.ExecutionModeSequential
	p.validated = false

	logger.Infof("pipeline %s configured with %d sequential stages", p.name, len(stages))
	return nil
}

// AddStageParallel 添加并行阶段组
// 使用方式: pipeline.AddStageParallel([][]string{{"a","b","c"}, {"d","e"}, {"f","g"}})
func (p *Pipeline) AddStageParallel(stageGroups StageGroups) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	targetStageGroups := make(StageGroups, 0, len(stageGroups))

	// 二次校验
	for _, groupItem := range stageGroups {
		group := make(StageGroup, 0, len(groupItem))

		for _, stageItem := range groupItem {
			stage, exists := p.stageRegistry[stageItem.Name()]
			if !exists {
				logger.Errorf("stage %s not registered", stageItem.Name())
				return fmt.Errorf("stage %s not found in registry", stageItem.Name())
			}
			group = append(group, stage)
		}

		targetStageGroups = append(targetStageGroups, group)
	}

	p.stageGroups = targetStageGroups
	p.stages = nil // 清空串行配置
	p.executor.config.Mode = common.ExecutionModeParallel
	p.validated = false

	logger.Infof("pipeline %s configured with %d parallel stage groups", p.name, len(stageGroups))
	return nil
}

// AddPipelineHook 添加管道钩子
func (p *Pipeline) AddPipelineHook(hook PipelineHook) *Pipeline {
	p.hookManager.AddPipelineHook(hook)
	return p
}

// AddStageHook 添加阶段钩子（应用到所有阶段）
func (p *Pipeline) AddStageHook(hook StageHook) *Pipeline {
	p.hookManager.AddStageHook(hook)
	return p
}

// SetErrorHandler 设置错误处理器
func (p *Pipeline) SetErrorHandler(handler func(*rcx.RequestContext, string, error) error) *Pipeline {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.errorHandler = handler
	return p
}

// AddRoute 添加路由配置
func (p *Pipeline) AddRoute(route common.RouteInfo) *Pipeline {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.routes = append(p.routes, route)
	return p
}

// SetExecutor 设置执行器
func (p *Pipeline) SetExecutor(executor *Executor) *Pipeline {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.executor = executor
	return p
}

// Validate 验证管道配置
func (p *Pipeline) Validate() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.validated {
		return nil
	}

	// 验证阶段配置
	switch p.executor.config.Mode {
	case common.ExecutionModeSequential:
		if len(p.stages) == 0 {
			return fmt.Errorf("pipeline %s has no stages", p.name)
		}
	case common.ExecutionModeParallel:
		if len(p.stageGroups) == 0 {
			return fmt.Errorf("pipeline %s has no stage groups", p.name)
		}
	case common.ExecutionModeDAG:
		logger.Errorf("DAG Execution Mode not supported yet")
		return fmt.Errorf("DAG Execution Mode not supported yet")
	}

	// 验证所有阶段都已注册
	allStages := p.getAllStages()
	for _, stage := range allStages {
		if _, exists := p.stageRegistry[stage.Name()]; !exists {
			return fmt.Errorf("stage %s not registered", stage.Name())
		}
	}

	p.validated = true
	logger.Infof("pipeline %s validation completed successfully", p.name)
	return nil
}

// getAllStages 获取所有阶段（用于验证）
func (p *Pipeline) getAllStages() []Stage {
	var allStages []Stage

	if p.stages != nil {
		allStages = append(allStages, p.stages...)
	}

	if p.stageGroups != nil {
		for _, group := range p.stageGroups {
			allStages = append(allStages, group...)
		}
	}

	return allStages
}

// Initialize 初始化管道
func (p *Pipeline) Initialize() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	// 验证配置
	if err := p.Validate(); err != nil {
		logger.Errorf("pipeline %s validation failed: %v", p.name, err)
		return err
	}

	// 初始化所有阶段
	allStages := p.getAllStages()
	for _, stage := range allStages {
		if err := stage.Initialize(); err != nil {
			logger.Errorf("failed to initialize stage %s in pipeline %s: %v",
				stage.Name(), p.name, err)
			return fmt.Errorf("failed to initialize stage %s: %w", stage.Name(), err)
		}
	}

	p.initialized = true
	logger.Infof("pipeline %s initialized successfully with %d stages", p.name, len(allStages))
	return nil
}

// Shutdown 关闭管道
func (p *Pipeline) Shutdown() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.shutdown {
		return nil
	}

	// 关闭所有阶段
	var errors []error
	allStages := p.getAllStages()
	for _, stage := range allStages {
		if err := stage.Shutdown(); err != nil {
			logger.Errorf("failed to shutdown stage %s in pipeline %s: %v",
				stage.Name(), p.name, err)
			errors = append(errors, err)
		}
	}

	p.shutdown = true
	p.initialized = false

	if len(errors) > 0 {
		return fmt.Errorf("shutdown completed with %d errors", len(errors))
	}

	logger.Infof("pipeline %s shutdown completed", p.name)
	return nil
}

// Process 处理请求
func (p *Pipeline) Process(ctx context.Context, rc *rcx.RequestContext) (*rcx.RequestContext, error) {
	return p.ProcessWithContext(rc)
}

// ProcessWithContext 使用给定的请求上下文处理请求
func (p *Pipeline) ProcessWithContext(ctx *rcx.RequestContext) (*rcx.RequestContext, error) {
	// 确保管道已初始化
	if !p.initialized {
		if err := p.Initialize(); err != nil {
			logger.Errorf("failed to initialize pipeline %s: %v", p.name, err)
			return ctx, err
		}
	}

	// 检查是否关闭
	if p.shutdown {
		err := fmt.Errorf("pipeline %s is shutdown", p.name)
		logger.Errorf("attempt to process request on shutdown pipeline: %v", err)
		return ctx, err
	}

	startTime := time.Now()
	logger.Infof("pipeline %s started processing request %s", p.name, ctx.ContextID)

	// 执行前置钩子
	p.hookManager.ExecutePipelineHooks("before", ctx, nil)

	defer func() {
		// 执行后置钩子
		p.hookManager.ExecutePipelineHooks("after", ctx, nil)

		duration := time.Since(startTime)
		logger.Infof("pipeline %s completed processing request %s in %v", p.name, ctx.ContextID, duration)
	}()

	// 根据模式执行阶段
	var err error
	switch p.executor.config.Mode {
	case common.ExecutionModeSequential:
		err = p.executor.Execute(ctx, p.stages)
	case common.ExecutionModeParallel:
		err = p.executor.Execute(ctx, p.stageGroups)
	default:
		err = fmt.Errorf("unsupported execution mode: %s", p.executor.config.Mode)
	}

	if err != nil {
		logger.Errorf("pipeline %s execution failed for request %s: %v", p.name, ctx.ContextID, err)

		// 尝试错误处理
		if p.errorHandler != nil {
			if handlerErr := p.errorHandler(ctx, p.name, err); handlerErr != nil {
				logger.Errorf("error handler failed in pipeline %s: %v", p.name, handlerErr)
			}
		}

		// 执行错误钩子
		p.hookManager.ExecutePipelineHooks("error", ctx, err)

		return ctx, err
	}

	return ctx, nil
}

// Stats 获取管道统计信息
func (p *Pipeline) Stats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var stageInfo interface{}
	var stageCount int

	switch p.executor.config.Mode {
	case common.ExecutionModeSequential:
		stageNames := make([]string, len(p.stages))
		for i, stage := range p.stages {
			stageNames[i] = stage.Name()
		}
		stageInfo = stageNames
		stageCount = len(p.stages)

	case common.ExecutionModeParallel:
		groupInfo := make([][]string, len(p.stageGroups))
		totalStages := 0
		for i, group := range p.stageGroups {
			groupInfo[i] = make([]string, len(group))
			for j, stage := range group {
				groupInfo[i][j] = stage.Name()
			}
			totalStages += len(group)
		}
		stageInfo = groupInfo
		stageCount = totalStages
	}

	return map[string]interface{}{
		"name":        p.name,
		"mode":        p.executor.config.Mode,
		"stages":      stageInfo,
		"stage_count": stageCount,
		"route_count": len(p.routes),
		"validated":   p.validated,
		"initialized": p.initialized,
		"shutdown":    p.shutdown,
	}
}

// String 字符串表示
func (p *Pipeline) String() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "not validated"
	if p.validated {
		status = "validated"
	}
	if p.initialized {
		status = "initialized"
	}
	if p.shutdown {
		status = "shutdown"
	}

	stageCount := len(p.getAllStages())

	return fmt.Sprintf("Pipeline(%s, %s, mode=%s, stages=%d)",
		p.name, status, p.executor.config.Mode, stageCount)
}
