
```go
// pkg/core/types.go
package core

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// RouteSource 路由源类型
type RouteSource string

const (
	RouteSourceSystem   RouteSource = "system"
	RouteSourcePlugin   RouteSource = "plugin"
	RouteSourcePipeline RouteSource = "pipeline"
)

// RequestState 请求状态
type RequestState string

const (
	StateInitialized        RequestState = "initialized"
	StatePreprocessing      RequestState = "preprocessing"
	StateModelResolution    RequestState = "model_resolution"
	StateRouting           RequestState = "routing"
	StateCacheCheck        RequestState = "cache_check"
	StateRequestRewriting  RequestState = "request_rewriting"
	StateOptimizing        RequestState = "optimizing"
	StateExecuting         RequestState = "executing"
	StateProcessingResponse RequestState = "processing_response"
	StateCompleted         RequestState = "completed"
	StateFailed            RequestState = "failed"
)

// InferenceScene 推理场景
type InferenceScene string

// RequestType 请求类型
type RequestType string

// Handler 请求处理器接口
type Handler interface {
	Handle(ctx context.Context, request *RequestContext) error
}

// Pipeline 管道接口（占位符）
type Pipeline interface {
	Name() string
	Routes() []*Route
	Process(ctx context.Context, request *RequestContext) error
}

// Route 路由定义
type Route struct {
	Path    string
	Method  string
	Handler Handler
}

// Router 路由器接口
type Router interface {
	AddRoute(route *Route) error
	Match(method, path string) (Handler, bool)
	Routes() []*Route
}

// pkg/core/context.go
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// RequestContext 请求上下文结构
type RequestContext struct {
	// 基本标识信息
	RequestID     string    `json:"request_id"`
	CreationTime  time.Time `json:"creation_time"`
	LastUpdated   time.Time `json:"last_updated"`
	state         RequestState
	stateMu       sync.RWMutex

	// 请求元数据
	Headers      map[string]string `json:"headers"`
	Endpoint     string           `json:"endpoint"`
	HTTPMethod   string           `json:"http_method"`
	QueryParams  map[string]string `json:"query_params"`
	ClientInfo   map[string]interface{} `json:"client_info"`

	// 请求内容
	RawBody    []byte                 `json:"-"`
	ParsedBody map[string]interface{} `json:"parsed_body"`

	// 场景和类型
	Scene               InferenceScene `json:"scene"`
	RequestType         RequestType    `json:"request_type"`
	RequestIdentifiers  map[string]string `json:"request_identifiers"`

	// 模型信息
	ModelName          string                 `json:"model_name"`
	SamplingParameters map[string]interface{} `json:"sampling_parameters"`
	IsStreaming        bool                   `json:"is_streaming"`

	// 路由信息
	BackendURL        string                   `json:"backend_url"`
	BackendInfo       map[string]interface{}   `json:"backend_info"`
	AvailableBackends []map[string]interface{} `json:"available_backends"`

	// 缓存信息
	CacheKey      string                 `json:"cache_key"`
	CacheHit      bool                   `json:"cache_hit"`
	CacheMetadata map[string]interface{} `json:"cache_metadata"`

	// 响应信息
	StatusCode      int               `json:"status_code"`
	ResponseStarted int32             `json:"response_started"` // 使用atomic
	ResponseHeaders map[string]string `json:"response_headers"`
	responseChunks  [][]byte          // 响应数据块
	chunksMu        sync.Mutex        // 保护响应数据块
	FinalResponse   interface{}       `json:"final_response"`

	// 性能跟踪
	StageTimings       map[string]time.Duration `json:"stage_timings"`
	StageSequence      []string                 `json:"stage_sequence"`
	ProcessingStart    time.Time                `json:"processing_start"`
	ProcessingEnd      time.Time                `json:"processing_end"`
	Metrics           map[string]interface{}    `json:"metrics"`

	// 错误处理
	Error        error  `json:"-"`
	ErrorMessage string `json:"error_message"`
	StackTrace   string `json:"stack_trace"`

	// 扩展数据
	extensions map[string]interface{}
	extMu      sync.RWMutex

	// 流式响应通道
	StreamChan chan []byte `json:"-"`

	// 原始HTTP请求和响应写入器（用于直接操作）
	httpRequest *http.Request       `json:"-"`
	httpWriter  http.ResponseWriter `json:"-"`
}

// contextPool 使用对象池来复用RequestContext
var contextPool = sync.Pool{
	New: func() interface{} {
		return &RequestContext{
			Headers:            make(map[string]string),
			QueryParams:        make(map[string]string),
			ClientInfo:         make(map[string]interface{}),
			ParsedBody:         make(map[string]interface{}),
			RequestIdentifiers: make(map[string]string),
			SamplingParameters: make(map[string]interface{}),
			BackendInfo:        make(map[string]interface{}),
			CacheMetadata:      make(map[string]interface{}),
			ResponseHeaders:    make(map[string]string),
			StageTimings:       make(map[string]time.Duration),
			StageSequence:      make([]string, 0, 16),
			Metrics:           make(map[string]interface{}),
			extensions:        make(map[string]interface{}),
			responseChunks:    make([][]byte, 0, 32),
		}
	},
}

// NewRequestContext 创建新的请求上下文
func NewRequestContext() *RequestContext {
	ctx := contextPool.Get().(*RequestContext)
	ctx.reset()
	ctx.RequestID = uuid.New().String()
	ctx.CreationTime = time.Now()
	ctx.LastUpdated = time.Now()
	ctx.state = StateInitialized
	return ctx
}

// FromHTTPRequest 从HTTP请求创建请求上下文
func FromHTTPRequest(r *http.Request, w http.ResponseWriter) *RequestContext {
	ctx := NewRequestContext()
	ctx.httpRequest = r
	ctx.httpWriter = w
	
	// 解析请求信息
	ctx.Endpoint = r.URL.Path
	ctx.HTTPMethod = r.Method
	
	// 解析headers
	for k, v := range r.Header {
		if len(v) > 0 {
			ctx.Headers[k] = v[0]
		}
	}
	
	// 解析查询参数
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			ctx.QueryParams[k] = v[0]
		}
	}
	
	// 解析客户端信息
	if r.RemoteAddr != "" {
		ctx.ClientInfo["remote_addr"] = r.RemoteAddr
	}
	if userAgent := r.Header.Get("User-Agent"); userAgent != "" {
		ctx.ClientInfo["user_agent"] = userAgent
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ctx.ClientInfo["forwarded_for"] = forwarded
	}
	
	// 异步解析请求体
	go ctx.parseRequestBody(r)
	
	return ctx
}

// reset 重置上下文对象以供复用
func (c *RequestContext) reset() {
	c.RequestID = ""
	c.CreationTime = time.Time{}
	c.LastUpdated = time.Time{}
	c.state = StateInitialized
	
	// 清空map但不重新分配
	for k := range c.Headers {
		delete(c.Headers, k)
	}
	for k := range c.QueryParams {
		delete(c.QueryParams, k)
	}
	for k := range c.ClientInfo {
		delete(c.ClientInfo, k)
	}
	for k := range c.ParsedBody {
		delete(c.ParsedBody, k)
	}
	for k := range c.RequestIdentifiers {
		delete(c.RequestIdentifiers, k)
	}
	for k := range c.SamplingParameters {
		delete(c.SamplingParameters, k)
	}
	for k := range c.BackendInfo {
		delete(c.BackendInfo, k)
	}
	for k := range c.CacheMetadata {
		delete(c.CacheMetadata, k)
	}
	for k := range c.ResponseHeaders {
		delete(c.ResponseHeaders, k)
	}
	for k := range c.StageTimings {
		delete(c.StageTimings, k)
	}
	for k := range c.Metrics {
		delete(c.Metrics, k)
	}
	for k := range c.extensions {
		delete(c.extensions, k)
	}
	
	// 重置切片
	c.StageSequence = c.StageSequence[:0]
	c.AvailableBackends = c.AvailableBackends[:0]
	c.responseChunks = c.responseChunks[:0]
	
	// 重置其他字段
	c.RawBody = nil
	c.Endpoint = ""
	c.HTTPMethod = ""
	c.Scene = ""
	c.RequestType = ""
	c.ModelName = ""
	c.IsStreaming = false
	c.BackendURL = ""
	c.CacheKey = ""
	c.CacheHit = false
	c.StatusCode = 0
	atomic.StoreInt32(&c.ResponseStarted, 0)
	c.FinalResponse = nil
	c.ProcessingStart = time.Time{}
	c.ProcessingEnd = time.Time{}
	c.Error = nil
	c.ErrorMessage = ""
	c.StackTrace = ""
	
	// 关闭并重置通道
	if c.StreamChan != nil {
		close(c.StreamChan)
		c.StreamChan = nil
	}
	
	c.httpRequest = nil
	c.httpWriter = nil
}

// Release 释放上下文对象回池中
func (c *RequestContext) Release() {
	contextPool.Put(c)
}

// parseRequestBody 异步解析请求体
func (c *RequestContext) parseRequestBody(r *http.Request) {
	if r.Body == nil {
		return
	}
	
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	c.RawBody = buf.Bytes()
	
	// 尝试解析JSON
	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
		var parsedBody map[string]interface{}
		if err := json.Unmarshal(c.RawBody, &parsedBody); err == nil {
			c.ParsedBody = parsedBody
			
			// 提取常见参数
			if model, ok := parsedBody["model"].(string); ok {
				c.ModelName = model
			}
			if stream, ok := parsedBody["stream"].(bool); ok {
				c.IsStreaming = stream
			}
			
			// 提取采样参数
			samplingParams := []string{"temperature", "top_p", "max_tokens", "n", 
				"presence_penalty", "frequency_penalty", "top_k", "seed"}
			for _, param := range samplingParams {
				if value, exists := parsedBody[param]; exists {
					c.SamplingParameters[param] = value
				}
			}
		} else {
			logger.Warnf("failed to parse JSON body: %v", err)
		}
	}
}

// State 获取当前状态
func (c *RequestContext) State() RequestState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

// SetState 设置请求状态
func (c *RequestContext) SetState(state RequestState) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	
	now := time.Now()
	duration := now.Sub(c.LastUpdated)
	
	// 记录阶段时长
	c.StageTimings[string(c.state)] = duration
	c.StageSequence = append(c.StageSequence, string(c.state))
	
	// 更新状态
	c.state = state
	c.LastUpdated = now
	
	logger.Debugf("request %s state changed to %s (previous stage took %v)",
		c.RequestID, state, duration)
}

// StartProcessing 开始处理
func (c *RequestContext) StartProcessing() {
	c.ProcessingStart = time.Now()
}

// EndProcessing 结束处理
func (c *RequestContext) EndProcessing() {
	c.ProcessingEnd = time.Now()
}

// GetProcessingTime 获取处理时间
func (c *RequestContext) GetProcessingTime() time.Duration {
	if !c.ProcessingStart.IsZero() && !c.ProcessingEnd.IsZero() {
		return c.ProcessingEnd.Sub(c.ProcessingStart)
	}
	return 0
}

// Fail 标记为失败
func (c *RequestContext) Fail(err error) {
	c.Error = err
	c.ErrorMessage = err.Error()
	c.SetState(StateFailed)
	logger.Errorf("request %s failed: %s", c.RequestID, err.Error())
}

// Complete 标记为完成
func (c *RequestContext) Complete(response interface{}) {
	if response != nil {
		c.FinalResponse = response
	}
	c.SetState(StateCompleted)
	c.EndProcessing()
	
	if duration := c.GetProcessingTime(); duration > 0 {
		logger.Infof("request %s completed in %v", c.RequestID, duration)
	}
}

// AddResponseChunk 添加响应块
func (c *RequestContext) AddResponseChunk(chunk []byte) {
	if atomic.CompareAndSwapInt32(&c.ResponseStarted, 0, 1) {
		logger.Debugf("request %s first response chunk received", c.RequestID)
	}
	
	c.chunksMu.Lock()
	c.responseChunks = append(c.responseChunks, chunk)
	c.chunksMu.Unlock()
	
	// 如果有流式通道，发送数据
	if c.StreamChan != nil {
		select {
		case c.StreamChan <- chunk:
		default:
			logger.Warnf("stream channel for request %s is full, dropping chunk", c.RequestID)
		}
	}
}

// GetResponseChunks 获取所有响应块
func (c *RequestContext) GetResponseChunks() [][]byte {
	c.chunksMu.Lock()
	defer c.chunksMu.Unlock()
	
	chunks := make([][]byte, len(c.responseChunks))
	copy(chunks, c.responseChunks)
	return chunks
}

// SetExtension 设置扩展数据
func (c *RequestContext) SetExtension(key string, value interface{}) {
	c.extMu.Lock()
	c.extensions[key] = value
	c.extMu.Unlock()
}

// GetExtension 获取扩展数据
func (c *RequestContext) GetExtension(key string) (interface{}, bool) {
	c.extMu.RLock()
	defer c.extMu.RUnlock()
	value, exists := c.extensions[key]
	return value, exists
}

// HasExtension 检查扩展数据是否存在
func (c *RequestContext) HasExtension(key string) bool {
	c.extMu.RLock()
	defer c.extMu.RUnlock()
	_, exists := c.extensions[key]
	return exists
}

// EnableStreaming 启用流式响应
func (c *RequestContext) EnableStreaming(bufferSize int) {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	c.StreamChan = make(chan []byte, bufferSize)
	c.IsStreaming = true
}

// String 字符串表示
func (c *RequestContext) String() string {
	return fmt.Sprintf("RequestContext(id=%s, model=%s, state=%s)", 
		c.RequestID, c.ModelName, c.State())
}

// pkg/core/dispatcher.go
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// RequestDispatcher 请求分发器
type RequestDispatcher struct {
	routeMapping   map[string]Pipeline        // 路由到Pipeline的映射
	errorHandlers  map[int]ErrorHandler       // 错误处理器
	mu             sync.RWMutex               // 读写锁保护映射
}

// ErrorHandler 错误处理器函数类型
type ErrorHandler func(ctx *RequestContext, err error) error

// dispatcherInstance 单例实例
var (
	dispatcherInstance *RequestDispatcher
	dispatcherOnce     sync.Once
)

// GetDispatcher 获取分发器单例
func GetDispatcher() *RequestDispatcher {
	dispatcherOnce.Do(func() {
		dispatcherInstance = &RequestDispatcher{
			routeMapping:  make(map[string]Pipeline),
			errorHandlers: make(map[int]ErrorHandler),
		}
		
		// 注册默认错误处理器
		dispatcherInstance.RegisterErrorHandler(404, defaultNotFoundHandler)
		dispatcherInstance.RegisterErrorHandler(500, defaultServerErrorHandler)
	})
	return dispatcherInstance
}

// RegisterRoute 注册路由
func (d *RequestDispatcher) RegisterRoute(path string, pipeline Pipeline, methods []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	d.routeMapping[path] = pipeline
	methodsStr := "ALL"
	if len(methods) > 0 {
		methodsStr = fmt.Sprintf("%v", methods)
	}
	
	logger.Infof("registered route: %s [%s] -> pipeline:%s", path, methodsStr, pipeline.Name())
}

// RegisterErrorHandler 注册错误处理器
func (d *RequestDispatcher) RegisterErrorHandler(statusCode int, handler ErrorHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	d.errorHandlers[statusCode] = handler
	logger.Debugf("registered error handler for status code: %d", statusCode)
}

// Dispatch 分发请求
func (d *RequestDispatcher) Dispatch(ctx context.Context, request *RequestContext) error {
	// 查找对应的Pipeline
	d.mu.RLock()
	pipeline, exists := d.routeMapping[request.Endpoint]
	d.mu.RUnlock()
	
	if !exists {
		logger.Warnf("no route mapping found for path: %s", request.Endpoint)
		return d.handleError(request, 404, fmt.Errorf("API not found: %s", request.Endpoint))
	}
	
	// 添加Pipeline信息到扩展数据
	request.SetExtension("pipeline_name", pipeline.Name())
	
	// 处理请求
	logger.Debugf("dispatching request to pipeline: %s", pipeline.Name())
	
	if err := pipeline.Process(ctx, request); err != nil {
		logger.Errorf("pipeline processing failed: %v", err)
		return d.handleError(request, 500, err)
	}
	
	return nil
}

// handleError 处理错误
func (d *RequestDispatcher) handleError(request *RequestContext, statusCode int, err error) error {
	request.Fail(err)
	request.SetExtension("status_code", statusCode)
	
	// 使用注册的错误处理器
	d.mu.RLock()
	handler, exists := d.errorHandlers[statusCode]
	d.mu.RUnlock()
	
	if exists {
		return handler(request, err)
	}
	
	// 默认错误处理
	return d.createErrorResponse(request, statusCode, err)
}

// createErrorResponse 创建错误响应
func (d *RequestDispatcher) createErrorResponse(request *RequestContext, statusCode int, err error) error {
	errorResp := map[string]interface{}{
		"error":      "Internal Server Error",
		"message":    err.Error(),
		"request_id": request.RequestID,
	}
	
	if statusCode == 404 {
		errorResp["error"] = "Not Found"
		errorResp["path"] = request.Endpoint
	}
	
	request.StatusCode = statusCode
	request.FinalResponse = errorResp
	
	return nil
}

// defaultNotFoundHandler 默认404处理器
func defaultNotFoundHandler(ctx *RequestContext, err error) error {
	response := map[string]interface{}{
		"error":      "Not Found",
		"message":    err.Error(),
		"request_id": ctx.RequestID,
		"path":       ctx.Endpoint,
	}
	
	ctx.StatusCode = 404
	ctx.FinalResponse = response
	return nil
}

// defaultServerErrorHandler 默认500处理器
func defaultServerErrorHandler(ctx *RequestContext, err error) error {
	response := map[string]interface{}{
		"error":      "Internal Server Error",
		"message":    err.Error(),
		"request_id": ctx.RequestID,
	}
	
	ctx.StatusCode = 500
	ctx.FinalResponse = response
	return nil
}

// CreateHTTPHandler 创建HTTP处理器
func (d *RequestDispatcher) CreateHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 创建请求上下文
		requestCtx := FromHTTPRequest(r, w)
		defer requestCtx.Release()
		
		// 开始处理
		requestCtx.StartProcessing()
		requestCtx.SetState(StateInitialized)
		
		// 分发请求
		ctx := context.Background()
		if err := d.Dispatch(ctx, requestCtx); err != nil {
			logger.Errorf("dispatch failed: %v", err)
		}
		
		// 写入响应
		d.writeResponse(w, requestCtx)
	}
}

// writeResponse 写入HTTP响应
func (d *RequestDispatcher) writeResponse(w http.ResponseWriter, ctx *RequestContext) {
	// 设置响应头
	for k, v := range ctx.ResponseHeaders {
		w.Header().Set(k, v)
	}
	
	// 处理错误响应
	if ctx.State() == StateFailed {
		statusCode := 500
		if code, exists := ctx.GetExtension("status_code"); exists {
			if sc, ok := code.(int); ok {
				statusCode = sc
			}
		}
		w.WriteHeader(statusCode)
		
		if ctx.FinalResponse != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ctx.FinalResponse)
		}
		return
	}
	
	// 处理流式响应
	if ctx.IsStreaming && ctx.StreamChan != nil {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		
		for chunk := range ctx.StreamChan {
			w.Write(chunk)
			flusher.Flush()
		}
		return
	}
	
	// 标准响应
	if ctx.StatusCode > 0 {
		w.WriteHeader(ctx.StatusCode)
	}
	
	if ctx.FinalResponse != nil {
		switch resp := ctx.FinalResponse.(type) {
		case map[string]interface{}, []interface{}:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case string:
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(resp))
		case []byte:
			w.Write(resp)
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	} else {
		// 默认成功响应
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "success",
			"request_id": ctx.RequestID,
		})
	}
}

// pkg/core/router_manager.go
package core

import (
	"net/http"
	"sync"
	"sync/atomic"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// RouterManager 路由管理器
type RouterManager struct {
	dispatcher *RequestDispatcher
	
	// 系统路由器集合
	systemRoutesCount int64
	systemRouters     map[string]Router
	systemMu          sync.RWMutex
	
	// 插件路由器集合
	pluginRoutesCount int64
	pluginRouters     map[string]Router
	pluginMu          sync.RWMutex
	
	// 已注册的路由
	registeredRoutes map[string]map[string]string // path -> method -> pipeline_name
	routeSources     map[string]map[string]RouteSource // path -> method -> source
	routeMu          sync.RWMutex
}

// managerInstance 单例实例
var (
	managerInstance *RouterManager
	managerOnce     sync.Once
)

// GetRouterManager 获取路由管理器单例
func GetRouterManager() *RouterManager {
	managerOnce.Do(func() {
		managerInstance = &RouterManager{
			dispatcher:       GetDispatcher(),
			systemRouters:    make(map[string]Router),
			pluginRouters:    make(map[string]Router),
			registeredRoutes: make(map[string]map[string]string),
			routeSources:     make(map[string]map[string]RouteSource),
		}
	})
	return managerInstance
}

// RegisterSystemRouter 注册系统级路由器
func (rm *RouterManager) RegisterSystemRouter(moduleName string, router Router, prefix string) {
	if router == nil {
		return
	}
	
	rm.systemMu.Lock()
	defer rm.systemMu.Unlock()
	
	// 检查重复注册
	if _, exists := rm.systemRouters[moduleName]; exists {
		logger.Infof("system router already registered: %s", moduleName)
		return
	}
	
	rm.systemRouters[moduleName] = router
	routes := router.Routes()
	atomic.AddInt64(&rm.systemRoutesCount, int64(len(routes)))
	
	for _, route := range routes {
		logger.Infof("registered route: %s [%s] -> %s", route.Path, route.Method, moduleName)
	}
}

// RegisterPipelineRoutes 注册Pipeline路由
func (rm *RouterManager) RegisterPipelineRoutes(pipeline Pipeline) {
	if pipeline == nil {
		return
	}
	
	routes := pipeline.Routes()
	if len(routes) == 0 {
		logger.Debugf("pipeline has no routes defined: %s", pipeline.Name())
		return
	}
	
	rm.routeMu.Lock()
	defer rm.routeMu.Unlock()
	
	for _, route := range routes {
		// 注册到分发器
		rm.dispatcher.RegisterRoute(route.Path, pipeline, []string{route.Method})
		
		// 记录注册信息
		if rm.registeredRoutes[route.Path] == nil {
			rm.registeredRoutes[route.Path] = make(map[string]string)
		}
		rm.registeredRoutes[route.Path][route.Method] = pipeline.Name()
		
		if rm.routeSources[route.Path] == nil {
			rm.routeSources[route.Path] = make(map[string]RouteSource)
		}
		rm.routeSources[route.Path][route.Method] = RouteSourcePipeline
	}
}

// RegisterPluginRouter 注册插件路由器
func (rm *RouterManager) RegisterPluginRouter(pluginName string, router Router) {
	if router == nil {
		return
	}
	
	rm.pluginMu.Lock()
	defer rm.pluginMu.Unlock()
	
	// 检查重复注册
	if _, exists := rm.pluginRouters[pluginName]; exists {
		logger.Infof("plugin router already registered: %s", pluginName)
		return
	}
	
	rm.pluginRouters[pluginName] = router
	routes := router.Routes()
	atomic.AddInt64(&rm.pluginRoutesCount, int64(len(routes)))
	
	for _, route := range routes {
		logger.Infof("registered route: %s [%s] -> %s", route.Path, route.Method, pluginName)
	}
}

// CreateHTTPHandler 创建HTTP处理器
func (rm *RouterManager) CreateHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 优先级处理：系统路由 > 插件路由 > Pipeline路由
		
		// 1. 检查系统路由
		if handler := rm.matchSystemRoute(r.Method, r.URL.Path); handler != nil {
			ctx := FromHTTPRequest(r, w)
			defer ctx.Release()
			if err := handler.Handle(r.Context(), ctx); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		
		// 2. 检查插件路