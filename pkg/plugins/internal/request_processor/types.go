package request_processor

import "time"

const (
	// DefaultTimeout SSE响应超时时间
	DefaultTimeout = 300 * time.Second

	// DefaultBufferSize 默认缓冲区大小
	DefaultBufferSize = 4096

	// SSEContentType SSE Content-Type
	SSEContentType = "text/event-stream"

	// JSONContentType JSON Content-Type
	JSONContentType = "application/json"
)

var (
	// SSEHeaders SSE响应头
	SSEHeaders = map[string]string{
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"Transfer-Encoding": "chunked",
		"X-Accel-Buffering": "no",
	}
)
