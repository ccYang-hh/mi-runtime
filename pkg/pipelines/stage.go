package pipelines

import (
	"fmt"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
)

// Stage 管道阶段接口
type Stage interface {
	// Name 获取阶段名称
	Name() string

	// Enabled 检查是否启用
	Enabled() bool

	// Process 处理请求上下文
	Process(context *rcx.RequestContext) error

	// Execute 执行阶段
	Execute(context *rcx.RequestContext) error

	// Initialize 初始化阶段
	Initialize() error

	// Shutdown 关闭阶段
	Shutdown() error

	// GetPrerequisites 获取前置依赖（用于DAG模式）
	GetPrerequisites() []string

	// GetNextStages 获取后续阶段（用于DAG模式）
	GetNextStages() []string
}

// BaseStage 基础阶段实现
type BaseStage struct {
	name          string
	enabled       bool
	initialized   bool
	config        sync.Map
	prerequisites []string // DAG模式使用
	nextStages    []string // DAG模式使用

	// 钩子管理器
	hookManager *HookManager

	// 同步控制
	mu sync.RWMutex
}

// NewBaseStage 创建基础阶段
func NewBaseStage(name string) *BaseStage {
	return &BaseStage{
		name:        name,
		enabled:     true,
		hookManager: NewHookManager(),
	}
}

// Name 获取阶段名称
func (s *BaseStage) Name() string {
	return s.name
}

// Enabled 检查是否启用
func (s *BaseStage) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// Enable 启用阶段
func (s *BaseStage) Enable() *BaseStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
	return s
}

// Disable 禁用阶段
func (s *BaseStage) Disable() *BaseStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
	return s
}

// Configure 配置阶段
func (s *BaseStage) Configure(config map[string]interface{}) *BaseStage {
	for key, value := range config {
		s.config.Store(key, value)
	}
	return s
}

// GetConfig 获取配置值
func (s *BaseStage) GetConfig(key string, defaultValue interface{}) interface{} {
	if value, ok := s.config.Load(key); ok {
		return value
	}
	return defaultValue
}

// AddPrerequisite 添加前置依赖（用于DAG模式）
func (s *BaseStage) AddPrerequisite(stageName string) *BaseStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prerequisites = append(s.prerequisites, stageName)
	return s
}

// GetPrerequisites 获取前置依赖
func (s *BaseStage) GetPrerequisites() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.prerequisites...)
}

// AddNextStage 添加后续阶段（用于DAG模式）
func (s *BaseStage) AddNextStage(stageName string) *BaseStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextStages = append(s.nextStages, stageName)
	return s
}

// GetNextStages 获取后续阶段
func (s *BaseStage) GetNextStages() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.nextStages...)
}

// AddStageHook 添加阶段钩子
func (s *BaseStage) AddStageHook(hook StageHook) *BaseStage {
	s.hookManager.AddStageHook(hook)
	return s
}

// Initialize 初始化阶段
func (s *BaseStage) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	s.initialized = true
	logger.Debugf("stage %s initialized", s.name)
	return nil
}

// Shutdown 关闭阶段
func (s *BaseStage) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.initialized = false
	logger.Debugf("stage %s shutdown", s.name)
	return nil
}

// Process 默认处理方法（需要被具体实现覆盖）
func (s *BaseStage) Process(context *rcx.RequestContext) error {
	return fmt.Errorf("stage %s does not implement Process method", s.name)
}

// Execute 执行阶段，包含钩子调用和错误处理
func (s *BaseStage) Execute(context *rcx.RequestContext) error {
	if !s.Enabled() {
		logger.Debugf("stage %s is disabled, skipping", s.name)
		return nil
	}

	// 执行前置钩子
	s.hookManager.ExecuteStageHooks("before", s, context, nil)

	// 执行处理逻辑
	logger.Debugf("executing stage %s", s.name)

	err := s.Process(context)

	if err != nil {
		logger.Errorf("stage %s execution failed: %v", s.name, err)

		// 执行错误钩子
		s.hookManager.ExecuteStageHooks("error", s, context, err)

		return err
	}

	logger.Debugf("stage %s completed successfully", s.name)

	// 执行后置钩子
	s.hookManager.ExecuteStageHooks("after", s, context, nil)

	return nil
}

// String 字符串表示
func (s *BaseStage) String() string {
	status := "enabled"
	if !s.enabled {
		status = "disabled"
	}

	return fmt.Sprintf("Stage(%s, %s)", s.name, status)
}

// ProcessorStage 基于处理器函数的阶段实现
type ProcessorStage struct {
	*BaseStage
	processor func(*rcx.RequestContext) error
}

// NewProcessorStage 创建基于处理器函数的阶段
func NewProcessorStage(name string, processor func(*rcx.RequestContext) error) *ProcessorStage {
	return &ProcessorStage{
		BaseStage: NewBaseStage(name),
		processor: processor,
	}
}

// Process 执行处理器函数
func (s *ProcessorStage) Process(context *rcx.RequestContext) error {
	if s.processor == nil {
		return fmt.Errorf("no processor function defined for stage %s", s.name)
	}
	return s.processor(context)
}
