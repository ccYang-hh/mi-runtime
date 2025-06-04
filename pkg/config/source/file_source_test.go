package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConfig represents a test configuration structure
type TestConfig struct {
	Name    string   `mapstructure:"name"`
	Port    int      `mapstructure:"port"`
	Enabled bool     `mapstructure:"enabled"`
	Tags    []string `mapstructure:"tags"`
}

func TestFileSource_Load_JSON(t *testing.T) {
	// Create temporary JSON config file
	configContent := `{
		"name": "test-app",
		"port": 8080,
		"enabled": true,
		"tags": ["web", "api"]
	}`

	tmpFile := createTempConfigFile(t, "config.json", configContent)
	defer os.Remove(tmpFile)

	// Create file source
	source := NewFileSource[TestConfig]("test", tmpFile)

	// Load configuration
	config, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify configuration
	expected := &TestConfig{
		Name:    "test-app",
		Port:    8080,
		Enabled: true,
		Tags:    []string{"web", "api"},
	}

	if config.Name != expected.Name {
		t.Errorf("Expected name %s, got %s", expected.Name, config.Name)
	}
	if config.Port != expected.Port {
		t.Errorf("Expected port %d, got %d", expected.Port, config.Port)
	}
	if config.Enabled != expected.Enabled {
		t.Errorf("Expected enabled %t, got %t", expected.Enabled, config.Enabled)
	}
	if len(config.Tags) != len(expected.Tags) {
		t.Errorf("Expected tags length %d, got %d", len(expected.Tags), len(config.Tags))
	}
}

func TestFileSource_Load_YAML(t *testing.T) {
	// Create temporary YAML config file
	configContent := `
name: test-app
port: 8080
enabled: true
tags:
  - web
  - api
`

	tmpFile := createTempConfigFile(t, "config.yaml", configContent)
	defer os.Remove(tmpFile)

	// Create file source
	source := NewFileSource[TestConfig]("test", tmpFile)

	// Load configuration
	config, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify configuration
	if config.Name != "test-app" {
		t.Errorf("Expected name test-app, got %s", config.Name)
	}
	if config.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", config.Port)
	}
}

func TestFileSource_Watch(t *testing.T) {
	// Create temporary config file
	initialContent := `{"name": "initial", "port": 8080}`
	tmpFile := createTempConfigFile(t, "config.json", initialContent)
	defer os.Remove(tmpFile)

	// Create file source
	source := NewFileSource[TestConfig]("test", tmpFile)
	defer source.Stop()

	// Channel to receive config updates
	configUpdates := make(chan *TestConfig, 1)

	// Start watching
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := source.Watch(ctx, func(config *TestConfig) {
		configUpdates <- config
	})
	if err != nil {
		t.Fatalf("Failed to start watching: %v", err)
	}

	// Wait a bit for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Update config file
	updatedContent := `{"name": "updated", "port": 9090}`
	if err := os.WriteFile(tmpFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// Wait for config update
	select {
	case config := <-configUpdates:
		if config.Name != "updated" {
			t.Errorf("Expected updated name, got %s", config.Name)
		}
		if config.Port != 9090 {
			t.Errorf("Expected updated port 9090, got %d", config.Port)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for config update")
	}
}

// Helper function to create temporary config files
func createTempConfigFile(t *testing.T, filename, content string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	return tmpFile
}
