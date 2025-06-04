package config

import (
	"xfusion.com/tmatrix/runtime/pkg/discovery"
)

// PluginInfo 插件信息结构体
type PluginInfo struct {
	Enabled    bool   `mapstructure:"enabled" json:"enabled" validate:"required"`
	Module     string `mapstructure:"module" json:"module"`
	Path       string `mapstructure:"path" json:"path"`
	Priority   int    `mapstructure:"priority" json:"priority" validate:"min=0,max=1000"`
	ConfigPath string `mapstructure:"config_path" json:"config_path"`
}

// PluginRegistryConfig 插件注册表配置
type PluginRegistryConfig struct {
	Plugins        map[string]*PluginInfo `mapstructure:"plugins" json:"plugins" validate:"dive"`
	DiscoveryPaths []string               `mapstructure:"discovery_paths" json:"discovery_paths"`
	AutoDiscovery  bool                   `mapstructure:"auto_discovery" json:"auto_discovery"`
}

// PipelineRoute 管道路由结构体
type PipelineRoute struct {
	Path   string `mapstructure:"path" json:"path" validate:"required"`
	Method string `mapstructure:"method" json:"method" validate:"required,oneof=GET POST PUT DELETE PATCH"`
}

// PipelineConfig 管道配置结构体
type PipelineConfig struct {
	PipelineName string           `mapstructure:"pipeline_name" json:"pipeline_name" validate:"required,min=1"`
	Plugins      []string         `mapstructure:"plugins" json:"plugins"`
	Routes       []*PipelineRoute `mapstructure:"routes" json:"routes" validate:"dive"`
}

// ETCDConfig ETCD配置结构体
type ETCDConfig struct {
	Host string `mapstructure:"host" json:"host" validate:"required,hostname_rfc1123|ip"`
	Port int    `mapstructure:"port" json:"port" validate:"required,min=1,max=65535"`
}

// RuntimeConfig 运行时配置结构体 - 系统核心配置
type RuntimeConfig struct {
	// 应用基本配置
	AppName        string `mapstructure:"app_name" json:"app_name" validate:"required,min=1"`
	AppVersion     string `mapstructure:"app_version" json:"app_version" validate:"required,semver"`
	AppDescription string `mapstructure:"app_description" json:"app_description"`

	// 服务配置
	Host             string                         `mapstructure:"host" json:"host" validate:"required,hostname_rfc1123|ip"`
	Port             int                            `mapstructure:"port" json:"port" validate:"required,min=1,max=65535"`
	ServiceDiscovery discovery.ServiceDiscoveryType `mapstructure:"service_discovery" json:"service_discovery" validate:"required,oneof=etcd k8s"`

	// CORS配置
	CorsOrigins []string `mapstructure:"cors_origins" json:"cors_origins" validate:"dive,uri"`

	// 插件配置
	PluginRegistry *PluginRegistryConfig `mapstructure:"plugin_registry" json:"plugin_registry" validate:"required"`

	// 管道配置
	Pipelines []*PipelineConfig `mapstructure:"pipelines" json:"pipelines" validate:"dive"`

	// ETCD配置
	ETCD *ETCDConfig `mapstructure:"etcd" json:"etcd" validate:"omitempty"`

	// 性能配置
	MaxBatchSize   int `mapstructure:"max_batch_size" json:"max_batch_size" validate:"required,min=1,max=10000"`
	RequestTimeout int `mapstructure:"request_timeout" json:"request_timeout" validate:"required,min=1,max=3600"`

	// 监控配置
	EnableMonitor bool   `mapstructure:"enable_monitor" json:"enable_monitor"`
	MonitorConfig string `mapstructure:"monitor_config" json:"monitor_config"`

	// 路由配置
	EnableRouter bool `mapstructure:"enable_router" json:"enable_router"`

	// 安全配置
	EnableAuth bool           `mapstructure:"enable_auth" json:"enable_auth"`
	AuthConfig map[string]any `mapstructure:"auth_config" json:"auth_config"`
}

// NewRuntimeConfig 创建默认的运行时配置
func NewRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		AppName:          "tMatrix Inference System",
		AppVersion:       "0.1.0",
		AppDescription:   "Intelligent Scheduling and Control Center Based on Multi-Instance Heterogeneous Inference Engine",
		Host:             "0.0.0.0",
		Port:             8080,
		ServiceDiscovery: discovery.ServiceDiscoveryTypeETCD,
		CorsOrigins:      []string{"*"},
		PluginRegistry: &PluginRegistryConfig{
			Plugins:        make(map[string]*PluginInfo),
			DiscoveryPaths: []string{},
			AutoDiscovery:  true,
		},
		Pipelines: []*PipelineConfig{},
		ETCD: &ETCDConfig{
			Host: "127.0.0.1",
			Port: 2379,
		},
		MaxBatchSize:   128,
		RequestTimeout: 60,
		EnableMonitor:  true,
		MonitorConfig:  "",
		EnableRouter:   false,
		EnableAuth:     false,
		AuthConfig:     make(map[string]any),
	}
}

// PluginEntry 插件条目，用于返回启用的插件列表
type PluginEntry struct {
	Name string
	Info *PluginInfo
}

// GetEnabledPlugins 获取所有启用的插件，按优先级排序
func (r *RuntimeConfig) GetEnabledPlugins() []PluginEntry {
	var enabled []PluginEntry

	for name, info := range r.PluginRegistry.Plugins {
		if info.Enabled {
			enabled = append(enabled, PluginEntry{
				Name: name,
				Info: info,
			})
		}
	}

	// 按优先级排序
	for i := 0; i < len(enabled)-1; i++ {
		for j := i + 1; j < len(enabled); j++ {
			if enabled[i].Info.Priority > enabled[j].Info.Priority {
				enabled[i], enabled[j] = enabled[j], enabled[i]
			}
		}
	}

	return enabled
}

// GetPipelineByName 根据名称获取管道配置
func (r *RuntimeConfig) GetPipelineByName(name string) *PipelineConfig {
	for _, pipeline := range r.Pipelines {
		if pipeline.PipelineName == name {
			return pipeline
		}
	}
	return nil
}
