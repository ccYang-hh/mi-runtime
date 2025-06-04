package discovery

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ServiceDiscoveryType 服务发现类型枚举
type ServiceDiscoveryType string

const (
	ServiceDiscoveryTypeStatic ServiceDiscoveryType = "static" // 静态服务发现
	ServiceDiscoveryTypeK8S    ServiceDiscoveryType = "k8s"    // Kubernetes服务发现
	ServiceDiscoveryTypeETCD   ServiceDiscoveryType = "etcd"   // ETCD服务发现
)

// TransportType 传输类型枚举
type TransportType string

const (
	TransportTypeTCP   TransportType = "tcp"   // TCP传输
	TransportTypeHTTP  TransportType = "http"  // HTTP传输
	TransportTypeHTTPS TransportType = "https" // HTTPS传输
)

// EndpointType 端点服务类型枚举，基于OpenAI标准
type EndpointType string

const (
	EndpointTypeCompletion     EndpointType = "completion"      // 文本补全服务
	EndpointTypeChatCompletion EndpointType = "chat_completion" // 对话补全服务
)

// EndpointStatus 端点健康状态枚举
type EndpointStatus string

const (
	EndpointStatusHealthy   EndpointStatus = "healthy"   // 健康状态
	EndpointStatusUnhealthy EndpointStatus = "unhealthy" // 不健康状态
)

// KVRole KV缓存角色枚举
type KVRole string

const (
	KVRoleProducer KVRole = "producer" // 生产者角色，只写入KV缓存
	KVRoleConsumer KVRole = "consumer" // 消费者角色，只读取KV缓存
	KVRoleBoth     KVRole = "both"     // 双重角色，既读又写KV缓存
)

// KVEventsConfig KV事件配置结构体
type KVEventsConfig struct {
	Publisher      string  `json:"publisher"`       // 事件发布器类型，如 "zmq"
	Endpoint       string  `json:"endpoint"`        // 事件发布端点地址
	ReplayEndpoint *string `json:"replay_endpoint"` // 重放端点地址，可选
}

// DefaultKVEventsConfig 返回默认的KV事件配置
func DefaultKVEventsConfig() *KVEventsConfig {
	return &KVEventsConfig{
		Publisher: "zmq",
		Endpoint:  "tcp://*:5557",
	}
}

// Endpoint 端点信息结构体，既用于内部逻辑也用于API交互
type Endpoint struct {
	EndpointID     string                 `json:"endpoint_id" binding:"-"`               // 端点唯一标识符
	Address        string                 `json:"address" binding:"required"`            // 服务地址，如 "http://localhost:8080"
	ModelName      string                 `json:"model_name" binding:"required"`         // 当前主服务模型名称，如 "gpt-3.5-turbo"
	Priority       int                    `json:"priority" binding:"omitempty"`          // 优先级，数值越大优先级越高，用于负载均衡
	KVRole         KVRole                 `json:"kv_role" binding:"omitempty"`           // KV缓存角色，定义该端点的缓存读写权限
	InstanceName   string                 `json:"instance_name" binding:"required"`      // 实例名称或ID，用于标识特定的服务实例
	TransportType  TransportType          `json:"transport_type" binding:"omitempty"`    // 传输协议类型
	Status         EndpointStatus         `json:"status" binding:"omitempty"`            // 端点当前健康状态
	EndpointTypes  []EndpointType         `json:"endpoint_type" binding:"required"`      // 端点支持的服务类型列表
	KVEventConfig  *KVEventsConfig        `json:"kv_event_config,omitempty" binding:"-"` // KV事件配置，用于缓存事件通知
	AddedTimestamp float64                `json:"added_timestamp,omitempty" binding:"-"` // 端点添加时间戳（Unix时间）
	Metadata       map[string]interface{} `json:"metadata,omitempty" binding:"-"`        // 扩展元数据，用于存储额外信息
}

// NewEndpoint 创建新的端点实例
func NewEndpoint(endpointID, address, modelName string) *Endpoint {
	return &Endpoint{
		EndpointID:     endpointID,
		Address:        address,
		ModelName:      modelName,
		Priority:       0,
		KVRole:         KVRoleBoth,
		TransportType:  TransportTypeHTTP,
		Status:         EndpointStatusUnhealthy,
		EndpointTypes:  []EndpointType{},
		KVEventConfig:  DefaultKVEventsConfig(),
		AddedTimestamp: float64(time.Now().Unix()),
		Metadata:       make(map[string]interface{}),
	}
}

