package api

import (
	"github.com/gin-gonic/gin"

	"xfusion.com/tmatrix/runtime/pkg/discovery"
)

// ServiceDiscoveryProvider 服务发现路由Provider
type ServiceDiscoveryProvider struct {
	*BaseRouterGroup
	discoveryService *discovery.EndpointService
}

// NewServiceDiscoveryProvider ...
func NewServiceDiscoveryProvider(discoveryService *discovery.EndpointService) *ServiceDiscoveryProvider {
	return &ServiceDiscoveryProvider{
		BaseRouterGroup:  NewBaseRouterGroup("endpoints", "/api/v1/endpoints"),
		discoveryService: discoveryService,
	}
}

func (provider *ServiceDiscoveryProvider) RegisterRoutes(group *gin.RouterGroup) {
	// 服务发现相关路由
	group.GET("/:endpoint_id", provider.discoveryService.GetEndpoint)
	group.GET("", provider.discoveryService.ListEndpoints)
	group.POST("", provider.discoveryService.CreateEndpoint)
	group.PUT("/:endpoint_id", provider.discoveryService.UpdateEndpoint)
	group.DELETE("/:endpoint_id", provider.discoveryService.DeleteEndpoint)
	group.DELETE("/all", provider.discoveryService.DeleteAllEndpoints)
}
