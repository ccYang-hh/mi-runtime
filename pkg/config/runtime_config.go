package config

import (
	"fmt"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/discovery"
)

// PluginInfo 插件信息结构体
type PluginInfo struct {
	// PluginName 插件名称
	PluginName string `mapstructure:"plugin_name" json:"plugin_name" yaml:"plugin_name" validate:"required"`

	// PluginVersion 插件版本
	PluginVersion string `mapstructure:"plugin_version" json:"plugin_version" yaml:"plugin_version" validate:"required"`

	// Description 描述
	Description string `mapstructure:"description" json:"description" yaml:"description"`

	// IsDaemon 是否为Daemon Plugin
	IsDaemon bool `mapstructure:"is_daemon" json:"is_daemon" yaml:"is_daemon" default:"false"`

	// Enabled 插件启停控制
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled" validate:"required"`

	// Priority 插件优先级
	Priority int `mapstructure:"priority" json:"priority" yaml:"priority" validate:"min=0,max=1000"`

	// ConfigPath 插件配置文件路径，为空表示插件不支持外部配置文件或使用静态配置
	ConfigPath string `mapstructure:"config_path" json:"config_path" yaml:"config_path"`

	// PluginType 插件类型
	PluginType string `mapstructure:"plugin_type" json:"plugin_type" yaml:"plugin_type" validate:"required"`

	// InitMode 插件初始化模式
	InitMode common.PluginInitMode `mapstructure:"init_mode" json:"init_mode" yaml:"init_mode" validate:"required"`

	// PackagePath Plugin所在包名称
	PackagePath string `mapstructure:"package_path" json:"package_path" yaml:"package_path"`

	// StructName Plugin Struct名称
	StructName string `mapstructure:"struct_name" json:"struct_name" yaml:"struct_name"`

	// ConstructorName Plugin构造函数
	ConstructorName string `mapstructure:"constructor_name" json:"constructor_name" yaml:"constructor_name"`

	// PluginPath 插件加载模式为”Plugin“时，该参数指定.so文件的加载路径
	PluginPath string `mapstructure:"plugin_path" json:"plugin_path" yaml:"plugin_path"`

	// InitParams 插件初始化参数
	InitParams map[string]interface{} `mapstructure:"init_params" json:"init_params" yaml:"init_params"`
}

// PluginRegistryConfig 插件注册表配置
type PluginRegistryConfig struct {
	// Plugins 插件注册表
	Plugins map[string]*PluginInfo `mapstructure:"plugins" json:"plugins" yaml:"plugins" validate:"dive"`

	// DiscoveryPaths 插件发现路径
	DiscoveryPaths []string `mapstructure:"discovery_paths" json:"discovery_paths" yaml:"discovery_paths"`

	// AutoDiscovery 是否启用自动发现
	AutoDiscovery bool `mapstructure:"auto_discovery" json:"auto_discovery" yaml:"auto_discovery"`

	// GlobalConfig 全局插件配置
	GlobalConfig map[string]interface{} `mapstructure:"global_config" json:"global_config" yaml:"global_config"`
}

// PipelineConfig 管道配置结构体
type PipelineConfig struct {
	PipelineName   string               `mapstructure:"pipeline_name" json:"pipeline_name" yaml:"pipeline_name" validate:"required,min=1"`
	Mode           common.ExecutionMode `mapstructure:"mode" json:"mode" yaml:"mode" validate:"required,oneof=sequential parallel dag"`
	MaxConcurrency int                  `mapstructure:"max_concurrency" json:"max_concurrency" yaml:"max_concurrency" validate:"min=1,max=100"`
	Plugins        []interface{}        `mapstructure:"plugins" json:"plugins" yaml:"plugins" validate:"min=1"`
	Routes         []common.RouteInfo   `mapstructure:"routes" json:"routes" yaml:"routes" validate:"dive"`
}

