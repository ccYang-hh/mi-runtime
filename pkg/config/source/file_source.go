package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// FileSource implements Source for file-based configuration
type FileSource[T any] struct {
	BaseSource[T]
	configPath string
	watcher    *fsnotify.Watcher
	mu         sync.RWMutex
	stopCh     chan struct{}
	callback   func(*T)
}

// NewFileSource creates a new file-based configuration source
func NewFileSource[T any](id, configPath string) *FileSource[T] {
	return &FileSource[T]{
		BaseSource: NewBaseSource[T](id),
		configPath: configPath,
	}
}

// Load reads and unmarshals configuration from file
func (f *FileSource[T]) Load(ctx context.Context) (*T, error) {
	rawConfig, err := f.getRawConfig(ctx)
	if err != nil {
		logger.Errorf("error loading raw config: %s", err)
		return nil, err
	}

	return f.unmarshalConfig(rawConfig)
}

// getRawConfig loads raw configuration from file
func (f *FileSource[T]) getRawConfig(ctx context.Context) (map[string]any, error) {
	if f.configPath == "" {
		logger.Warn("configuration file path is empty")
		return make(map[string]any), nil
	}

	// Check if file exists
	if _, err := os.Stat(f.configPath); os.IsNotExist(err) {
		logger.Warnf("configuration file does not exist, path: %s", f.configPath)
		return make(map[string]any), nil
	}

	// Read file content
	data, err := os.ReadFile(f.configPath)
	if err != nil {
		logger.Errorf("failed to read config file %s: %s", f.configPath, err)
		return nil, fmt.Errorf("failed to read config file %s: %w", f.configPath, err)
	}

	// Parse based on file extension
	ext := strings.ToLower(filepath.Ext(f.configPath))
	var config map[string]any

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			logger.Errorf("failed to parse YAML config %s: %s", f.configPath, err)
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &config); err != nil {
			logger.Errorf("failed to parse JSON config %s: %s", f.configPath, err)
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		logger.Warnf("不支持的配置文件格式", "extension", ext)
		return make(map[string]any), nil
	}

	if config == nil {
		config = make(map[string]any)
	}

	return config, nil
}

// Watch begins monitoring configuration file changes
func (f *FileSource[T]) Watch(ctx context.Context, callback func(*T)) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Return early if already watching
	if f.watcher != nil {
		return nil
	}

	if f.configPath == "" {
		logger.Error("invalid config path")
		return fmt.Errorf("invalid config path")
	}

	// Create file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorf("failed to create file watcher: %s", err)
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Watch the directory containing the config file
	configDir := filepath.Dir(f.configPath)
	if err := watcher.Add(configDir); err != nil {
		watcher.Close()
		logger.Errorf("failed to add directory to watcher: %s", err)
		return fmt.Errorf("failed to add directory to watcher: %w", err)
	}

	f.watcher = watcher
	f.callback = callback
	f.stopCh = make(chan struct{})

	// Start watching in a separate goroutine
	go f.watchLoop(ctx)

	logger.Debugf("started monitoring configuration file, path: %s", f.configPath)
	return nil
}

// Stop stops monitoring configuration file changes
func (f *FileSource[T]) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.watcher == nil {
		return nil
	}

	// Signal stop and close watcher
	close(f.stopCh)
	if err := f.watcher.Close(); err != nil {
		logger.Errorf("failed to close file watcher, error: %s", err)
	}

	f.watcher = nil
	f.callback = nil
	f.stopCh = nil

	logger.Debugf("stopped monitoring configuration file, path: %s", f.configPath)
	return nil
}

// watchLoop runs the file watching loop
func (f *FileSource[T]) watchLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("panic occurred in file watch loop: %v", r)
		}
	}()

	// Debounce timer to avoid multiple rapid fire events
	var debounceTimer *time.Timer
	const debounceDelay = 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stopCh:
			return
		case event, ok := <-f.watcher.Events:
			if !ok {
				return
			}

			// Check if the event is for our config file
			if event.Name != f.configPath {
				continue
			}

			// Only handle write/create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			logger.Infof("file change detected, file: %s, operation: %s", event.Name, event.Op.String())

			// Debounce the callback to avoid multiple rapid calls
			if debounceTimer != nil {
				debounceTimer.Stop()
			}

			debounceTimer = time.AfterFunc(debounceDelay, func() {
				if f.callback != nil {
					// Load new configuration and call callback
					newConfig, err := f.Load(ctx)
					if err != nil {
						logger.Errorf("failed to reload configuration: %s", err)
						return
					}
					f.callback(newConfig)
				}
			})

		case err, ok := <-f.watcher.Errors:
			if !ok {
				return
			}
			logger.Errorf("file watcher error: %s", err)
		}
	}
}
