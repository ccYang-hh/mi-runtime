package pipelines

import (
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
)

// StageHook 阶段钩子接口
// 钩子可以在阶段处理的不同点执行，用于添加横切关注点
// 如日志记录、指标收集、调试等，而不必修改阶段的核心逻辑
type StageHook interface {
	// Name 获取钩子名称
	Name() string

	// BeforeStage 在阶段执行前调用
	BeforeStage(stage Stage, context *rcx.RequestContext) error

	// AfterStage 在阶段执行后调用
	AfterStage(stage Stage, context *rcx.RequestContext) error

	// OnError 在阶段执行出错时调用
	OnError(stage Stage, context *rcx.RequestContext, err error) error
}

// PipelineHook 管道钩子接口
// 与阶段钩子类似，但作用于整个管道的执行流程
type PipelineHook interface {
	// Name 获取钩子名称
	Name() string

	// BeforePipeline 在管道开始处理请求之前调用
	BeforePipeline(context *rcx.RequestContext) error

	// AfterPipeline 在管道完成处理请求之后调用
	AfterPipeline(context *rcx.RequestContext) error

	// OnPipelineError 在管道处理请求出错时调用
	OnPipelineError(context *rcx.RequestContext, err error) error
}

// BaseStageHook 基础阶段钩子实现
type BaseStageHook struct {
	name string
}

// NewBaseStageHook 创建基础阶段钩子
func NewBaseStageHook(name string) *BaseStageHook {
	return &BaseStageHook{name: name}
}

func (h *BaseStageHook) Name() string {
	return h.name
}

func (h *BaseStageHook) BeforeStage(stage Stage, context *rcx.RequestContext) error {
	// 默认空实现
	return nil
}

func (h *BaseStageHook) AfterStage(stage Stage, context *rcx.RequestContext) error {
	// 默认空实现
	return nil
}

func (h *BaseStageHook) OnError(stage Stage, context *rcx.RequestContext, err error) error {
	// 默认空实现
	return nil
}

// BasePipelineHook 基础管道钩子实现
type BasePipelineHook struct {
	name string
}

// NewBasePipelineHook 创建基础管道钩子
func NewBasePipelineHook(name string) *BasePipelineHook {
	return &BasePipelineHook{name: name}
}

func (h *BasePipelineHook) Name() string {
	return h.name
}

func (h *BasePipelineHook) BeforePipeline(context *rcx.RequestContext) error {
	// 默认空实现
	return nil
}

func (h *BasePipelineHook) AfterPipeline(context *rcx.RequestContext) error {
	// 默认空实现
	return nil
}

func (h *BasePipelineHook) OnPipelineError(context *rcx.RequestContext, err error) error {
	// 默认空实现
	return nil
}

// StageHookFunc 函数式阶段钩子
type StageHookFunc struct {
	name        string
	beforeStage func(Stage, *rcx.RequestContext) error
	afterStage  func(Stage, *rcx.RequestContext) error
	onError     func(Stage, *rcx.RequestContext, error) error
}

// NewStageHookFunc 创建函数式阶段钩子
func NewStageHookFunc(name string) *StageHookFunc {
	return &StageHookFunc{name: name}
}

func (h *StageHookFunc) Name() string {
	return h.name
}

func (h *StageHookFunc) BeforeStage(stage Stage, context *rcx.RequestContext) error {
	if h.beforeStage != nil {
		return h.beforeStage(stage, context)
	}
	return nil
}

func (h *StageHookFunc) AfterStage(stage Stage, context *rcx.RequestContext) error {
	if h.afterStage != nil {
		return h.afterStage(stage, context)
	}
	return nil
}

func (h *StageHookFunc) OnError(stage Stage, context *rcx.RequestContext, err error) error {
	if h.onError != nil {
		return h.onError(stage, context, err)
	}
	return nil
}

// SetBeforeStage 设置前置处理函数
func (h *StageHookFunc) SetBeforeStage(fn func(Stage, *rcx.RequestContext) error) *StageHookFunc {
	h.beforeStage = fn
	return h
}

// SetAfterStage 设置后置处理函数
func (h *StageHookFunc) SetAfterStage(fn func(Stage, *rcx.RequestContext) error) *StageHookFunc {
	h.afterStage = fn
	return h
}

