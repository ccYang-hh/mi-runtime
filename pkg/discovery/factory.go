package discovery

import (
	"fmt"
	"sync"
	"time"
)

var (
	// 全局单例实例
	globalServiceDiscovery ServiceDiscovery
	// 单例初始化锁
	discoveryOnce sync.Once
)

// GetServiceDiscovery 使用指定配置获取单例
// 注意：一旦创建，后续调用会忽略新的配置
func GetServiceDiscovery(config *Config) (ServiceDiscovery, error) {
	var err error
	discoveryOnce.Do(func() {
		globalServiceDiscovery, err = newServiceDiscovery(config)
	})

	if err != nil {
		return nil, err
	}

	return globalServiceDiscovery, nil
}

// GetServiceDiscoveryDirectly ...
func GetServiceDiscoveryDirectly() ServiceDiscovery {
	return globalServiceDiscovery
}

// Config 服务发现配置
type Config struct {
	Type     ServiceDiscoveryType `yaml:"type"`
	CacheTTL time.Duration        `yaml:"cache_ttl"`
	ETCD     *ETCDConfig          `yaml:"etcd,omitempty"`
	Static   *StaticConfig        `yaml:"static,omitempty"`
}

// ETCDConfig ETCD配置
type ETCDConfig struct {
	Endpoints []string `yaml:"endpoints"`
	Prefix    string   `yaml:"prefix"`
}

// StaticConfig 静态配置
type StaticConfig struct {
	Endpoints []*Endpoint `yaml:"endpoints"`
}

// newServiceDiscovery 创建服务发现实例
func newServiceDiscovery(config *Config) (ServiceDiscovery, error) {
	switch config.Type {
	case ServiceDiscoveryTypeStatic:
		if config.Static == nil {
			return nil, fmt.Errorf("static config is required for static service discovery")
		}
		return NewStaticServiceDiscovery(config.Static.Endpoints), nil

	case ServiceDiscoveryTypeETCD:
		if config.ETCD == nil {
			return nil, fmt.Errorf("etcd config is required for etcd service discovery")
		}
		return NewETCDServiceDiscovery(config.ETCD.Endpoints, config.ETCD.Prefix, config.CacheTTL)

	default:
		return nil, fmt.Errorf("unsupported service discovery type: %s", config.Type)
	}
}
