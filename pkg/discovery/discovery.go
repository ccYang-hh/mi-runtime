package discovery

import (
	"context"
)

// ServiceDiscovery 服务发现核心接口
type ServiceDiscovery interface {
	// GetEndpoints 获取所有可用端点列表
	GetEndpoints(ctx context.Context) ([]*Endpoint, error)

	// GetEndpointsByModel 根据模型名称获取对应的端点列表
	GetEndpointsByModel(ctx context.Context, modelName string) ([]*Endpoint, error)

	// RegisterEndpoint 注册新的端点到服务发现中心
	RegisterEndpoint(ctx context.Context, endpoint *Endpoint, options ...RegisterOption) error

	// RemoveEndpoint 从服务发现中心移除指定的端点
	RemoveEndpoint(ctx context.Context, endpointID string) error

	// Health 执行服务发现的健康检查
	Health(ctx context.Context) error

	// Close 关闭服务发现，释放相关资源
	Close() error
}

// RegisterOption 端点注册选项接口
type RegisterOption interface {
	Apply(*RegisterConfig)
}

// RegisterConfig 端点注册配置
type RegisterConfig struct {
	TTL int // 端点生存时间（秒），0表示永久有效
}

// WithTTL 设置端点TTL的选项
type WithTTL int

func (w WithTTL) Apply(config *RegisterConfig) {
	config.TTL = int(w)
}

// EventHandler 端点变化事件处理器接口
type EventHandler interface {
	// OnEndpointAdded 当新端点被添加时调用
	OnEndpointAdded(endpoint *Endpoint)

	// OnEndpointRemoved 当端点被移除时调用
	OnEndpointRemoved(endpointID string)

	// OnEndpointUpdated 当端点信息被更新时调用
	OnEndpointUpdated(endpoint *Endpoint)
}

// WatchableServiceDiscovery 支持监听端点变化的服务发现接口
type WatchableServiceDiscovery interface {
	ServiceDiscovery

	// Watch 开始监听端点变化事件
	Watch(ctx context.Context, handler EventHandler) error
}
