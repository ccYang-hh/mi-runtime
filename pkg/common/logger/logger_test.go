package logger

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// resetGlobalLogger 重置全局logger（测试辅助函数）
func resetGlobalLogger() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
	globalLogger = nil
	sugar = nil
	once = sync.Once{}
}

func TestLoggerBasicFunctionality(t *testing.T) {
	// 重置全局logger
	resetGlobalLogger()

	// 初始化logger
	err := Init("./logger_config.yaml")
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}

	// 测试各种日志级别
	Debug("Debug message", zap.String("level", "debug"))
	Info("Info message", zap.String("level", "info"))
	Warn("Warning message", zap.String("level", "warn"))
	Error("Error message", zap.String("level", "error"))

	// 测试格式化日志
	Debugf("Debug formatted: %s", "debug")
	Infof("Info formatted: %s", "info")
	Warnf("Warning formatted: %s", "warning")
	Errorf("Error formatted: %s", "error")

	// 同步确保写入
	Sync()

	// 验证日志文件是否创建
	if _, err := os.Stat("./logs/app.log"); os.IsNotExist(err) {
		t.Error("Log file was not created")
	}
}

func TestColorOutput(t *testing.T) {
	// 重置全局logger
	resetGlobalLogger()

	// 强制启用颜色输出进行测试
	os.Setenv("TMATRIX_LOG_CONSOLE_COLOR", "true")
	defer os.Unsetenv("TMATRIX_LOG_CONSOLE_COLOR")

	err := Init()
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}

	t.Log("=== 颜色输出测试 ===")
	t.Log("注意：如果在GoLand中看不到颜色，请在真实终端中运行此测试")

	Debug("这是调试信息 - 应该是白色")
	Info("这是普通信息 - 应该是绿色")
	Warn("这是警告信息 - 应该是黄色")
	Error("这是错误信息 - 应该是红色")
	// Fatal("这是严重错误信息 - 应该是深红色")

	Sync()
}

func TestEnvironmentVariableOverride(t *testing.T) {
	// 重置全局logger
	resetGlobalLogger()

	// 设置测试目录
	testDir := "./test_env_logs"
	defer os.RemoveAll(testDir)

	// 保存原始环境变量
	originalEnvs := map[string]string{
		"TMATRIX_LOG_LEVEL":          os.Getenv("TMATRIX_LOG_LEVEL"),
		"TMATRIX_LOG_DIR":            os.Getenv("TMATRIX_LOG_DIR"),
		"TMATRIX_LOG_CONSOLE_ENABLE": os.Getenv("TMATRIX_LOG_CONSOLE_ENABLE"),
		"TMATRIX_LOG_CONSOLE_COLOR":  os.Getenv("TMATRIX_LOG_CONSOLE_COLOR"),
		"TMATRIX_LOG_FILE_ENABLE":    os.Getenv("TMATRIX_LOG_FILE_ENABLE"),
		"TMATRIX_LOG_FILE_LEVEL":     os.Getenv("TMATRIX_LOG_FILE_LEVEL"),
		"TMATRIX_LOG_FILE_FILENAME":  os.Getenv("TMATRIX_LOG_FILE_FILENAME"),
		"TMATRIX_LOG_FILE_FORMATTER": os.Getenv("TMATRIX_LOG_FILE_FORMATTER"),
	}

	// 在测试结束后恢复环境变量
	defer func() {
		for key, value := range originalEnvs {
			if value == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, value)
			}
		}
	}()

	// 设置测试环境变量
	testEnvs := map[string]string{
		"TMATRIX_LOG_LEVEL":          "WARN",
		"TMATRIX_LOG_DIR":            testDir,
		"TMATRIX_LOG_CONSOLE_ENABLE": "true",
		"TMATRIX_LOG_CONSOLE_COLOR":  "false",
		"TMATRIX_LOG_FILE_ENABLE":    "true",
		"TMATRIX_LOG_FILE_LEVEL":     "INFO",
		"TMATRIX_LOG_FILE_FILENAME":  "env_test.log",
		"TMATRIX_LOG_FILE_FORMATTER": "plain",
	}

	for key, value := range testEnvs {
		os.Setenv(key, value)
	}

	// 加载配置
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证环境变量覆盖生效
	if config.LogLevel != "WARN" {
		t.Errorf("Expected log level WARN, got %s", config.LogLevel)
	}

	if config.LogDir != testDir {
		t.Errorf("Expected log dir %s, got %s", testDir, config.LogDir)
	}

	if config.Console.Color != false {
		t.Errorf("Expected console color false, got %v", config.Console.Color)
	}

	if config.File.Level != "INFO" {
		t.Errorf("Expected file level INFO, got %s", config.File.Level)
	}

	if config.File.Filename != "env_test.log" {
		t.Errorf("Expected filename env_test.log, got %s", config.File.Filename)
	}

	if config.File.Formatter != "plain" {
		t.Errorf("Expected formatter plain, got %s", config.File.Formatter)
	}

	// 初始化logger并测试
	err = Init()
	if err != nil {
		t.Fatalf("Failed to initialize logger with env config: %v", err)
	}

	t.Log("=== 环境变量覆盖测试 ===")

	// 测试日志输出
	Debug("This should not appear in console")                   // 不会在控制台显示
	Info("This appears in file only", zap.String("test", "env")) // 只在文件中
	Warn("This appears everywhere", zap.String("test", "env"))   // 控制台和文件都有
	Error("This is an error", zap.String("test", "env"))         // 控制台和文件都有

	Sync()

	// 验证日志文件
	logFile := filepath.Join(testDir, "env_test.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Environment configured log file was not created")
	}

	// 读取并验证文件内容
	content, err := ioutil.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)
	t.Logf("Log file content:\n%s", contentStr)

	// 验证应该包含INFO, WARN, ERROR但不包含DEBUG
	if strings.Contains(contentStr, "This should not appear") {
		t.Error("DEBUG message should not appear in file when level is INFO")
	}

	if !strings.Contains(contentStr, "This appears in file only") {
		t.Error("INFO message should appear in file")
	}

	if !strings.Contains(contentStr, "This appears everywhere") {
		t.Error("WARN message should appear in file")
	}
}

func BenchmarkLogger(b *testing.B) {
	// 重置全局logger
	resetGlobalLogger()

	// 初始化Logger
	_ = Init()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			Info("Benchmark message",
				zap.String("key", "value"),
				zap.Int("number", 42),
				zap.Duration("duration", time.Millisecond),
			)
		}
	})
}