// Validate 验证PipelineConfig
func (pc *PipelineConfig) Validate() error {
	if pc.PipelineName == "" {
		logger.Errorf("pipeline_name is empty")
		return fmt.Errorf("pipeline name is empty")
	}

	if pc.Plugins == nil || len(pc.Plugins) == 0 {
		logger.Errorf("plugins of pipeline %s is empty", pc.PipelineName)
		return fmt.Errorf("plugins of pipeline %s is empty", pc.PipelineName)
	}

	if pc.MaxConcurrency <= 0 {
		logger.Errorf("max concurrency of pipeline %s is invalid", pc.PipelineName)
		return fmt.Errorf("max concurrency of pipeline %s is invalid", pc.PipelineName)
	}

	// 验证执行模式
	switch pc.Mode {
	case common.ExecutionModeSequential:
	case common.ExecutionModeParallel:
		break
	default:
		logger.Errorf("invalid or unsupported execution mode: %s", pc.Mode)
		return fmt.Errorf("invalid or unsupported execution mode: %s", pc.Mode)
	}

	return nil
}

// ETCDConfig ETCD配置结构体
type ETCDConfig struct {
	Host string `mapstructure:"host" json:"host" yaml:"host" validate:"required,hostname_rfc1123|ip"`
	Port int    `mapstructure:"port" json:"port" yaml:"port" validate:"required,min=1,max=65535"`
}

// RuntimeConfig 运行时配置结构体 - 系统核心配置
type RuntimeConfig struct {
	// 应用基本配置
	AppName        string `mapstructure:"app_name" json:"app_name" yaml:"app_name" validate:"required,min=1"`
	AppVersion     string `mapstructure:"app_version" json:"app_version" yaml:"app_version" validate:"required,semver"`
	AppDescription string `mapstructure:"app_description" json:"app_description" yaml:"app_description"`

	// 服务配置
	Host             string                         `mapstructure:"host" json:"host" yaml:"host" validate:"required,hostname_rfc1123|ip"`
	Port             int                            `mapstructure:"port" json:"port" yaml:"port" validate:"required,min=1,max=65535"`
	ServiceDiscovery discovery.ServiceDiscoveryType `mapstructure:"service_discovery" json:"service_discovery" yaml:"service_discovery" validate:"required,oneof=etcd helm"`

	// CORS配置
	CorsOrigins []string `mapstructure:"cors_origins" json:"cors_origins" yaml:"cors_origins" validate:"dive,uri"`

	// 插件配置
	PluginRegistry *PluginRegistryConfig `mapstructure:"plugin_registry" json:"plugin_registry" yaml:"plugin_registry" validate:"required"`

	// 管道配置
	Pipelines []*PipelineConfig `mapstructure:"pipelines" json:"pipelines" yaml:"pipelines" validate:"dive"`

	// ETCD配置
	ETCD *ETCDConfig `mapstructure:"etcd" json:"etcd" yaml:"etcd" validate:"omitempty"`

	// 性能配置
	MaxBatchSize   int `mapstructure:"max_batch_size" json:"max_batch_size" yaml:"max_batch_size" validate:"required,min=1,max=10000"`
	RequestTimeout int `mapstructure:"request_timeout" json:"request_timeout" yaml:"request_timeout" validate:"required,min=1,max=3600"`

	// 监控配置
	EnableMonitor bool   `mapstructure:"enable_monitor" json:"enable_monitor" yaml:"enable_monitor"`
	MonitorConfig string `mapstructure:"monitor_config" json:"monitor_config" yaml:"monitor_config"`

	// 路由配置
	EnableRouter bool `mapstructure:"enable_router" json:"enable_router" yaml:"enable_router"`

	// 安全配置
	EnableAuth bool           `mapstructure:"enable_auth" json:"enable_auth" yaml:"enable_auth"`
	AuthConfig map[string]any `mapstructure:"auth_config" json:"auth_config" yaml:"auth_config"`
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
