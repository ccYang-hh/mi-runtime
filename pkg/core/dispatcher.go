package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/components/pools"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

// RequestDispatcher 高性能请求分发器
type RequestDispatcher struct {
	// 核心组件
	pool           *DispatcherPool
	contextManager *rcx.ContextManager

	// 路由映射: path -> methods -> pipeline
	routeMapping   map[string]map[string]pipelines.IPipeline
	routeMappingRW sync.RWMutex

	// 错误处理器
	errorHandlers   map[int]gin.HandlerFunc
	errorHandlersRW sync.RWMutex

	// 中间件
	middlewares   []gin.HandlerFunc
	middlewaresRW sync.RWMutex

	// 配置
	requestTimeout time.Duration
}

// RegisterRoute 注册路由
func (dispatcher *RequestDispatcher) RegisterRoute(path string, pipeline pipelines.IPipeline, methods []string) error {
	if len(methods) == 0 {
		return fmt.Errorf("no methods specified for route %s", path)
	}

	dispatcher.routeMappingRW.Lock()
	defer dispatcher.routeMappingRW.Unlock()

	if dispatcher.routeMapping[path] == nil {
		dispatcher.routeMapping[path] = make(map[string]pipelines.IPipeline)
	}

	for _, method := range methods {
		if _, exists := dispatcher.routeMapping[path][method]; exists {
			logger.Warnf("route already exists: %s %s", method, path)
		}
		dispatcher.routeMapping[path][method] = pipeline
	}

	logger.Debugf("registered route: %s %v -> pipeline:%s", path, methods, pipeline.GetName())
	return nil
}

// GetHandler 获取Gin处理函数
func (dispatcher *RequestDispatcher) GetHandler() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// 应用中间件
		dispatcher.middlewaresRW.RLock()
		middlewares := make([]gin.HandlerFunc, len(dispatcher.middlewares))
		copy(middlewares, dispatcher.middlewares)
		dispatcher.middlewaresRW.RUnlock()

		// 执行中间件链
		for _, middleware := range middlewares {
			middleware(c)
			if c.IsAborted() {
				return
			}
		}

		// 分发请求
		dispatcher.dispatch(c)
	})
}

// dispatch 分发请求
func (dispatcher *RequestDispatcher) dispatch(c *gin.Context) {
	// 快速路由查找
	path := c.Request.URL.Path
	method := c.Request.Method

	dispatcher.routeMappingRW.RLock()
	pathRoutes, pathExists := dispatcher.routeMapping[path]
	if !pathExists {
		dispatcher.routeMappingRW.RUnlock()
		dispatcher.handleError(c, http.StatusNotFound, "route not found")
		return
	}

	pipeline, methodExists := pathRoutes[method]
	if !methodExists {
		dispatcher.routeMappingRW.RUnlock()
		dispatcher.handleError(c, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dispatcher.routeMappingRW.RUnlock()

	// 创建请求上下文
	reqCtx := rcx.AcquireRequestContext(c, dispatcher.contextManager)

	// 使用专用协程池异步处理请求
	var ctx context.Context
	var cancel context.CancelFunc
	if dispatcher.requestTimeout > 0 {
		ctx, cancel = context.WithTimeout(reqCtx.Context(), dispatcher.requestTimeout)
	} else {
		ctx, cancel = context.WithCancel(reqCtx.Context())
	}
	defer cancel()

	submitted := dispatcher.pool.ProcessRequest(ctx, reqCtx, func(ctx context.Context, reqCtx *rcx.RequestContext) error {
		return dispatcher.processRequest(ctx, c, reqCtx, pipeline)
	})

	if !submitted {
		rcx.ReleaseRequestContext(reqCtx)
		dispatcher.handleError(c, http.StatusServiceUnavailable, "server busy")
		return
	}
}

// processRequest 处理请求
func (dispatcher *RequestDispatcher) processRequest(
	ctx context.Context, c *gin.Context, rc *rcx.RequestContext, pipeline pipelines.IPipeline) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("panic in request processing for %s: %v", rc.ContextID, r)
			dispatcher.handleError(c, http.StatusInternalServerError, "internal server error")
		}
		rcx.ReleaseRequestContext(rc)
	}()

	rc.StartProcessing()
	rc.SetExtension("pipeline_name", pipeline.GetName())

	// 处理请求
	if err := pipeline.Process(ctx, rc); err != nil {
		logger.Errorf("pipeline processing failed for %s: %v", rc.ContextID, err)
		dispatcher.handleError(c, http.StatusInternalServerError, err.Error())
		return err
	}

	// 创建响应
	dispatcher.createResponse(c, rc)
	return nil
}