// SetOnError 设置错误处理函数
func (h *StageHookFunc) SetOnError(fn func(Stage, *rcx.RequestContext, error) error) *StageHookFunc {
	h.onError = fn
	return h
}

// PipelineHookFunc 函数式管道钩子
type PipelineHookFunc struct {
	name            string
	beforePipeline  func(*rcx.RequestContext) error
	afterPipeline   func(*rcx.RequestContext) error
	onPipelineError func(*rcx.RequestContext, error) error
}

// NewPipelineHookFunc 创建函数式管道钩子
func NewPipelineHookFunc(name string) *PipelineHookFunc {
	return &PipelineHookFunc{name: name}
}

func (h *PipelineHookFunc) Name() string {
	return h.name
}

func (h *PipelineHookFunc) BeforePipeline(context *rcx.RequestContext) error {
	if h.beforePipeline != nil {
		return h.beforePipeline(context)
	}
	return nil
}

func (h *PipelineHookFunc) AfterPipeline(context *rcx.RequestContext) error {
	if h.afterPipeline != nil {
		return h.afterPipeline(context)
	}
	return nil
}

func (h *PipelineHookFunc) OnPipelineError(context *rcx.RequestContext, err error) error {
	if h.onPipelineError != nil {
		return h.onPipelineError(context, err)
	}
	return nil
}

// SetBeforePipeline 设置管道前置处理函数
func (h *PipelineHookFunc) SetBeforePipeline(fn func(*rcx.RequestContext) error) *PipelineHookFunc {
	h.beforePipeline = fn
	return h
}

// SetAfterPipeline 设置管道后置处理函数
func (h *PipelineHookFunc) SetAfterPipeline(fn func(*rcx.RequestContext) error) *PipelineHookFunc {
	h.afterPipeline = fn
	return h
}

// SetOnPipelineError 设置管道错误处理函数
func (h *PipelineHookFunc) SetOnPipelineError(fn func(*rcx.RequestContext, error) error) *PipelineHookFunc {
	h.onPipelineError = fn
	return h
}

// HookManager 钩子管理器 - 用于安全地执行钩子
type HookManager struct {
	stageHooks    []StageHook
	pipelineHooks []PipelineHook
}

// NewHookManager 创建钩子管理器
func NewHookManager() *HookManager {
	return &HookManager{
		stageHooks:    make([]StageHook, 0),
		pipelineHooks: make([]PipelineHook, 0),
	}
}

// AddStageHook 添加阶段钩子
func (hm *HookManager) AddStageHook(hook StageHook) {
	hm.stageHooks = append(hm.stageHooks, hook)
}

// AddPipelineHook 添加管道钩子
func (hm *HookManager) AddPipelineHook(hook PipelineHook) {
	hm.pipelineHooks = append(hm.pipelineHooks, hook)
}

// ExecuteStageHooks 执行阶段钩子
func (hm *HookManager) ExecuteStageHooks(hookType string, stage Stage, context *rcx.RequestContext, err error) {
	for _, hook := range hm.stageHooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("stage hook %s panic: %v", hook.Name(), r)
				}
			}()

			var hookErr error
			switch hookType {
			case "before":
				hookErr = hook.BeforeStage(stage, context)
			case "after":
				hookErr = hook.AfterStage(stage, context)
			case "error":
				hookErr = hook.OnError(stage, context, err)
			}

			if hookErr != nil {
				logger.Errorf("stage hook %s execution failed: %v", hook.Name(), hookErr)
			}
		}()
	}
}

// ExecutePipelineHooks 执行管道钩子
func (hm *HookManager) ExecutePipelineHooks(hookType string, context *rcx.RequestContext, err error) {
	for _, hook := range hm.pipelineHooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("pipeline hook %s panic: %v", hook.Name(), r)
				}
			}()

			var hookErr error
			switch hookType {
			case "before":
				hookErr = hook.BeforePipeline(context)
			case "after":
				hookErr = hook.AfterPipeline(context)
			case "error":
				hookErr = hook.OnPipelineError(context, err)
			}

			if hookErr != nil {
				logger.Errorf("pipeline hook %s execution failed: %v", hook.Name(), hookErr)
			}
		}()
	}
}
