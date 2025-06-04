package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ANSI颜色码定义
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"   // ERROR
	ColorYellow  = "\033[33m"   // WARN
	ColorGreen   = "\033[32m"   // INFO
	ColorWhite   = "\033[37m"   // DEBUG
	ColorBoldRed = "\033[1;31m" // FATAL/PANIC
)

// 日志级别颜色映射
var levelColorMap = map[zapcore.Level]string{
	zapcore.DebugLevel:  ColorWhite,
	zapcore.InfoLevel:   ColorGreen,
	zapcore.WarnLevel:   ColorYellow,
	zapcore.ErrorLevel:  ColorRed,
	zapcore.DPanicLevel: ColorBoldRed,
	zapcore.PanicLevel:  ColorBoldRed,
	zapcore.FatalLevel:  ColorBoldRed,
}

var (
	globalLogger *zap.Logger
	sugar        *zap.SugaredLogger
	once         sync.Once
)

// Init 初始化全局日志器
func Init(configPath ...string) error {
	var err error
	once.Do(func() {
		err = initLogger(configPath...)
	})
	return err
}

// initLogger 内部初始化函数
func initLogger(configPath ...string) error {
	config, err := LoadConfig(configPath...)
	if err != nil {
		return err
	}

	cores := make([]zapcore.Core, 0, 2)

	// 控制台核心
	if config.Console.Enable {
		consoleCore := createConsoleCore(config)
		cores = append(cores, consoleCore)
	}

	// 文件核心
	if config.File.Enable {
		fileCore := createFileCore(config)
		cores = append(cores, fileCore)
	}

	if len(cores) == 0 {
		return fmt.Errorf("no logger cores enabled")
	}

	// 创建logger
	core := zapcore.NewTee(cores...)

	// 配置选项
	options := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1), // 跳过wrapper层
	}

	// ERROR及以上级别添加堆栈追踪
	globalLevel := parseLogLevel(config.LogLevel)
	if globalLevel <= zapcore.ErrorLevel {
		options = append(options, zap.AddStacktrace(zapcore.ErrorLevel))
	}

	globalLogger = zap.New(core, options...)
	sugar = globalLogger.Sugar()

	return nil
}

// createConsoleCore 创建控制台核心
func createConsoleCore(config *Config) zapcore.Core {
	level := parseLogLevel(config.Console.Level)

	// 控制台编码器配置 - 实现目标格式
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:          "T",
		LevelKey:         "L",
		NameKey:          "N",
		CallerKey:        "C",
		FunctionKey:      zapcore.OmitKey,
		MessageKey:       "M",
		StacktraceKey:    "S",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      getConsoleLevelEncoder(config.Console.Color),
		EncodeTime:       consoleTimeEncoder,
		EncodeDuration:   zapcore.StringDurationEncoder,
		EncodeCaller:     consoleCallerEncoder,
		ConsoleSeparator: " ", // 使用空格分隔
	}

	encoder := zapcore.NewConsoleEncoder(encoderConfig)
	writer := zapcore.AddSync(os.Stdout)

	return zapcore.NewCore(encoder, writer, level)
}

// createFileCore 创建文件核心
func createFileCore(config *Config) zapcore.Core {
	level := parseLogLevel(config.File.Level)

	// 文件轮转配置
	logPath := filepath.Join(config.LogDir, config.File.Filename)
	lumberjackLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    config.File.MaxSize,
		MaxBackups: config.File.MaxBackups,
		MaxAge:     config.File.MaxAge,
		Compress:   config.File.Compress,
	}

	writer := zapcore.AddSync(lumberjackLogger)

	// 选择编码器
	var encoder zapcore.Encoder
	if config.File.Formatter == "json" {
		// 标准JSON格式
		encoderConfig := zapcore.EncoderConfig{
			TimeKey:        "timestamp",
			LevelKey:       "level",
			MessageKey:     "message",
			CallerKey:      "caller",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"),
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		// Plain文本格式，与控制台相同但无颜色
		encoderConfig := zapcore.EncoderConfig{
			TimeKey:          "T",
			LevelKey:         "L",
			NameKey:          "N",
			CallerKey:        "C",
			FunctionKey:      zapcore.OmitKey,
			MessageKey:       "M",
			StacktraceKey:    "S",
			LineEnding:       zapcore.DefaultLineEnding,
			EncodeLevel:      plainLevelEncoder,
			EncodeTime:       plainTimeEncoder,
			EncodeDuration:   zapcore.StringDurationEncoder,
			EncodeCaller:     plainCallerEncoder,
			ConsoleSeparator: " ",
		}
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	return zapcore.NewCore(encoder, writer, level)
}

// 控制台时间编码器 - 格式: [2025-06-04 00:02:07]
func consoleTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + t.Format("2006-01-02 15:04:05") + "]")
}

