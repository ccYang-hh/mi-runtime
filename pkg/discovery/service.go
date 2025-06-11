package discovery

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

var (
	// 全局单例实例
	globalEndpointService *EndpointService
	// 单例初始化锁
	singletonOnce sync.Once
)

// GetEndpointService 获取全局单例实例（懒加载）
func GetEndpointService(serviceDiscovery ServiceDiscovery) *EndpointService {
	singletonOnce.Do(func() {
		globalEndpointService = newEndpointService(serviceDiscovery)
	})
	return globalEndpointService
}

// GetEndpointServiceDirectly ...
func GetEndpointServiceDirectly() *EndpointService {
	return globalEndpointService
}

// EndpointService 端点API处理器
type EndpointService struct {
	discovery    ServiceDiscovery // 服务发现实例
	executor     chan func()      // 同步操作执行器
	once         sync.Once        // 确保执行器只启动一次
	timeout      time.Duration    // 操作超时时间
	maxQueueSize int              // 最大队列大小
}

// newEndpointService 创建端点处理器
func newEndpointService(discovery ServiceDiscovery) *EndpointService {
	h := &EndpointService{
		discovery:    discovery,
		executor:     make(chan func(), 200), // 增加缓冲区大小
		timeout:      30 * time.Second,
		maxQueueSize: 200,
	}

	// 启动同步操作执行器
	h.once.Do(func() {
		go h.runExecutor()
	})

	return h
}

// runExecutor 运行同步操作执行器
func (h *EndpointService) runExecutor() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("endpoint handler executor panicked: %v", r)
		}
	}()

	for fn := range h.executor {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("operation panicked in executor: %v", r)
				}
			}()
			fn()
		}()
	}
}

// runSync 在专用goroutine中执行同步操作
func (h *EndpointService) runSync(fn func() (interface{}, error)) (interface{}, error) {
	resultCh := make(chan interface{}, 1)
	errorCh := make(chan error, 1)

	// 检查队列是否满
	select {
	case h.executor <- func() {
		result, err := fn()
		if err != nil {
			errorCh <- err
		} else {
			resultCh <- result
		}
	}:
		// 成功提交任务
	default:
		return nil, fmt.Errorf("operation queue is full")
	}

	// 等待结果
	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errorCh:
		return nil, err
	case <-time.After(h.timeout):
		return nil, fmt.Errorf("operation timeout after %v", h.timeout)
	}
}

// CreateEndpoint 创建端点
func (h *EndpointService) CreateEndpoint(c *gin.Context) {
	var req CreateEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf("failed to bind create endpoint request: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: fmt.Sprintf("Invalid request format: %v", err),
			Code:    400001,
		})
		return
	}

	// 确保有Endpoint实例
	if req.Endpoint == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "missing_endpoint",
			Message: "Endpoint data is required",
			Code:    400002,
		})
		return
	}

	// 设置默认值
	req.Endpoint.SetDefaults()

	// 验证端点
	if err := req.Endpoint.Validate(); err != nil {
		logger.Errorf("invalid endpoint data: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_endpoint",
			Message: fmt.Sprintf("Invalid endpoint data: %v", err),
			Code:    400003,
		})
		return
	}

	// 注册端点
	_, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var options []RegisterOption
		if req.TTL != nil && *req.TTL > 0 {
			options = append(options, WithTTL(*req.TTL))
		}

		return nil, h.discovery.RegisterEndpoint(ctx, req.Endpoint, options...)
	})

	if err != nil {
		logger.Errorf("failed to register endpoint %s: %v", req.Endpoint.EndpointID, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "registration_failed",
			Message: fmt.Sprintf("Failed to register endpoint: %v", err),
			Code:    500001,
		})
		return
	}

	// TODO: 启动KV订阅任务（根据业务需求实现）
	if req.Endpoint.KVRole == KVRoleProducer {
		logger.Infof("endpoint %s registered as producer, may need kv subscription", req.Endpoint.EndpointID)
		// 这里可以调用相应的服务来启动KV订阅
		// prefixCacheService.AddVLLMInstance(req.Endpoint.InstanceName, ...)
	}

	// 返回结果
	c.JSON(http.StatusCreated, req.Endpoint)
	logger.Infof("endpoint created successfully: %s at %s", req.Endpoint.EndpointID, req.Endpoint.Address)
}

