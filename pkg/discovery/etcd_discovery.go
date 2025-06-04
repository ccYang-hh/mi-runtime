package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// ETCDServiceDiscovery ETCD服务发现实现
type ETCDServiceDiscovery struct {
	*CachedServiceDiscovery                             // 嵌入缓存服务发现
	client                  *clientv3.Client            // ETCD客户端
	prefix                  string                      // ETCD键前缀
	leases                  map[string]clientv3.LeaseID // 租约映射表
	leaseMu                 sync.RWMutex                // 租约锁
	watchCh                 chan struct{}               // 监控通道
	cancelFunc              context.CancelFunc          // 取消函数
	closed                  bool                        // 关闭标志
	closeMu                 sync.RWMutex                // 关闭锁
	eventHandler            EventHandler                // 事件处理器
}

// NewETCDServiceDiscovery 创建ETCD服务发现实例
func NewETCDServiceDiscovery(endpoints []string, prefix string, cacheTTL time.Duration) (*ETCDServiceDiscovery, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()), // 新的不安全连接方式
		},
	})
	if err != nil {
		logger.Errorf("failed to create etcd client: %v", err)
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	prefix = strings.TrimSuffix(prefix, "/")

	e := &ETCDServiceDiscovery{
		client:  client,
		prefix:  prefix,
		leases:  make(map[string]clientv3.LeaseID),
		watchCh: make(chan struct{}, 1),
	}

	// 创建缓存服务发现
	e.CachedServiceDiscovery = NewCachedServiceDiscovery(cacheTTL, e.fetchETCDEndpoints)

	// 启动监听
	e.startWatch()

	logger.Infof("etcd service discovery initialized with prefix: %s", prefix)
	return e, nil
}

// fetchETCDEndpoints 从ETCD获取端点
func (e *ETCDServiceDiscovery) fetchETCDEndpoints(ctx context.Context) ([]*Endpoint, error) {
	resp, err := e.client.Get(ctx, e.prefix+"/", clientv3.WithPrefix())
	if err != nil {
		logger.Errorf("failed to get endpoints from etcd: %v", err)
		return nil, fmt.Errorf("failed to get endpoints from etcd: %w", err)
	}

	var endpoints []*Endpoint
	for _, kv := range resp.Kvs {
		endpoint, err := EndpointFromJSON(kv.Value)
		if err != nil {
			logger.Errorf("failed to decode endpoint from etcd key %s: %v", string(kv.Key), err)
			continue
		}
		endpoints = append(endpoints, endpoint)
	}

	logger.Infof("fetched %d endpoints from etcd", len(endpoints))
	return endpoints, nil
}

// startWatch 启动ETCD监听
func (e *ETCDServiceDiscovery) startWatch() {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFunc = cancel

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("etcd watch goroutine panicked: %v", r)
			}
		}()

		watchCh := e.client.Watch(ctx, e.prefix+"/", clientv3.WithPrefix())

		for {
			select {
			case <-ctx.Done():
				logger.Infof("etcd watch stopped")
				return
			case watchResp, ok := <-watchCh:
				if !ok {
					logger.Warnf("etcd watch channel closed, restarting...")
					time.Sleep(time.Second)
					watchCh = e.client.Watch(ctx, e.prefix+"/", clientv3.WithPrefix())
					continue
				}

				if watchResp.Err() != nil {
					logger.Errorf("etcd watch error: %v", watchResp.Err())
					continue
				}

				e.handleWatchEvents(watchResp.Events)
			}
		}
	}()
}

// handleWatchEvents 处理ETCD监听事件
func (e *ETCDServiceDiscovery) handleWatchEvents(events []*clientv3.Event) {
	shouldInvalidate := false

	for _, event := range events {
		switch event.Type {
		case clientv3.EventTypePut:
			endpointID := e.extractEndpointIDFromKey(string(event.Kv.Key))
			logger.Infof("etcd endpoint added/updated: %s", endpointID)

			// 解析端点并更新本地缓存
			if endpoint, err := EndpointFromJSON(event.Kv.Value); err == nil {
				e.AddEndpoint(endpoint)
				if e.eventHandler != nil {
					e.eventHandler.OnEndpointAdded(endpoint)
				}
			} else {
				logger.Errorf("failed to parse endpoint from etcd event: %v", err)
			}
			shouldInvalidate = true

		case clientv3.EventTypeDelete:
			endpointID := e.extractEndpointIDFromKey(string(event.Kv.Key))
			logger.Infof("etcd endpoint removed: %s", endpointID)

			// 从本地缓存移除
			e.RemoveEndpointFromCache(endpointID)
			if e.eventHandler != nil {
				e.eventHandler.OnEndpointRemoved(endpointID)
			}
			shouldInvalidate = true
		}
	}

	if shouldInvalidate {
		e.InvalidateCache()
	}
}