// Plain时间编码器 - 格式: [2025-06-04 00:02:07]
func plainTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + t.Format("2006-01-02 15:04:05") + "]")
}

// 控制台调用者编码器 - 格式: (filename:line)
func consoleCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	if !caller.Defined {
		enc.AppendString("")
		return
	}
	enc.AppendString("(" + filepath.Base(caller.File) + ":" +
		fmt.Sprintf("%d", caller.Line) + ")")
}

// Plain调用者编码器 - 格式: (filename:line)
func plainCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	if !caller.Defined {
		enc.AppendString("")
		return
	}
	enc.AppendString("(" + filepath.Base(caller.File) + ":" +
		fmt.Sprintf("%d", caller.Line) + ")")
}

// 获取控制台级别编码器
func getConsoleLevelEncoder(enableColor bool) zapcore.LevelEncoder {
	if enableColor {
		return consoleColorLevelEncoder
	}
	return plainConsoleLevelEncoder
}

// fileColorLevelEncoder 文件专用带颜色的级别编码器
func fileColorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if colorCode, exists := levelColorMap[level]; exists {
		// 文件中使用完整的ANSI转义序列
		coloredLevel := colorCode + level.CapitalString() + ColorReset
		enc.AppendString(coloredLevel)
	} else {
		enc.AppendString(level.CapitalString())
	}
}

// consoleColorLevelEncoder 控制台专用带颜色的级别编码器
func consoleColorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if colorCode, exists := levelColorMap[level]; exists {
		// 控制台使用标准ANSI码
		coloredLevel := colorCode + level.CapitalString() + ColorReset
		enc.AppendString(coloredLevel)
	} else {
		enc.AppendString(level.CapitalString())
	}
}

// 无颜色控制台级别编码器
func plainConsoleLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(level.CapitalString())
}

// Plain级别编码器
func plainLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(level.CapitalString())
}

// parseLogLevel 解析日志级别
func parseLogLevel(level string) zapcore.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return zapcore.DebugLevel
	case "INFO":
		return zapcore.InfoLevel
	case "WARN", "WARNING":
		return zapcore.WarnLevel
	case "ERROR":
		return zapcore.ErrorLevel
	case "FATAL":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// Sync 同步日志缓冲区
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

func Debug(msg string, fields ...zap.Field) {
	ensureLogger()
	globalLogger.Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	ensureLogger()
	globalLogger.Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	ensureLogger()
	globalLogger.Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	ensureLogger()
	globalLogger.Error(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	ensureLogger()
	globalLogger.Fatal(msg, fields...)
}

func Debugf(template string, args ...interface{}) {
	ensureLogger()
	sugar.Debugf(template, args...)
}

func Infof(template string, args ...interface{}) {
	ensureLogger()
	sugar.Infof(template, args...)
}

func Warnf(template string, args ...interface{}) {
	ensureLogger()
	sugar.Warnf(template, args...)
}

func Errorf(template string, args ...interface{}) {
	ensureLogger()
	sugar.Errorf(template, args...)
}

func Fatalf(template string, args ...interface{}) {
	ensureLogger()
	sugar.Fatalf(template, args...)
}

// ensureLogger 确保logger已初始化
func ensureLogger() {
	if globalLogger == nil {
		_ = Init() // 使用默认配置初始化
	}
}
