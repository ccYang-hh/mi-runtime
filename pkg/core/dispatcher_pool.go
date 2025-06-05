package core

import (
	"context"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	. "xfusion.com/tmatrix/runtime/pkg/components/pools"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
)

// DispatcherPool Dispatcher专用协程池
type DispatcherPool struct {
	*WorkerPool

	// Dispatcher特定的统计
	requestsProcessed int64
	avgResponseTime   time.Duration

	// 请求跟踪
	activeRequests map[string]*rcx.RequestContext
	requestsMu     sync.RWMutex
}

// NewDispatcherPool 创建Dispatcher专用协程池
func NewDispatcherPool(rootCtx context.Context, config WorkerPoolConfig) *DispatcherPool {
	if config.Name == "" {
		config.Name = "dispatcher"
	}

	return &DispatcherPool{
		WorkerPool:     NewWorkerPool(rootCtx, config),
		activeRequests: make(map[string]*rcx.RequestContext),
	}
}

// ProcessRequest 处理请求
func (dp *DispatcherPool) ProcessRequest(ctx context.Context, rc *rcx.RequestContext,
	processor func(context.Context, *rcx.RequestContext) error) bool {

	// 包装处理任务
	task := func() {
		dp.trackRequestStart(rc)
		defer dp.trackRequestEnd(rc)

		start := time.Now()
		err := processor(ctx, rc)
		duration := time.Since(start)

		if err != nil {
			logger.Errorf("request processing failed for %s: %v", rc.ContextID, err)
			rc.Fail(err)
		}

		// 更新响应时间统计
		dp.updateResponseTimeStats(duration)
	}

	return dp.Submit(task)
}

// trackRequestStart 开始跟踪请求
func (dp *DispatcherPool) trackRequestStart(reqCtx *rcx.RequestContext) {
	dp.requestsMu.Lock()
	defer dp.requestsMu.Unlock()

	dp.activeRequests[reqCtx.ContextID] = reqCtx
}

// trackRequestEnd 结束跟踪请求
func (dp *DispatcherPool) trackRequestEnd(reqCtx *rcx.RequestContext) {
	dp.requestsMu.Lock()
	defer dp.requestsMu.Unlock()

	delete(dp.activeRequests, reqCtx.ContextID)
	dp.requestsProcessed++
}

// updateResponseTimeStats 更新响应时间统计
func (dp *DispatcherPool) updateResponseTimeStats(duration time.Duration) {
	// 简单的移动平均算法
	if dp.avgResponseTime == 0 {
		dp.avgResponseTime = duration
	} else {
		dp.avgResponseTime = (dp.avgResponseTime*9 + duration) / 10
	}
}

// GetActiveRequests 获取活跃请求
func (dp *DispatcherPool) GetActiveRequests() map[string]*rcx.RequestContext {
	dp.requestsMu.RLock()
	defer dp.requestsMu.RUnlock()

	result := make(map[string]*rcx.RequestContext, len(dp.activeRequests))
	for k, v := range dp.activeRequests {
		result[k] = v
	}

	return result
}

// GetDispatcherStats 获取Dispatcher特定统计
func (dp *DispatcherPool) GetDispatcherStats() map[string]interface{} {
	stats := dp.GetStats()

	dp.requestsMu.RLock()
	activeRequestsCount := len(dp.activeRequests)
	dp.requestsMu.RUnlock()

	return map[string]interface{}{
		"worker_pool":        stats,
		"active_requests":    activeRequestsCount,
		"requests_processed": dp.requestsProcessed,
		"avg_response_time":  dp.avgResponseTime,
	}
}

// CancelRequest 取消特定请求
func (dp *DispatcherPool) CancelRequest(ContextID string) bool {
	dp.requestsMu.RLock()
	rc, exists := dp.activeRequests[ContextID]
	dp.requestsMu.RUnlock()

	if exists {
		rc.Cancel()
		return true
	}

	return false
}
