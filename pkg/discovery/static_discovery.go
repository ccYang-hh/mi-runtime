package discovery

import (
	"context"
	"errors"
)

// StaticServiceDiscovery 静态服务发现实现
type StaticServiceDiscovery struct {
	*CachedServiceDiscovery             // 嵌入缓存服务发现
	staticEndpoints         []*Endpoint // 静态端点列表
}

// NewStaticServiceDiscovery 创建静态服务发现
func NewStaticServiceDiscovery(endpoints []*Endpoint) *StaticServiceDiscovery {
	// 克隆端点，避免外部修改
	clonedEndpoints := make([]*Endpoint, len(endpoints))
	for i, ep := range endpoints {
		clonedEndpoints[i] = ep.Clone()
	}

	s := &StaticServiceDiscovery{
		staticEndpoints: clonedEndpoints,
	}

	// 使用无限大的缓存TTL，因为静态端点不会变化
	s.CachedServiceDiscovery = NewCachedServiceDiscovery(
		1<<63-1, // 最大时间间隔，实际上永不过期
		s.fetchStaticEndpoints,
	)

	return s
}

// fetchStaticEndpoints 获取静态端点
func (s *StaticServiceDiscovery) fetchStaticEndpoints(ctx context.Context) ([]*Endpoint, error) {
	// 返回克隆的端点，保证线程安全
	result := make([]*Endpoint, len(s.staticEndpoints))
	for i, ep := range s.staticEndpoints {
		result[i] = ep.Clone()
	}
	return result, nil
}

// RegisterEndpoint 静态服务发现不支持注册
func (s *StaticServiceDiscovery) RegisterEndpoint(ctx context.Context,
	endpoint *Endpoint, options ...RegisterOption) error {
	return errors.New("RegisterEndpoint: static service discovery does not support dynamic registration")
}

// RemoveEndpoint 静态服务发现不支持移除
func (s *StaticServiceDiscovery) RemoveEndpoint(ctx context.Context, endpointID string) error {
	return errors.New("RemoveEndpoint: static service discovery does not support dynamic removal")
}

// Close 关闭静态服务发现
func (s *StaticServiceDiscovery) Close() error {
	return nil
}