// ListEndpoints 获取端点列表
func (h *EndpointService) ListEndpoints(c *gin.Context) {
	// 可选的查询参数
	modelName := c.Query("model_name")

	result, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if modelName != "" {
			return h.discovery.GetEndpointsByModel(ctx, modelName)
		}
		return h.discovery.GetEndpoints(ctx)
	})

	if err != nil {
		logger.Errorf("failed to list endpoints: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "list_failed",
			Message: fmt.Sprintf("Failed to list endpoints: %v", err),
			Code:    500002,
		})
		return
	}

	endpoints := result.([]*Endpoint)

	c.JSON(http.StatusOK, EndpointListResponse{
		Endpoints: endpoints,
		Total:     len(endpoints),
	})

	logger.Infof("listed %d endpoints", len(endpoints))
}

// GetEndpoint 获取单个端点
func (h *EndpointService) GetEndpoint(c *gin.Context) {
	endpointID := c.Param("endpoint_id")
	if endpointID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "missing_parameter",
			Message: "endpoint_id is required",
			Code:    400004,
		})
		return
	}

	result, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return h.discovery.GetEndpoints(ctx)
	})

	if err != nil {
		logger.Errorf("failed to get endpoints: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "get_failed",
			Message: fmt.Sprintf("Failed to get endpoints: %v", err),
			Code:    500003,
		})
		return
	}

	endpoints := result.([]*Endpoint)
	for _, ep := range endpoints {
		if ep.EndpointID == endpointID {
			c.JSON(http.StatusOK, ep)
			return
		}
	}

	c.JSON(http.StatusNotFound, ErrorResponse{
		Error:   "endpoint_not_found",
		Message: fmt.Sprintf("Endpoint with ID %s not found", endpointID),
		Code:    404001,
	})
}

// UpdateEndpoint 更新端点
func (h *EndpointService) UpdateEndpoint(c *gin.Context) {
	endpointID := c.Param("endpoint_id")
	if endpointID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "missing_parameter",
			Message: "endpoint_id is required",
			Code:    400004,
		})
		return
	}

	var req UpdateEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf("failed to bind update endpoint request: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Message: fmt.Sprintf("Invalid request format: %v", err),
			Code:    400001,
		})
		return
	}

	// 确保有Endpoint实例
	if req.Endpoint == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "missing_endpoint",
			Message: "Endpoint data is required",
			Code:    400002,
		})
		return
	}

	// 确保ID一致
	req.Endpoint.EndpointID = endpointID

	// 设置其他默认值但保留ID
	if req.Endpoint.KVRole == "" {
		req.Endpoint.KVRole = KVRoleBoth
	}
	if req.Endpoint.TransportType == "" {
		req.Endpoint.TransportType = TransportTypeHTTP
	}
	if req.Endpoint.Status == "" {
		req.Endpoint.Status = EndpointStatusHealthy
	}
	if req.Endpoint.KVEventConfig == nil {
		req.Endpoint.KVEventConfig = DefaultKVEventsConfig()
	}
	if req.Endpoint.Metadata == nil {
		req.Endpoint.Metadata = make(map[string]interface{})
	}
	// 更新时间戳
	req.Endpoint.AddedTimestamp = float64(time.Now().Unix())

	// 验证端点
	if err := req.Endpoint.Validate(); err != nil {
		logger.Errorf("invalid endpoint data for update: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_endpoint",
			Message: fmt.Sprintf("Invalid endpoint data: %v", err),
			Code:    400003,
		})
		return
	}

	// 更新端点（实际上是重新注册）
	_, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var options []RegisterOption
		if req.TTL != nil && *req.TTL > 0 {
			options = append(options, WithTTL(*req.TTL))
		}

		return nil, h.discovery.RegisterEndpoint(ctx, req.Endpoint, options...)
	})

	if err != nil {
		logger.Errorf("failed to update endpoint %s: %v", endpointID, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "update_failed",
			Message: fmt.Sprintf("Failed to update endpoint: %v", err),
			Code:    500004,
		})
		return
	}

	c.JSON(http.StatusOK, req.Endpoint)
	logger.Infof("endpoint updated successfully: %s", endpointID)
}

