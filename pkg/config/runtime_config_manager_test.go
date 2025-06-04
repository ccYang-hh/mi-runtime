package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Helper function to create temporary config files
func createTempConfigFile(t *testing.T, filename, content string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}

	return tmpFile
}

func TestManager_BasicOperations(t *testing.T) {
	// 创建测试配置文件
	configContent := `
app_name: "Test App"
app_version: "1.0.0"
host: "localhost"
port: 8080
service_discovery: "etcd"
cors_origins: ["*"]

plugin_registry:
  auto_discovery: true
  discovery_paths: []
  plugins: {}

pipelines: []

etcd:
  host: "127.0.0.1"
  port: 2379

max_batch_size: 128
request_timeout: 60
enable_monitor: true
monitor_config: ""
enable_router: false
enable_auth: false
auth_config: {}
`

	tmpFile := createTempConfigFile(t, "test_config.yaml", configContent)
	defer os.Remove(tmpFile)

	// 创建配置管理器
	manager, err := NewManager(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer manager.Close()

	// 测试获取配置
	config := manager.GetConfig()
	if config == nil {
		t.Fatal("Expected config to be non-nil")
	}

	if config.AppName != "Test App" {
		t.Errorf("Expected app_name 'Test App', got '%s'", config.AppName)
	}

	if config.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", config.Port)
	}
}

func TestManager_ConfigWatch(t *testing.T) {
	// 创建初始配置文件
	initialContent := `
app_name: "Initial App"
app_version: "1.0.0"
host: "localhost"
port: 8080
service_discovery: "etcd"
cors_origins: ["*"]

plugin_registry:
  auto_discovery: true
  discovery_paths: []
  plugins: {}

pipelines: []

etcd:
  host: "127.0.0.1"
  port: 2379

max_batch_size: 128
request_timeout: 60
enable_monitor: true
monitor_config: ""
enable_router: false
enable_auth: false
auth_config: {}
`

	tmpFile := createTempConfigFile(t, "watch_config.yaml", initialContent)
	defer os.Remove(tmpFile)

	// 创建配置管理器
	manager, err := NewManager(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer manager.Close()

	// 设置变更监听
	var wg sync.WaitGroup
	var receivedEvent *ConfigChangeEvent

	wg.Add(1)
	manager.Subscribe(func(event *ConfigChangeEvent) {
		receivedEvent = event
		wg.Done()
	})

	// 等待一点时间确保监听器启动
	time.Sleep(100 * time.Millisecond)

	// 更新配置文件
	updatedContent := `
app_name: "Updated App"
app_version: "2.0.0"
host: "localhost"
port: 9090
service_discovery: "etcd"
cors_origins: ["*"]

plugin_registry:
  auto_discovery: true
  discovery_paths: []
  plugins: {}

pipelines: []

etcd:
  host: "127.0.0.1"
  port: 2379

max_batch_size: 256
request_timeout: 60
enable_monitor: true
monitor_config: ""
enable_router: false
enable_auth: false
auth_config: {}
`

	if err := os.WriteFile(tmpFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// 等待变更事件
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 验证收到的事件
		if receivedEvent == nil {
			t.Fatal("Expected to receive config change event")
		}

		if receivedEvent.Config.AppName != "Updated App" {
			t.Errorf("Expected updated app_name 'Updated App', got '%s'", receivedEvent.Config.AppName)
		}

		if receivedEvent.Config.Port != 9090 {
			t.Errorf("Expected updated port 9090, got %d", receivedEvent.Config.Port)
		}

		if receivedEvent.Config.MaxBatchSize != 256 {
			t.Errorf("Expected updated max_batch_size 256, got %d", receivedEvent.Config.MaxBatchSize)
		}

	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for config change event")
	}
}

func TestManager_Validation(t *testing.T) {
	// 创建无效的配置文件
	invalidContent := `
app_name: ""
app_version: "invalid-version"
host: "localhost"
port: -1
service_discovery: "invalid"
cors_origins: ["*"]

plugin_registry:
  auto_discovery: true
  discovery_paths: []
  plugins: {}

pipelines: []

max_batch_size: 0
request_timeout: -1
enable_monitor: true
monitor_config: ""
enable_router: false
enable_auth: false
auth_config: {}
`

	tmpFile := createTempConfigFile(t, "invalid_config.yaml", invalidContent)
	defer os.Remove(tmpFile)

	// 尝试创建配置管理器，应该失败
	_, err := NewManager(tmpFile)
	if err == nil {
		t.Error("Expected manager creation to fail with invalid config")
	}
}

func TestManager_Singleton(t *testing.T) {
	configContent := `
app_name: "Singleton Test"
app_version: "1.0.0"
host: "localhost"
port: 8080
service_discovery: "etcd"
cors_origins: ["*"]

plugin_registry:
  auto_discovery: true
  discovery_paths: []
  plugins: {}

pipelines: []

etcd:
  host: "127.0.0.1"
  port: 2379

max_batch_size: 128
request_timeout: 60
enable_monitor: true
monitor_config: ""
enable_router: false
enable_auth: false
auth_config: {}
`

	tmpFile := createTempConfigFile(t, "singleton_config.yaml", configContent)
	defer os.Remove(tmpFile)

	// 重置单例以便测试
	managerOnce = sync.Once{}
	managerInstance = nil

	// 获取两次单例，应该是同一个实例
	manager1 := GetManager(tmpFile)
	manager2 := GetManager(tmpFile)

	if manager1 != manager2 {
		t.Error("Expected GetManager to return the same singleton instance")
	}

	defer manager1.Close()
}