// createResponse 创建响应
func (dispatcher *RequestDispatcher) createResponse(c *gin.Context, rc *rcx.RequestContext) {
	// 检查请求是否已完成
	if rc.State == rcx.StateFailed {
		statusCode := rc.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}

		c.JSON(statusCode, gin.H{
			"error":      rc.ErrorMessage,
			"context_id": rc.ContextID,
		})
		return
	}

	// 设置响应头
	for key, value := range rc.ResponseHeaders {
		c.Header(key, value)
	}

	// 处理流式响应
	if rc.IsStreaming {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		c.Stream(func(w io.Writer) bool {
			select {
			case data, ok := <-rc.StreamChannel:
				if !ok {
					return false
				}
				_, _ = w.Write(data)
				return true
			case <-rc.Context().Done():
				return false
			}
		})
		return
	}

	// 标准响应
	statusCode := rc.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	if rc.FinalResponse != nil {
		c.JSON(statusCode, rc.FinalResponse)
	} else {
		c.JSON(statusCode, gin.H{
			"status":     "success",
			"context_id": rc.ContextID,
		})
	}
}

// handleError 处理错误
func (dispatcher *RequestDispatcher) handleError(c *gin.Context, statusCode int, message string) {
	dispatcher.errorHandlersRW.RLock()
	handler, exists := dispatcher.errorHandlers[statusCode]
	dispatcher.errorHandlersRW.RUnlock()

	if exists {
		handler(c)
		return
	}

	c.JSON(statusCode, gin.H{
		"error":   http.StatusText(statusCode),
		"message": message,
	})
}

// registerDefaultErrorHandlers 注册默认错误处理器
func (dispatcher *RequestDispatcher) registerDefaultErrorHandlers() {
	dispatcher.errorHandlers[http.StatusNotFound] = func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Not Found",
			"message": "API endpoint not found",
			"path":    c.Request.URL.Path,
		})
	}

	dispatcher.errorHandlers[http.StatusMethodNotAllowed] = func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{
			"error":   "Method Not Allowed",
			"message": "HTTP method not supported for this endpoint",
			"method":  c.Request.Method,
			"path":    c.Request.URL.Path,
		})
	}

	dispatcher.errorHandlers[http.StatusInternalServerError] = func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": "Server encountered an unexpected error",
		})
	}

	dispatcher.errorHandlers[http.StatusServiceUnavailable] = func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Service Unavailable",
			"message": "Server is temporarily overloaded",
		})
	}
}

// RegisterMiddleware 注册中间件
func (dispatcher *RequestDispatcher) RegisterMiddleware(middleware gin.HandlerFunc) {
	dispatcher.middlewaresRW.Lock()
	defer dispatcher.middlewaresRW.Unlock()

	dispatcher.middlewares = append(dispatcher.middlewares, middleware)
}

// RegisterErrorHandler 注册错误处理器
func (dispatcher *RequestDispatcher) RegisterErrorHandler(statusCode int, handler gin.HandlerFunc) {
	dispatcher.errorHandlersRW.Lock()
	defer dispatcher.errorHandlersRW.Unlock()

	dispatcher.errorHandlers[statusCode] = handler
	logger.Debugf("registered error handler for status code: %d", statusCode)
}

// GetStats 获取分发器统计信息
func (dispatcher *RequestDispatcher) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if dispatcher.pool != nil {
		stats["dispatcher_pool"] = dispatcher.pool.GetDispatcherStats()
	}

	dispatcher.routeMappingRW.RLock()
	routeCount := len(dispatcher.routeMapping)
	dispatcher.routeMappingRW.RUnlock()

	stats["routes_count"] = routeCount
	stats["request_timeout"] = dispatcher.requestTimeout

	return stats
}

// NewRequestDispatcher 创建新的请求分发器
func NewRequestDispatcher(
	ctx context.Context, contextManager *rcx.ContextManager,
	poolConfig pools.WorkerPoolConfig, requestTimeout time.Duration,
) *RequestDispatcher {
	dispatcher := &RequestDispatcher{
		pool:           NewDispatcherPool(ctx, poolConfig),
		contextManager: contextManager,
		routeMapping:   make(map[string]map[string]pipelines.IPipeline),
		errorHandlers:  make(map[int]gin.HandlerFunc),
		middlewares:    make([]gin.HandlerFunc, 0),
		requestTimeout: requestTimeout,
	}

	// 注册默认错误处理器
	dispatcher.registerDefaultErrorHandlers()

	return dispatcher
}