// SetDefaults 设置默认值（用于API创建时）
func (e *Endpoint) SetDefaults() {
	if e.EndpointID == "" {
		e.EndpointID = generateUUID()
	}
	if e.KVRole == "" {
		e.KVRole = KVRoleBoth
	}
	if e.TransportType == "" {
		e.TransportType = TransportTypeHTTP
	}
	if e.Status == "" {
		e.Status = EndpointStatusHealthy
	}
	if e.KVEventConfig == nil {
		e.KVEventConfig = DefaultKVEventsConfig()
	}
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	if e.AddedTimestamp == 0 {
		e.AddedTimestamp = float64(time.Now().Unix())
	}
}

// generateUUID 生成UUID（简化版，实际项目中应使用专业的UUID库）
func generateUUID() string {
	return uuid.New().String()
}

// Clone 深拷贝端点实例，确保线程安全
func (e *Endpoint) Clone() *Endpoint {
	clone := *e
	// 深拷贝切片和映射
	clone.EndpointTypes = make([]EndpointType, len(e.EndpointTypes))
	copy(clone.EndpointTypes, e.EndpointTypes)

	clone.Metadata = make(map[string]interface{})
	for k, v := range e.Metadata {
		clone.Metadata[k] = v
	}

	if e.KVEventConfig != nil {
		config := *e.KVEventConfig
		clone.KVEventConfig = &config
	}

	return &clone
}

// ToJSON 将端点转换为JSON字符串
func (e *Endpoint) ToJSON() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ToJSONString 将端点转换为JSON字符串（便捷方法）
func (e *Endpoint) ToJSONString() (string, error) {
	data, err := e.ToJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// EndpointFromJSON 从JSON字节数组创建端点实例
func EndpointFromJSON(data []byte) (*Endpoint, error) {
	var endpoint Endpoint
	err := json.Unmarshal(data, &endpoint)
	if err != nil {
		return nil, err
	}
	return &endpoint, nil
}

// EndpointFromJSONString 从JSON字符串创建端点实例
func EndpointFromJSONString(jsonStr string) (*Endpoint, error) {
	return EndpointFromJSON([]byte(jsonStr))
}

// Validate 验证端点数据的有效性
func (e *Endpoint) Validate() error {
	if e.EndpointID == "" {
		return errors.New("invalid endpoint")
	}
	if e.Address == "" {
		return errors.New("invalid endpoint")
	}
	if e.ModelName == "" {
		return errors.New("invalid endpoint")
	}
	return nil
}

// IsHealthy 检查端点是否处于健康状态
func (e *Endpoint) IsHealthy() bool {
	return e.Status == EndpointStatusHealthy
}

// PodServiceLabel POD资源标签枚举，用于Kubernetes服务发现
type PodServiceLabel string

const (
	PodServiceLabelModelName            PodServiceLabel = "tmatrix.runtime.service.model"     // 模型名称标签
	PodServiceLabelServiceTransportType PodServiceLabel = "tmatrix.runtime.service.transport" // 传输类型标签
	PodServiceLabelServicePriority      PodServiceLabel = "tmatrix.runtime.service.priority"  // 优先级标签
)

// CreateEndpointRequest 创建端点请求
type CreateEndpointRequest struct {
	*Endpoint
	TTL *int `json:"ttl,omitempty" binding:"-"` // TTL单独处理，不属于Endpoint结构本身
}

// UpdateEndpointRequest 更新端点请求
type UpdateEndpointRequest struct {
	*Endpoint
	TTL *int `json:"ttl,omitempty" binding:"-"` // TTL单独处理
}

// EndpointListResponse 端点列表响应
type EndpointListResponse struct {
	Endpoints []*Endpoint `json:"endpoints"`
	Total     int         `json:"total"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status               string               `json:"status"`
	ServiceDiscoveryType ServiceDiscoveryType `json:"service_discovery_type"`
}

// DeleteResponse 删除响应
type DeleteResponse struct {
	Status     string   `json:"status"`
	EndpointID string   `json:"endpoint_id,omitempty"`
	Endpoints  []string `json:"endpoints,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}
