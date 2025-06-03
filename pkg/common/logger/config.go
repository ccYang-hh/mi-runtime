package logger

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Config 日志配置
type Config struct {
	LogLevel string        `mapstructure:"log_level" yaml:"log_level"`
	LogDir   string        `mapstructure:"log_dir" yaml:"log_dir"`
	Console  ConsoleConfig `mapstructure:"console" yaml:"console"`
	File     FileConfig    `mapstructure:"file" yaml:"file"`
}

// ConsoleConfig 控制台配置
type ConsoleConfig struct {
	Enable bool   `mapstructure:"enable" yaml:"enable"`
	Level  string `mapstructure:"level" yaml:"level"`
	Color  bool   `mapstructure:"color" yaml:"color"`
}

// FileConfig 文件配置
type FileConfig struct {
	Enable     bool   `mapstructure:"enable" yaml:"enable"`
	Level      string `mapstructure:"level" yaml:"level"`
	Filename   string `mapstructure:"filename" yaml:"filename"`
	MaxSize    int    `mapstructure:"max_size" yaml:"max_size"`       // MB
	MaxBackups int    `mapstructure:"max_backups" yaml:"max_backups"` // 文件数量
	MaxAge     int    `mapstructure:"max_age" yaml:"max_age"`         // 天数
	Compress   bool   `mapstructure:"compress" yaml:"compress"`
	Formatter  string `mapstructure:"formatter" yaml:"formatter"` // plain/json
}

// DefaultConfig 默认配置
var DefaultConfig = Config{
	LogLevel: "INFO",
	LogDir:   "./logs",
	Console: ConsoleConfig{
		Enable: true,
		Level:  "INFO",
		Color:  true,
	},
	File: FileConfig{
		Enable:     true,
		Level:      "INFO",
		Filename:   "app.log",
		MaxSize:    100, // 100MB
		MaxBackups: 7,   // 7个文件
		MaxAge:     30,  // 30天
		Compress:   true,
		Formatter:  "plain",
	},
}

// LoadConfig 加载配置
func LoadConfig(configPath ...string) (*Config, error) {
	config := DefaultConfig

	// 1. 加载文件配置
	if len(configPath) > 0 && configPath[0] != "" {
		if err := loadFileConfig(configPath[0], &config); err != nil {
			return nil, err
		}
	} else if envPath := os.Getenv("TMATRIX_LOG_CONFIG_PATH"); envPath != "" {
		if err := loadFileConfig(envPath, &config); err != nil {
			return nil, err
		}
	} else {
		// 尝试加载默认配置文件
		_ = loadFileConfig("logging_config.yaml", &config)
	}

	// 2. 应用环境变量覆盖
	applyEnvConfig(&config)

	// 3. 确保目录存在
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return nil, err
	}

	return &config, nil
}

// loadFileConfig 从文件加载配置
func loadFileConfig(path string, config *Config) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // 文件不存在不报错
	}

	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return err
	}

	return v.Unmarshal(config)
}

// applyEnvConfig 应用环境变量配置
func applyEnvConfig(config *Config) {
	if level := os.Getenv("TMATRIX_LOG_LEVEL"); level != "" {
		config.LogLevel = level
	}
	if dir := os.Getenv("TMATRIX_LOG_DIR"); dir != "" {
		config.LogDir = dir
	}

	// Console
	if enable := os.Getenv("TMATRIX_LOG_CONSOLE_ENABLE"); enable != "" {
		config.Console.Enable = parseBool(enable)
	}
	if level := os.Getenv("TMATRIX_LOG_CONSOLE_LEVEL"); level != "" {
		config.Console.Level = level
	}
	if color := os.Getenv("TMATRIX_LOG_CONSOLE_COLOR"); color != "" {
		config.Console.Color = parseBool(color)
	}

	// File
	if enable := os.Getenv("TMATRIX_LOG_FILE_ENABLE"); enable != "" {
		config.File.Enable = parseBool(enable)
	}
	if level := os.Getenv("TMATRIX_LOG_FILE_LEVEL"); level != "" {
		config.File.Level = level
	}
	if filename := os.Getenv("TMATRIX_LOG_FILE_FILENAME"); filename != "" {
		config.File.Filename = filename
	}
	if maxSize := os.Getenv("TMATRIX_LOG_FILE_MAX_SIZE"); maxSize != "" {
		if size, err := strconv.Atoi(maxSize); err == nil {
			config.File.MaxSize = size
		}
	}
	if maxBackups := os.Getenv("TMATRIX_LOG_FILE_MAX_BACKUPS"); maxBackups != "" {
		if backups, err := strconv.Atoi(maxBackups); err == nil {
			config.File.MaxBackups = backups
		}
	}
	if maxAge := os.Getenv("TMATRIX_LOG_FILE_MAX_AGE"); maxAge != "" {
		if age, err := strconv.Atoi(maxAge); err == nil {
			config.File.MaxAge = age
		}
	}
	if compress := os.Getenv("TMATRIX_LOG_FILE_COMPRESS"); compress != "" {
		config.File.Compress = parseBool(compress)
	}
	if formatter := os.Getenv("TMATRIX_LOG_FILE_FORMATTER"); formatter != "" {
		config.File.Formatter = formatter
	}
}

// parseBool 解析布尔值
func parseBool(s string) bool {
	lower := strings.ToLower(s)
	return lower == "true" || lower == "1"
}
