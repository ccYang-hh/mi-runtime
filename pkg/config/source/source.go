package source

import (
	"context"

	"github.com/mitchellh/mapstructure"
)

// Source defines the generic interface for configuration sources
type Source[T any] interface {
	// ID returns the unique identifier for this configuration source
	ID() string

	// Load retrieves and unmarshals the current configuration
	Load(ctx context.Context) (*T, error)

	// Watch begins monitoring configuration changes
	// The callback function will be called with the new configuration whenever changes are detected
	Watch(ctx context.Context, callback func(*T)) error

	// Stop stops monitoring configuration changes
	Stop() error
}

// BaseSource provides common functionality for configuration sources
type BaseSource[T any] struct {
	id string
}

// NewBaseSource creates a new base configuration source
func NewBaseSource[T any](id string) BaseSource[T] {
	return BaseSource[T]{id: id}
}

// ID returns the configuration source identifier
func (b BaseSource[T]) ID() string {
	return b.id
}

// unmarshalConfig converts a map to a strongly typed configuration struct
func (b BaseSource[T]) unmarshalConfig(data map[string]any) (*T, error) {
	var config T

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Metadata: nil,
		Result:   &config,
		TagName:  "mapstructure",
		// Handle embedded structs
		Squash: true,
		// Convert string to time.Time, etc.
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	})
	if err != nil {
		return nil, err
	}

	if err := decoder.Decode(data); err != nil {
		return nil, err
	}

	return &config, nil
}

// getRawConfig should be implemented by concrete sources
type rawConfigLoader interface {
	getRawConfig(ctx context.Context) (map[string]any, error)
}
