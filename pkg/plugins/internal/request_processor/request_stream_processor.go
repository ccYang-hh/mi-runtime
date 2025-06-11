package request_processor

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

// StreamProcessorStage 流处理阶段 - 职责单一：发起请求，根据场景处理响应
type StreamProcessorStage struct {
	*pipelines.BaseStage
	httpClient  *http.Client
	bufferSize  int
	readTimeout time.Duration
}

// NewStreamProcessorStage 创建流处理阶段
func NewStreamProcessorStage(name string) *StreamProcessorStage {
	return &StreamProcessorStage{
		BaseStage: pipelines.NewBaseStage(name),
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		bufferSize:  4096,
		readTimeout: 30 * time.Second,
	}
}

// Process 处理请求 - 核心逻辑：发起HTTP请求，根据场景分支处理
func (s *StreamProcessorStage) Process(ctx *rcx.RequestContext) error {
	if ctx.BackendURL == "" {
		logger.Errorf("Backend URL is empty")
		return fmt.Errorf("no backend URL specified")
	}

	ctx.SetState(rcx.StateProcessingResponse)

	// 创建HTTP请求
	req, err := s.createHTTPRequest(ctx)
	if err != nil {
		logger.Errorf("Failed to create HTTP request: %v", err)
		return fmt.Errorf("create http request err: %w", err)
	}

	// 发起请求
	resp, err := s.httpClient.Do(req)
	if err != nil {
		logger.Errorf("Failed to send HTTP request: %v", err)
		return fmt.Errorf("http request err: %w", err)
	}

	// 设置响应头和状态码
	s.setResponseHeaders(ctx, resp)

	// 根据场景分支处理
	if ctx.IsStreaming {
		return s.handleStreamingResponse(ctx, resp)
	}

	return s.handleNormalResponse(ctx, resp)
}

// createHTTPRequest 创建HTTP请求
func (s *StreamProcessorStage) createHTTPRequest(ctx *rcx.RequestContext) (*http.Request, error) {
	method := ctx.HTTPMethod
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequestWithContext(ctx.Context(), method, ctx.BackendURL, bytes.NewReader(ctx.RawBody))
	if err != nil {
		logger.Errorf("Failed to create HTTP request: %+v", err)
		return nil, err
	}

	// 复制请求头
	for key, value := range ctx.Headers {
		req.Header.Add(key, value)
	}

	return req, nil
}

// setResponseHeaders 设置响应头到上下文
func (s *StreamProcessorStage) setResponseHeaders(ctx *rcx.RequestContext, resp *http.Response) {
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0] // 简化处理，取第一个值
		}
	}

	// 流式响应移除Content-Length
	if ctx.IsStreaming {
		delete(headers, "Content-Length")
		headers["Cache-Control"] = "no-cache"
		headers["Connection"] = "keep-alive"
		headers["Content-Type"] = "text/event-stream"
	}

	ctx.ResponseHeaders = headers
	ctx.StatusCode = resp.StatusCode
}

// handleNormalResponse 处理普通响应 - 读取完整响应存入上下文
func (s *StreamProcessorStage) handleNormalResponse(ctx *rcx.RequestContext, resp *http.Response) error {
	defer resp.Body.Close()

	// 读取完整响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Errorf("failed to read response body: %+v", err)
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// 记录首个字节时间
	ctx.SetExtension("first_token_time", time.Now())

	// 存入上下文
	ctx.FinalResponse = body
	ctx.SetExtension("response_size", len(body))

	return nil
}

// handleStreamingResponse 处理流式响应 - 创建channel并启动goroutine
func (s *StreamProcessorStage) handleStreamingResponse(ctx *rcx.RequestContext, resp *http.Response) error {
	// 创建流数据channel
	streamChan := make(chan []byte, 50) // 缓冲channel避免阻塞
	ctx.StreamChannel = streamChan

	// 启动goroutine处理流数据
	go s.streamReader(ctx, resp, streamChan)

	return nil
}

// streamReader 流数据读取器
func (s *StreamProcessorStage) streamReader(ctx *rcx.RequestContext, resp *http.Response, streamChan chan<- []byte) {
	defer func() {
		resp.Body.Close()
		close(streamChan)
		if r := recover(); r != nil {
			logger.Errorf("Recovered in streamReader: %+v", r)
		}
	}()

	reader := bufio.NewReader(resp.Body)
	buffer := make([]byte, s.bufferSize)
	firstChunk := true

	for {
		select {
		case <-ctx.Context().Done():
			logger.Debugf("Context Cone，Stop Request Stream Processor")
			return
		default:
		}

		// 设置读取超时
		if netConn, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = netConn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		n, err := reader.Read(buffer)
		if n > 0 {
			// 记录首个chunk时间
			if firstChunk {
				firstChunk = false
				ctx.SetExtension("first_token_time", time.Now())
			}

			// 创建数据副本并发送到channel
			chunk := make([]byte, n)
			copy(chunk, buffer[:n])

			select {
			case streamChan <- chunk:
				// 成功发送
			case <-ctx.Context().Done():
				return
			case <-time.After(5 * time.Second):
				logger.Warnf("send stream chunk timeout")
				return
			}
		}

		if err != nil {
			if err == io.EOF {
				logger.Debugf("read stream chunk EOF")
			} else {
				logger.Errorf("read stream err: %+v", err)
			}
			return
		}
	}
}

// Shutdown 关闭阶段
func (s *StreamProcessorStage) Shutdown() error {
	if s.httpClient != nil {
		s.httpClient.CloseIdleConnections()
	}
	return s.BaseStage.Shutdown()
}
