package pipelines

import (
	"fmt"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	// 执行模式
	Mode common.ExecutionMode `json:"mode" yaml:"mode"`

	// 并行度（仅在并行模式下有效）
	MaxConcurrency int `json:"max_concurrency" yaml:"max_concurrency"`

	// 是否快速失败
	FailFast bool `json:"fail_fast" yaml:"fail_fast"`
}

// DefaultExecutorConfig 默认执行器配置
func DefaultExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		Mode:           common.ExecutionModeSequential,
		MaxConcurrency: 10,
		FailFast:       false,
	}
}

// Executor 管道执行器
type Executor struct {
	config *ExecutorConfig
}

// NewExecutor 创建执行器
func NewExecutor(config *ExecutorConfig) *Executor {
	if config == nil {
		config = DefaultExecutorConfig()
	}
	return &Executor{config: config}
}

// ExecuteSequential 顺序执行阶段
// 配置格式: stages: [a, b, c]
// 执行顺序: a -> b -> c (严格串行)
func (e *Executor) ExecuteSequential(ctx *rcx.RequestContext, stages []Stage) error {
	logger.Infof("executing %d stages in sequential mode", len(stages))

	for i, stage := range stages {
		select {
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		default:
		}

		logger.Debugf("executing stage %d/%d: %s", i+1, len(stages), stage.Name())

		// 执行阶段
		if err := stage.Execute(ctx); err != nil {
			logger.Errorf("sequential stage %s execution failed: %v", stage.Name(), err)

			if e.config.FailFast {
				logger.Errorf("stage %s sequential execution failed, err: %+v", stage.Name(), err)
				return fmt.Errorf("stage %s sequential execution failed, err: %+v", stage.Name(), err)
			}

			// 记录错误但继续执行
			ctx.Fail(err)
		}
	}

	logger.Infof("sequential execution completed for %d stages", len(stages))
	return nil
}

// ExecuteParallel 并行执行阶段
// 配置格式: stages: [[a,b,c], [d,e], [f,g]]
// 执行顺序: [a,b,c]并行 -> 等待全部完成 -> [d,e]并行 -> 等待全部完成 -> [f,g]并行
func (e *Executor) ExecuteParallel(ctx *rcx.RequestContext, stageGroups StageGroups) error {
	logger.Infof("executing %d stage groups in parallel mode", len(stageGroups))

	for groupIndex, group := range stageGroups {
		select {
		case <-ctx.Context().Done():
			return ctx.Context().Err()
		default:
		}

		logger.Debugf("executing stage group %d/%d with %d stages",
			groupIndex+1, len(stageGroups), len(group))

		if err := e.executeStageGroup(ctx, group, groupIndex); err != nil {
			logger.Errorf("stage group %d execution failed: %v", groupIndex, err)

			if e.config.FailFast {
				return err
			}

			// 记录错误但继续执行下一组
			ctx.Fail(err)
		}
	}

	logger.Infof("parallel execution completed for %d stage groups", len(stageGroups))
	return nil
}

// executeStageGroup 执行单个阶段组（组内并行）
func (e *Executor) executeStageGroup(ctx *rcx.RequestContext, group StageGroup, groupIndex int) error {
	if len(group) == 0 {
		return nil
	}

	// 如果只有一个阶段，直接执行
	if len(group) == 1 {
		return group[0].Execute(ctx)
	}

	// 创建信号量控制并发度
	semaphore := make(chan struct{}, e.config.MaxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error
	errors := make([]error, 0)

	// 并行执行组内所有阶段
	for _, stage := range group {
		wg.Add(1)

		go func(s Stage) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Context().Done():
				return
			default:
			}

			// 执行阶段
			if err := s.Execute(ctx); err != nil {
				logger.Errorf("parallel stage %s in group %d execution failed: %v",
					s.Name(), groupIndex, err)

				mu.Lock()
				errors = append(errors, err)
				if firstError == nil {
					firstError = fmt.Errorf("parallel execution failed in group %d, err: %+v", groupIndex, err)
				}
				mu.Unlock()

				if e.config.FailFast {
					ctx.Cancel() // 取消其他阶段
				}
			}
		}(stage)
	}

	// 等待所有阶段完成
	wg.Wait()

	// 处理错误
	if len(errors) > 0 {
		if e.config.FailFast {
			return firstError
		}

		return firstError
	}

	return nil
}

// ExecuteDAG DAG执行模式（TODO: 未来版本实现）
// 这将支持完全灵活的依赖关系调度
func (e *Executor) ExecuteDAG(ctx *rcx.RequestContext, stages []Stage) error {
	// TODO: 实现基于依赖关系的有向无环图执行
	// 1. 构建依赖图
	// 2. 拓扑排序
	// 3. 并行执行无依赖的阶段
	// 4. 动态调度后续阶段

	logger.Warnf("DAG execution mode not implemented yet, falling back to sequential")
	return e.ExecuteSequential(ctx, stages)
}

// Execute 根据模式执行阶段
func (e *Executor) Execute(ctx *rcx.RequestContext, stages interface{}) error {
	switch e.config.Mode {
	case common.ExecutionModeSequential:
		if stageList, ok := stages.([]Stage); ok {
			return e.ExecuteSequential(ctx, stageList)
		}
		return fmt.Errorf("sequential mode requires []Stage, got %T", stages)

	case common.ExecutionModeParallel:
		if stageGroups, ok := stages.(StageGroups); ok {
			return e.ExecuteParallel(ctx, stageGroups)
		}
		return fmt.Errorf("parallel mode requires StageGroups, got %T", stages)

	case common.ExecutionModeDAG:
		if stageList, ok := stages.([]Stage); ok {
			return e.ExecuteDAG(ctx, stageList)
		}
		return fmt.Errorf("DAG mode requires []Stage, got %T", stages)

	default:
		return fmt.Errorf("unsupported execution mode: %s", e.config.Mode)
	}
}
