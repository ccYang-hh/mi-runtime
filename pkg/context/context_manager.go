package context

import (
	"context"
	"sync"
	"time"
	
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// ContextManager 上下文管理器 - 管理全局和请求级别的context
type ContextManager struct {
	// 应用级别的根context
	appCtx    context.Context
	appCancel context.CancelFunc

	// 请求上下文映射
	requestContexts sync.Map // requestID -> *RequestContext

	// 统计信息
	activeRequests int64
	totalRequests  int64
	mu             sync.RWMutex
}

// NewContextManager 创建上下文管理器
func NewContextManager() *ContextManager {
	appCtx, appCancel := context.WithCancel(context.Background())

	cm := &ContextManager{
		appCtx:    appCtx,
		appCancel: appCancel,
	}

	// 启动清理协程
	go cm.startCleanupRoutine()

	return cm
}

// GetRootContext 获取根Context
func (cm *ContextManager) GetRootContext() (context.Context, context.CancelFunc) {
	return cm.appCtx, cm.appCancel
}

// CreateRequestContext 创建请求上下文
func (cm *ContextManager) CreateRequestContext(contextID string, timeout time.Duration) (context.Context, context.CancelFunc) {
	// 创建带超时的请求上下文，继承应用上下文
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(cm.appCtx, timeout)
	} else {
		ctx, cancel = context.WithCancel(cm.appCtx)
	}

	// 包装cancel函数，添加清理逻辑
	wrappedCancel := func() {
		cancel()
		cm.requestContexts.Delete(contextID)
	}

	return ctx, wrappedCancel
}

// RegisterRequestContext 注册请求上下文
func (cm *ContextManager) RegisterRequestContext(rc *RequestContext) {
	cm.requestContexts.Store(rc.ContextID, rc)
}

// GetRequestContext 获取请求上下文
func (cm *ContextManager) GetRequestContext(contextID string) (*RequestContext, bool) {
	if val, ok := cm.requestContexts.Load(contextID); ok {
		return val.(*RequestContext), true
	}
	return nil, false
}

// CancelAllRequests 取消所有请求
func (cm *ContextManager) CancelAllRequests() {
	logger.Infof("canceling all active requests")

	cm.requestContexts.Range(func(key, value interface{}) bool {
		if rc, ok := value.(*RequestContext); ok {
			rc.Cancel()
		}
		return true
	})
}

// Shutdown 优雅关闭
func (cm *ContextManager) Shutdown() {
	logger.Infof("shutting down context manager")

	// 取消所有请求
	cm.CancelAllRequests()

	// 取消应用上下文
	cm.appCancel()
}

// startCleanupRoutine 启动清理协程
func (cm *ContextManager) startCleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.cleanupExpiredContexts()
		case <-cm.appCtx.Done():
			return
		}
	}
}

// cleanupExpiredContexts 清理过期的上下文
func (cm *ContextManager) cleanupExpiredContexts() {
	var expiredCount int

	cm.requestContexts.Range(func(key, value interface{}) bool {
		if rc, ok := value.(*RequestContext); ok {
			// 检查context是否已过期
			select {
			case <-rc.Context().Done():
				cm.requestContexts.Delete(key)
				expiredCount++
			default:
				// context仍然活跃
			}
		}
		return true
	})

	if expiredCount > 0 {
		logger.Debugf("cleaned up %d expired request contexts", expiredCount)
	}
}

// GetStats 获取统计信息
func (cm *ContextManager) GetStats() map[string]interface{} {
	activeCount := 0
	cm.requestContexts.Range(func(key, value interface{}) bool {
		activeCount++
		return true
	})

	return map[string]interface{}{
		"active_requests": activeCount,
	}
}
