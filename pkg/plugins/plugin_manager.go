package plugins

import (
	"context"
	"xfusion.com/tmatrix/runtime/pkg/config"
)

type IPluginManager interface {
	Start() error
	Shutdown() error
}

type PluginManager struct{}

func NewPluginManager(ctx context.Context,
	pluginRegistry *config.PluginRegistryConfig) (IPluginManager, error) {
	return &PluginManager{}, nil
}

func (pm *PluginManager) Shutdown() error {
	return nil
}

func (pm *PluginManager) Start() error {
	return nil
}
