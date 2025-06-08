package plugins

import (
	"context"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// PluginDaemonProcessor Daemon插件处理器接口
type PluginDaemonProcessor interface {
	// Start 启动Daemon处理器
	Start(ctx context.Context) error

	// Stop 停止Daemon处理器
	Stop(ctx context.Context) error

	// IsRunning 检查是否正在运行
	IsRunning() bool

	// GetStatus 获取处理器状态信息
	GetStatus() map[string]interface{}
}

// BaseDaemonProcessor Daemon处理器基础实现
type BaseDaemonProcessor struct {
	name      string
	running   bool
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
	stopTime  time.Time
}

// NewBaseDaemonProcessor 创建基础Daemon处理器
func NewBaseDaemonProcessor(name string) *BaseDaemonProcessor {
	return &BaseDaemonProcessor{
		name: name,
	}
}

func (p *BaseDaemonProcessor) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		logger.Warnf("Daemon processor %s already running", p.name)
		return nil
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.running = true
	p.startTime = time.Now()

	logger.Infof("Daemon processor %s started", p.name)
	return nil
}

func (p *BaseDaemonProcessor) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	if p.cancel != nil {
		p.cancel()
	}

	p.running = false
	p.stopTime = time.Now()

	logger.Infof("Daemon processor %s stopped", p.name)
	return nil
}

func (p *BaseDaemonProcessor) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.running
}

func (p *BaseDaemonProcessor) GetStatus() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := map[string]interface{}{
		"name":    p.name,
		"running": p.running,
	}

	if !p.startTime.IsZero() {
		status["start_time"] = p.startTime
	}

	if !p.stopTime.IsZero() {
		status["stop_time"] = p.stopTime
	}

	if p.running {
		status["uptime"] = time.Since(p.startTime).String()
	}

	return status
}

func (p *BaseDaemonProcessor) GetContext() context.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.ctx == nil {
		return context.Background()
	}

	return p.ctx
}