// extractEndpointIDFromKey 从ETCD键中提取端点ID
func (e *ETCDServiceDiscovery) extractEndpointIDFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// RegisterEndpoint 注册端点到ETCD
func (e *ETCDServiceDiscovery) RegisterEndpoint(ctx context.Context, endpoint *Endpoint, options ...RegisterOption) error {
	if err := endpoint.Validate(); err != nil {
		logger.Errorf("invalid endpoint for registration: %v", err)
		return fmt.Errorf("invalid endpoint: %w", err)
	}

	config := &RegisterConfig{}
	for _, opt := range options {
		opt.Apply(config)
	}

	key := fmt.Sprintf("%s/%s", e.prefix, endpoint.EndpointID)
	data, err := endpoint.ToJSON()
	if err != nil {
		logger.Errorf("failed to marshal endpoint %s: %v", endpoint.EndpointID, err)
		return fmt.Errorf("failed to marshal endpoint: %w", err)
	}

	if config.TTL > 0 {
		// 使用租约
		lease, err := e.client.Grant(ctx, int64(config.TTL))
		if err != nil {
			logger.Errorf("failed to create lease for endpoint %s: %v", endpoint.EndpointID, err)
			return fmt.Errorf("failed to create lease: %w", err)
		}

		_, err = e.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID))
		if err != nil {
			logger.Errorf("failed to put endpoint %s with lease: %v", endpoint.EndpointID, err)
			return fmt.Errorf("failed to put endpoint with lease: %w", err)
		}

		e.leaseMu.Lock()
		e.leases[endpoint.EndpointID] = lease.ID
		e.leaseMu.Unlock()

		logger.Infof("registered endpoint %s with TTL %d", endpoint.EndpointID, config.TTL)
	} else {
		// 不使用租约
		_, err = e.client.Put(ctx, key, string(data))
		if err != nil {
			logger.Errorf("failed to put endpoint %s: %v", endpoint.EndpointID, err)
			return fmt.Errorf("failed to put endpoint: %w", err)
		}

		logger.Infof("registered endpoint %s", endpoint.EndpointID)
	}

	// 同步更新本地缓存
	e.AddEndpoint(endpoint)

	return nil
}

// RemoveEndpoint 从ETCD移除端点
func (e *ETCDServiceDiscovery) RemoveEndpoint(ctx context.Context, endpointID string) error {
	key := fmt.Sprintf("%s/%s", e.prefix, endpointID)

	// 撤销租约（如果有）
	e.leaseMu.Lock()
	if leaseID, exists := e.leases[endpointID]; exists {
		delete(e.leases, endpointID)
		e.leaseMu.Unlock()

		if _, err := e.client.Revoke(ctx, leaseID); err != nil {
			logger.Errorf("failed to revoke lease for endpoint %s: %v", endpointID, err)
		}
	} else {
		e.leaseMu.Unlock()
	}

	// 删除键
	_, err := e.client.Delete(ctx, key)
	if err != nil {
		logger.Errorf("failed to delete endpoint %s: %v", endpointID, err)
		return fmt.Errorf("failed to delete endpoint: %w", err)
	}

	// 同步更新本地缓存
	e.RemoveEndpointFromCache(endpointID)

	logger.Infof("removed endpoint %s", endpointID)
	return nil
}

// RemoveAllEndpoints 移除所有端点
func (e *ETCDServiceDiscovery) RemoveAllEndpoints(ctx context.Context) error {
	_, err := e.client.Delete(ctx, e.prefix+"/", clientv3.WithPrefix())
	if err != nil {
		logger.Errorf("failed to delete all endpoints with prefix %s: %v", e.prefix, err)
		return fmt.Errorf("failed to delete all endpoints: %w", err)
	}

	// 清空本地缓存
	e.UpdateEndpoints([]*Endpoint{})

	logger.Infof("removed all endpoints with prefix %s", e.prefix)
	return nil
}

// SetEventHandler 设置事件处理器
func (e *ETCDServiceDiscovery) SetEventHandler(handler EventHandler) {
	e.eventHandler = handler
	logger.Infof("event handler set for etcd service discovery")
}

// Health 健康检查
func (e *ETCDServiceDiscovery) Health(ctx context.Context) error {
	e.closeMu.RLock()
	closed := e.closed
	e.closeMu.RUnlock()

	if closed {
		err := fmt.Errorf("etcd service discovery is closed")
		logger.Errorf("health check failed: %v", err)
		return err
	}

	// 测试ETCD连接
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err := e.client.Get(ctx, "health-check")
	if err != nil {
		logger.Errorf("etcd health check failed: %v", err)
		return fmt.Errorf("etcd health check failed: %w", err)
	}

	return e.CachedServiceDiscovery.Health(ctx)
}

// Close 关闭ETCD服务发现
func (e *ETCDServiceDiscovery) Close() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()

	if e.closed {
		return nil
	}

	e.closed = true

	// 停止监听
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	// 撤销所有租约
	e.leaseMu.Lock()
	for endpointID, leaseID := range e.leases {
		if _, err := e.client.Revoke(context.Background(), leaseID); err != nil {
			logger.Errorf("failed to revoke lease for endpoint %s: %v", endpointID, err)
		}
	}
	e.leases = make(map[string]clientv3.LeaseID)
	e.leaseMu.Unlock()

	// 关闭ETCD客户端
	err := e.client.Close()
	if err != nil {
		logger.Errorf("failed to close etcd client: %v", err)
	}

	logger.Infof("etcd service discovery closed")
	return err
}
