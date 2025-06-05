package context

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"sync"
	"time"
	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// RequestState 请求状态枚举
type RequestState string

const (
	StateInitialized        RequestState = "initialized"
	StatePreprocessing      RequestState = "preprocessing"
	StateModelResolution    RequestState = "model_resolution"
	StateRouting            RequestState = "routing"
	StateCacheCheck         RequestState = "cache_check"
	StateRequestRewriting   RequestState = "request_rewriting"
	StateOptimizing         RequestState = "optimizing"
	StateExecuting          RequestState = "executing"
	StateProcessingResponse RequestState = "processing_response"
	StateCompleted          RequestState = "completed"
	StateFailed             RequestState = "failed"
)

// RequestContext 请求上下文结构
type RequestContext struct {
	// 基本标识信息
	ContextID       string       `json:"context_id"`
	RequestID       string       `json:"request_id"`
	CreationTime    time.Time    `json:"creation_time"`
	LastUpdatedTime time.Time    `json:"last_updated_time"`
	State           RequestState `json:"state"`

	// 请求元数据
	Headers     map[string]string      `json:"headers"`
	Endpoint    string                 `json:"endpoint"`
	HTTPMethod  string                 `json:"http_method"`
	QueryParams map[string]string      `json:"query_params"`
	ClientInfo  map[string]interface{} `json:"client_info"`

	// 请求内容
	RawBody    []byte                 `json:"-"`
	ParsedBody map[string]interface{} `json:"parsed_body"`

	// 场景和类型
	Scene              common.InferenceScene `json:"scene"`
	RequestType        common.RequestType    `json:"request_type"`
	RequestIdentifiers map[string]string     `json:"request_identifiers"`

	// 模型信息
	ModelName      string                 `json:"model_name"`
	SamplingParams map[string]interface{} `json:"sampling_parameters"`
	IsStreaming    bool                   `json:"is_streaming"`

	// 路由信息
	BackendURL        string                   `json:"backend_url"`
	BackendInfo       map[string]interface{}   `json:"backend_info"`
	AvailableBackends []map[string]interface{} `json:"available_backends"`

	// 响应信息
	StatusCode      int               `json:"status_code"`
	ResponseStarted bool              `json:"response_started"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseChunks  [][]byte          `json:"response_chunks"`
	FinalResponse   interface{}       `json:"final_response"`
	StreamChannel   chan []byte       `json:"-"`

	// 性能跟踪
	StageTimings    map[RequestState]time.Duration `json:"stage_timings"`
	StageSequence   []RequestState                 `json:"stage_sequence"`
	ProcessingStart time.Time                      `json:"processing_start_time"`
	ProcessingEnd   time.Time                      `json:"processing_end_time"`

	// 错误处理
	Error        error  `json:"-"`
	ErrorMessage string `json:"error_message"`

	// 扩展数据
	Extensions sync.Map `json:"extensions"`

	// 原始上下文
	ginCtx     *gin.Context
	baseCtx    context.Context
	cancelFunc context.CancelFunc

	// 内存池标记
	inPool bool
	mu     sync.RWMutex
}

// requestContextPool RequestContext作为短生命周期对象，使用对象池来复用RequestContext，减少频繁内存分配，降低GC压力
var requestContextPool = sync.Pool{
	New: func() interface{} {
		return &RequestContext{
			Headers:            make(map[string]string),
			QueryParams:        make(map[string]string),
			ClientInfo:         make(map[string]interface{}),
			ParsedBody:         make(map[string]interface{}),
			RequestIdentifiers: make(map[string]string),
			SamplingParams:     make(map[string]interface{}),
			BackendInfo:        make(map[string]interface{}),
			AvailableBackends:  make([]map[string]interface{}, 0),
			ResponseHeaders:    make(map[string]string),
			ResponseChunks:     make([][]byte, 0, 32),
			StageTimings:       make(map[RequestState]time.Duration),
			StageSequence:      make([]RequestState, 0),
		}
	},
}

// AcquireRequestContext 从池中获取RequestContext
func AcquireRequestContext(c *gin.Context, contextManager *ContextManager) *RequestContext {
	// 从池中取出一个RequestContext
	rc := requestContextPool.Get().(*RequestContext)
	rc.reset()

	// 重新赋值
	rc.ContextID = uuid.New().String()
	rc.CreationTime = time.Now()
	rc.LastUpdatedTime = time.Now()
	rc.State = StateInitialized
	rc.initFromGinContext(c, contextManager)
	rc.inPool = false

	// 注册到上下文管理器
	contextManager.RegisterRequestContext(rc)
	return rc
}

// ReleaseRequestContext 释放RequestContext到池中
func ReleaseRequestContext(rc *RequestContext) {
	if rc.inPool {
		return // 防止重复释放
	}

	rc.cleanup()
	rc.inPool = true
	requestContextPool.Put(rc)
}

// reset 重置上下文对象以供复用
func (rc *RequestContext) reset() {
	rc.ContextID = ""
	rc.RequestID = ""
	rc.CreationTime = time.Time{}
	rc.LastUpdatedTime = time.Time{}
	rc.State = StateInitialized

	// 清空map但不重新分配
	for k := range rc.Headers {
		delete(rc.Headers, k)
	}
	for k := range rc.QueryParams {
		delete(rc.QueryParams, k)
	}
	for k := range rc.ClientInfo {
		delete(rc.ClientInfo, k)
	}
	for k := range rc.ParsedBody {
		delete(rc.ParsedBody, k)
	}
	for k := range rc.RequestIdentifiers {
		delete(rc.RequestIdentifiers, k)
	}
	for k := range rc.SamplingParams {
		delete(rc.SamplingParams, k)
	}
	for k := range rc.BackendInfo {
		delete(rc.BackendInfo, k)
	}
	for k := range rc.ResponseHeaders {
		delete(rc.ResponseHeaders, k)
	}
	for k := range rc.StageTimings {
		delete(rc.StageTimings, k)
	}

	// 重置切片
	rc.StageSequence = rc.StageSequence[:0]
	rc.AvailableBackends = rc.AvailableBackends[:0]
	rc.ResponseChunks = rc.ResponseChunks[:0]

	// 清空sync.Map
	rc.Extensions.Range(func(key, value interface{}) bool {
		rc.Extensions.Delete(key)
		return true
	})

	// 重置其他字段
	rc.RawBody = nil
	rc.Endpoint = ""
	rc.HTTPMethod = ""
	rc.Scene = ""
	rc.RequestType = ""
	rc.ModelName = ""
	rc.IsStreaming = false
	rc.BackendURL = ""
	rc.StatusCode = 0
	rc.ResponseStarted = false
	rc.FinalResponse = nil
	rc.ProcessingStart = time.Time{}
	rc.ProcessingEnd = time.Time{}
	rc.Error = nil
	rc.ErrorMessage = ""
}

// cleanup 清理资源
func (rc *RequestContext) cleanup() {
	if rc.cancelFunc != nil {
		rc.cancelFunc()
	}

	// 关闭stream channel
	select {
	case <-rc.StreamChannel:
	default:
		close(rc.StreamChannel)
		rc.StreamChannel = nil
	}
}

// initFromGinContext 从Gin上下文初始化请求上下文
func (rc *RequestContext) initFromGinContext(c *gin.Context, contextManager *ContextManager) {
	// 赋值Gin Context
	rc.ginCtx = c

	// 创建请求级别的context，继承应用context
	// 超时设置为0
	// TODO 后续可能需要支持动态配置RequestContext超时过期的能力
	rc.baseCtx, rc.cancelFunc = contextManager.CreateRequestContext(rc.RequestID, 0)

	// 设置基本信息
	rc.Endpoint = c.Request.URL.Path
	rc.HTTPMethod = c.Request.Method

	// 提取请求头
	for key, values := range c.Request.Header {
		if len(values) > 0 {
			rc.Headers[key] = values[0]
		}
	}

	// 提取查询参数
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			rc.QueryParams[key] = values[0]
		}
	}

	// 提取客户端信息
	rc.ClientInfo["remote_addr"] = c.ClientIP()
	rc.ClientInfo["user_agent"] = c.GetHeader("User-Agent")
	rc.ClientInfo["forwarded_for"] = c.GetHeader("X-Forwarded-For")
	rc.ClientInfo["request_id"] = c.GetHeader("X-Request-ID")
	if c.Request.TLS != nil {
		rc.ClientInfo["tls"] = true
	}

	// 解析请求体
	go rc.parseRequestBody()
}

// parseRequestBody 解析请求体
func (rc *RequestContext) parseRequestBody() {
	if rc.ginCtx.Request.Body == nil {
		return
	}

	body, err := io.ReadAll(rc.ginCtx.Request.Body)
	if err != nil {
		logger.Warnf("failed to read request body: %v", err)
		return
	}
	rc.RawBody = body

	// 尝试解析JSON
	contentType := rc.ginCtx.GetHeader("Content-Type")
	if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
		var parsed map[string]interface{}
		if err = json.Unmarshal(body, &parsed); err != nil {
			logger.Errorf("failed to parse JSON body: %+v", err)
		} else {
			rc.ParsedBody = parsed
			rc.extractModelInfo(parsed)
		}
	}
}

// extractModelInfo 提取模型相关信息
func (rc *RequestContext) extractModelInfo(body map[string]interface{}) {
	if model, ok := body["model"].(string); ok {
		rc.ModelName = model
	}

	if stream, ok := body["stream"].(bool); ok {
		rc.IsStreaming = stream
	}

	// 提取采样参数
	samplingKeys := []string{"temperature", "top_p", "max_tokens", "n",
		"presence_penalty", "frequency_penalty", "top_k", "seed"}

	for _, key := range samplingKeys {
		if value, exists := body[key]; exists {
			rc.SamplingParams[key] = value
		}
	}
}

// StartProcessing 开始处理请求
func (rc *RequestContext) StartProcessing() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	rc.ProcessingStart = now
}

// EndProcessing 结束处理请求
func (rc *RequestContext) EndProcessing() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	rc.ProcessingEnd = now
}

// GetProcessingTime 获取处理时间
func (rc *RequestContext) GetProcessingTime() time.Duration {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	return rc.ProcessingEnd.Sub(rc.ProcessingStart)
}

// SetState 设置请求状态
func (rc *RequestContext) SetState(state RequestState) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	duration := now.Sub(rc.LastUpdatedTime)

	// 记录上一阶段时长
	rc.StageTimings[rc.State] = duration
	rc.StageSequence = append(rc.StageSequence, rc.State)

	// 更新状态
	rc.State = state
	rc.LastUpdatedTime = now

	logger.Debugf("request %s state changed to %s (previous stage took %v)",
		rc.RequestID, state, duration)
}

// Fail 标记请求失败
func (rc *RequestContext) Fail(err error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.Error = err
	rc.ErrorMessage = err.Error()
	rc.SetState(StateFailed)

	logger.Errorf("request %s failed: %s", rc.RequestID, rc.ErrorMessage)
}

// Complete 完成请求处理
func (rc *RequestContext) Complete(response interface{}) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if response != nil {
		rc.FinalResponse = response
	}

	rc.State = StateCompleted
	rc.EndProcessing()

	if duration := rc.GetProcessingTime(); duration > 0 {
		logger.Infof("request %s completed in %v", rc.RequestID, duration)
	}
}

// GetExtension 获取扩展数据
func (rc *RequestContext) GetExtension(key string) (interface{}, bool) {
	return rc.Extensions.Load(key)
}

// SetExtension 设置扩展数据
func (rc *RequestContext) SetExtension(key string, value interface{}) {
	rc.Extensions.Store(key, value)
}

// HasExtension 检查扩展数据是否存在
func (rc *RequestContext) HasExtension(key string) bool {
	_, exists := rc.Extensions.Load(key)
	return exists
}

// AddResponseChunk 添加响应块
func (rc *RequestContext) AddResponseChunk(chunk []byte) {
	rc.ResponseStarted = true

	rc.mu.Lock()
	rc.ResponseChunks = append(rc.ResponseChunks, chunk)
	rc.mu.Unlock()

	// 如果有流式通道，发送数据
	if rc.StreamChannel != nil {
		select {
		case rc.StreamChannel <- chunk:
		default:
			logger.Warnf("stream channel for request %s is full, dropping chunk", rc.RequestID)
		}
	}
}

// GetResponseChunks 获取所有响应块
func (rc *RequestContext) GetResponseChunks() [][]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	chunks := make([][]byte, len(rc.ResponseChunks))
	copy(chunks, rc.ResponseChunks)
	return chunks
}

// Context 获取基础上下文
func (rc *RequestContext) Context() context.Context {
	return rc.baseCtx
}

// Cancel 取消请求
func (rc *RequestContext) Cancel() {
	if rc.cancelFunc != nil {
		rc.cancelFunc()
	}
}

// GetGinContext 获取原始Gin上下文
func (rc *RequestContext) GetGinContext() *gin.Context {
	return rc.ginCtx
}

// ToMap 转换为map用于序列化
func (rc *RequestContext) ToMap() map[string]interface{} {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	result := make(map[string]interface{})

	// 添加基本字段
	result["context_id"] = rc.ContextID
	result["request_id"] = rc.RequestID
	result["creation_time"] = rc.CreationTime
	result["last_updated_time"] = rc.LastUpdatedTime
	result["state"] = rc.State
	result["status_code"] = rc.StatusCode

	// 添加其他可序列化的字段
	result["headers"] = rc.Headers
	result["endpoint"] = rc.Endpoint
	result["http_method"] = rc.HTTPMethod
	result["query_params"] = rc.QueryParams
	result["client_info"] = rc.ClientInfo
	result["parsed_body"] = rc.ParsedBody
	result["model_name"] = rc.ModelName
	result["is_streaming"] = rc.IsStreaming
	result["processing_time"] = rc.GetProcessingTime()

	return result
}
