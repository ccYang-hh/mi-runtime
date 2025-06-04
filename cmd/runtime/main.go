package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	"xfusion.com/tmatrix/runtime/pkg/core"
)

// Args 命令行参数结构
type Args struct {
	Config string
	//Host     string
	//Port     int
	//LogLevel string
}

// parseArgs 解析命令行参数
func parseArgs() *Args {
	args := &Args{}

	flag.StringVar(&args.Config, "config", "config.yaml", "Configuration file path")
	//flag.StringVar(&args.Host, "host", "", "Host to bind the server to")
	//flag.IntVar(&args.Port, "port", 0, "Port to bind the server to")
	//flag.StringVar(&args.LogLevel, "log-level", "", "Logging level (debug, info, warning, error, critical)")

	flag.Parse()
	return args
}

func main() {
	args := parseArgs()

	// 初始化日志
	if err := logger.Init("./logger_config.yaml"); err != nil {
		fmt.Printf("failed to initialize logger: %+v\n", err)
		os.Exit(1)
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建运行时核心
	runtime, err := core.NewRuntimeCore(ctx, args.Config)
	if err != nil {
		logger.Errorf("failed to create runtime core: %+v", err)
		os.Exit(1)
	}

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动运行时
	if err := runtime.Start(); err != nil {
		logger.Errorf("failed to start runtime: %+v", err)
		os.Exit(1)
	}

	logger.Infof("runtime started successfully")

	// 等待退出信号
	sig := <-sigChan
	logger.Infof("received signal %s, shutting down...", sig)

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := runtime.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("failed to shutdown runtime: %+v", err)
		os.Exit(1)
	}

	logger.Infof("runtime shutdown completed")
}