// DeleteEndpoint 删除端点
func (h *EndpointService) DeleteEndpoint(c *gin.Context) {
	endpointID := c.Param("endpoint_id")
	if endpointID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "missing_parameter",
			Message: "endpoint_id is required",
			Code:    400004,
		})
		return
	}

	_, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return nil, h.discovery.RemoveEndpoint(ctx, endpointID)
	})

	if err != nil {
		logger.Errorf("failed to delete endpoint %s: %v", endpointID, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "delete_failed",
			Message: fmt.Sprintf("Failed to delete endpoint: %v", err),
			Code:    500005,
		})
		return
	}

	c.JSON(http.StatusOK, DeleteResponse{
		Status:     "success",
		EndpointID: endpointID,
		Message:    "Endpoint deleted successfully",
	})

	logger.Infof("endpoint deleted successfully: %s", endpointID)
}

// DeleteAllEndpoints 删除所有端点
func (h *EndpointService) DeleteAllEndpoints(c *gin.Context) {
	// 先获取所有端点ID
	result, err := h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return h.discovery.GetEndpoints(ctx)
	})

	if err != nil {
		logger.Errorf("failed to get endpoints before deletion: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "get_failed",
			Message: fmt.Sprintf("Failed to get endpoints: %v", err),
			Code:    500002,
		})
		return
	}

	endpoints := result.([]*Endpoint)
	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.EndpointID
	}

	// 删除所有端点
	_, err = h.runSync(func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 尝试批量删除（如果支持）
		if etcdDiscovery, ok := h.discovery.(*ETCDServiceDiscovery); ok {
			return nil, etcdDiscovery.RemoveAllEndpoints(ctx)
		}

		// 否则逐个删除
		for _, endpointID := range endpointIDs {
			if err := h.discovery.RemoveEndpoint(ctx, endpointID); err != nil {
				logger.Errorf("failed to delete endpoint %s during batch delete: %v", endpointID, err)
				return nil, err
			}
		}
		return nil, nil
	})

	if err != nil {
		logger.Errorf("failed to delete all endpoints: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "delete_all_failed",
			Message: fmt.Sprintf("Failed to delete all endpoints: %v", err),
			Code:    500006,
		})
		return
	}

	c.JSON(http.StatusOK, DeleteResponse{
		Status:    "success",
		Endpoints: endpointIDs,
		Message:   fmt.Sprintf("Successfully deleted %d endpoints", len(endpointIDs)),
	})

	logger.Infof("deleted %d endpoints successfully", len(endpointIDs))
}

// HealthCheck 健康检查
func (h *EndpointService) HealthCheck(c *gin.Context) {
	err := h.discovery.Health(context.Background())
	status := "healthy"
	httpStatus := http.StatusOK

	if err != nil {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
		logger.Errorf("health check failed: %v", err)
	}

	// 确定服务发现类型
	var discoveryType ServiceDiscoveryType
	switch h.discovery.(type) {
	case *ETCDServiceDiscovery:
		discoveryType = ServiceDiscoveryTypeETCD
	case *K8SServiceDiscovery:
		discoveryType = ServiceDiscoveryTypeK8S
	case *StaticServiceDiscovery:
		discoveryType = ServiceDiscoveryTypeStatic
	default:
		discoveryType = "unknown"
	}

	c.JSON(httpStatus, HealthResponse{
		Status:               status,
		ServiceDiscoveryType: discoveryType,
	})
}

// Close 关闭处理器
func (h *EndpointService) Close() error {
	close(h.executor)
	return h.discovery.Close()
}
