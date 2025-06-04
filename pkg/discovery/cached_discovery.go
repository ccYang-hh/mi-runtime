package discovery

import (
	"context"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// CachedServiceDiscovery 带缓存的服务发现基础实现
type CachedServiceDiscovery struct {
	endpoints      []*Endpoint                                    // 缓存的端点列表
	lastUpdate     time.Time                                      // 最后更新时间
	cacheTTL       time.Duration                                  // 缓存生存时间
	mu             sync.RWMutex                                   // 读写锁
	updating       bool                                           // 是否正在更新标志
	updateCond     *sync.Cond                                     // 更新条件变量
	fetchEndpoints func(ctx context.Context) ([]*Endpoint, error) // 获取端点的函数
}

// NewCachedServiceDiscovery 创建带缓存的服务发现实例
func NewCachedServiceDiscovery(cacheTTL time.Duration, fetchFunc func(ctx context.Context) ([]*Endpoint, error)) *CachedServiceDiscovery {
	c := &CachedServiceDiscovery{
		endpoints:      make([]*Endpoint, 0),
		cacheTTL:       cacheTTL,
		fetchEndpoints: fetchFunc,
	}
	c.updateCond = sync.NewCond(&c.mu)
	return c
}

// GetEndpoints 获取所有端点（使用缓存）
func (c *CachedServiceDiscovery) GetEndpoints(ctx context.Context) ([]*Endpoint, error) {
	now := time.Now()

	// 快速路径：读锁检查缓存是否有效
	c.mu.RLock()
	if now.Sub(c.lastUpdate) < c.cacheTTL {
		// 缓存有效，直接返回克隆的数据
		result := make([]*Endpoint, len(c.endpoints))
		for i, ep := range c.endpoints {
			result[i] = ep.Clone()
		}
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	// 慢速路径：需要更新缓存
	c.mu.Lock()
	defer c.mu.Unlock()

	// 双重检查：避免多个goroutine同时更新
	if now.Sub(c.lastUpdate) < c.cacheTTL {
		result := make([]*Endpoint, len(c.endpoints))
		for i, ep := range c.endpoints {
			result[i] = ep.Clone()
		}
		return result, nil
	}

	// 如果有其他goroutine正在更新，等待完成
	for c.updating {
		c.updateCond.Wait()
		// 重新检查缓存
		if time.Now().Sub(c.lastUpdate) < c.cacheTTL {
			result := make([]*Endpoint, len(c.endpoints))
			for i, ep := range c.endpoints {
				result[i] = ep.Clone()
			}
			return result, nil
		}
	}

	// 执行更新
	c.updating = true
	defer func() {
		c.updating = false
		c.updateCond.Broadcast()
	}()

	// 临时释放锁，避免阻塞其他读取操作
	c.mu.Unlock()
	endpoints, err := c.fetchEndpoints(ctx)
	c.mu.Lock()

	if err != nil {
		logger.Errorf("failed to fetch endpoints: %v", err)
		// 返回缓存的数据（如果有）
		result := make([]*Endpoint, len(c.endpoints))
		for i, ep := range c.endpoints {
			result[i] = ep.Clone()
		}
		return result, err
	}

	c.endpoints = endpoints
	c.lastUpdate = time.Now()

	// 返回克隆的数据
	result := make([]*Endpoint, len(c.endpoints))
	for i, ep := range c.endpoints {
		result[i] = ep.Clone()
	}
	return result, nil
}

// GetEndpointsByModel 获取指定模型的端点
func (c *CachedServiceDiscovery) GetEndpointsByModel(ctx context.Context, modelName string) ([]*Endpoint, error) {
	endpoints, err := c.GetEndpoints(ctx)
	if err != nil {
		logger.Errorf("failed to get endpoints for model %s: %v", modelName, err)
		return nil, err
	}

	var result []*Endpoint
	for _, ep := range endpoints {
		if ep.ModelName == modelName {
			result = append(result, ep)
		}
	}
	return result, nil
}

// InvalidateCache 手动使缓存失效
func (c *CachedServiceDiscovery) InvalidateCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUpdate = time.Time{} // 设置为零值，强制下次更新
	logger.Infof("cache invalidated")
}

// UpdateEndpoints 直接更新缓存中的端点（用于实时更新）
func (c *CachedServiceDiscovery) UpdateEndpoints(endpoints []*Endpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoints = endpoints
	c.lastUpdate = time.Now()
	logger.Infof("cache updated with %d endpoints", len(endpoints))
}

// AddEndpoint 添加端点到缓存
func (c *CachedServiceDiscovery) AddEndpoint(endpoint *Endpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否已存在，如果存在则更新
	for i, ep := range c.endpoints {
		if ep.EndpointID == endpoint.EndpointID {
			c.endpoints[i] = endpoint.Clone()
			c.lastUpdate = time.Now()
			logger.Infof("endpoint %s updated in cache", endpoint.EndpointID)
			return
		}
	}

	// 不存在则添加
	c.endpoints = append(c.endpoints, endpoint.Clone())
	c.lastUpdate = time.Now()
	logger.Infof("endpoint %s added to cache", endpoint.EndpointID)
}

// RemoveEndpointFromCache 从缓存中移除端点
func (c *CachedServiceDiscovery) RemoveEndpointFromCache(endpointID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, ep := range c.endpoints {
		if ep.EndpointID == endpointID {
			// 使用切片技巧高效删除元素
			c.endpoints[i] = c.endpoints[len(c.endpoints)-1]
			c.endpoints = c.endpoints[:len(c.endpoints)-1]
			c.lastUpdate = time.Now()
			logger.Infof("endpoint %s removed from cache", endpointID)
			return
		}
	}
}

// Health 健康检查
func (c *CachedServiceDiscovery) Health(ctx context.Context) error {
	c.mu.RLock()
	hasEndpoints := len(c.endpoints) > 0
	c.mu.RUnlock()

	if !hasEndpoints {
		logger.Warnf("no endpoints available in cache")
	}
	return nil
}
