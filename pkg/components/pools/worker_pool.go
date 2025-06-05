package pools

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// Task 任务接口
type Task func()

// TaskWithResult 带结果的任务接口
type TaskWithResult func() (interface{}, error)

// WorkerPoolStats 协程池统计信息
type WorkerPoolStats struct {
	Workers        int32 `json:"workers"`
	QueueLength    int   `json:"queue_length"`
	TotalTasks     int64 `json:"total_tasks"`
	ActiveTasks    int64 `json:"active_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks    int64 `json:"failed_tasks"`

	// 性能指标
	AvgTaskDuration   time.Duration `json:"avg_task_duration"`
	MaxTaskDuration   time.Duration `json:"max_task_duration"`
	TaskThroughputMin float64       `json:"task_throughput_min"`

	// 资源使用
	QueueUtilization  float64 `json:"queue_utilization"`
	WorkerUtilization float64 `json:"worker_utilization"`
}

// WorkerPool 通用协程池
type WorkerPool struct {
	name       string
	minWorkers int32
	maxWorkers int32
	queueSize  int32

	// 工作队列
	taskQueue chan Task

	// 协程管理
	workers    int32
	activeTask int64
	wg         sync.WaitGroup

	// 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc

	// 统计信息
	stats   WorkerPoolStats
	statsMu sync.RWMutex

	// 任务时长统计
	taskDurations []time.Duration
	taskDurMu     sync.Mutex

	// 监控
	monitorInterval time.Duration
	lastTaskCount   int64
	lastCheckTime   time.Time
}

// WorkerPoolConfig 协程池配置
type WorkerPoolConfig struct {
	Name            string        `json:"name"`
	MinWorkers      int           `json:"min_workers"`
	MaxWorkers      int           `json:"max_workers"`
	QueueSize       int           `json:"queue_size"`
	MonitorInterval time.Duration `json:"monitor_interval"`
}

// NewWorkerPool 创建通用协程池
func NewWorkerPool(rootCtx context.Context, config WorkerPoolConfig) *WorkerPool {
	if config.MinWorkers <= 0 {
		config.MinWorkers = runtime.NumCPU()
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = runtime.NumCPU() * 4
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 1000
	}
	if config.MonitorInterval <= 0 {
		config.MonitorInterval = 5 * time.Second
	}
	if config.Name == "" {
		config.Name = "default"
	}

	ctx, cancel := context.WithCancel(rootCtx)

	pool := &WorkerPool{
		name:            config.Name,
		minWorkers:      int32(config.MinWorkers),
		maxWorkers:      int32(config.MaxWorkers),
		queueSize:       int32(config.QueueSize),
		taskQueue:       make(chan Task, config.QueueSize),
		ctx:             ctx,
		cancel:          cancel,
		monitorInterval: config.MonitorInterval,
		taskDurations:   make([]time.Duration, 0, 1000),
		lastCheckTime:   time.Now(),
	}

	// 启动初始协程
	for i := 0; i < config.MinWorkers; i++ {
		pool.startWorker()
	}

	// 启动监控协程
	go pool.monitor()

	logger.Infof("worker pool '%s' started: min=%d, max=%d, queue=%d",
		config.Name, config.MinWorkers, config.MaxWorkers, config.QueueSize)

	return pool
}

// Submit 提交任务
func (wp *WorkerPool) Submit(task Task) bool {
	if task == nil {
		return false
	}

	atomic.AddInt64(&wp.stats.TotalTasks, 1)

	select {
	case wp.taskQueue <- task:
		return true
	case <-wp.ctx.Done():
		return false
	default:
		// 队列满了，尝试创建新的worker
		if wp.tryCreateWorker() {
			select {
			case wp.taskQueue <- task:
				return true
			default:
				atomic.AddInt64(&wp.stats.FailedTasks, 1)
				return false
			}
		}
		atomic.AddInt64(&wp.stats.FailedTasks, 1)
		return false
	}
}

// SubmitWithTimeout 带超时的任务提交
func (wp *WorkerPool) SubmitWithTimeout(task Task, timeout time.Duration) bool {
	if task == nil {
		return false
	}

	atomic.AddInt64(&wp.stats.TotalTasks, 1)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case wp.taskQueue <- task:
		return true
	case <-timer.C:
		atomic.AddInt64(&wp.stats.FailedTasks, 1)
		return false
	case <-wp.ctx.Done():
		atomic.AddInt64(&wp.stats.FailedTasks, 1)
		return false
	}
}

// startWorker 启动一个worker
func (wp *WorkerPool) startWorker() {
	if atomic.LoadInt32(&wp.workers) >= wp.maxWorkers {
		return
	}

	atomic.AddInt32(&wp.workers, 1)
	wp.wg.Add(1)

	go func() {
		defer func() {
			atomic.AddInt32(&wp.workers, -1)
			wp.wg.Done()

			if r := recover(); r != nil {
				logger.Errorf("worker panic in pool '%s': %v", wp.name, r)
				atomic.AddInt64(&wp.stats.FailedTasks, 1)

				// 重新启动一个worker来替代
				if atomic.LoadInt32(&wp.workers) < wp.minWorkers {
					wp.startWorker()
				}
			}
		}()

		idleTimer := time.NewTimer(30 * time.Second)
		defer idleTimer.Stop()

		for {
			select {
			case task := <-wp.taskQueue:
				if task != nil {
					wp.executeTask(task)

					// 重置空闲计时器
					if !idleTimer.Stop() {
						<-idleTimer.C
					}
					idleTimer.Reset(30 * time.Second)
				}

			case <-idleTimer.C:
				// 空闲超时，如果worker数量超过最小值，则退出
				if atomic.LoadInt32(&wp.workers) > wp.minWorkers {
					return
				}
				idleTimer.Reset(30 * time.Second)

			case <-wp.ctx.Done():
				return
			}
		}
	}()
}

// executeTask 执行任务
func (wp *WorkerPool) executeTask(task Task) {
	atomic.AddInt64(&wp.activeTask, 1)
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		atomic.AddInt64(&wp.activeTask, -1)
		atomic.AddInt64(&wp.stats.CompletedTasks, 1)

		// 记录任务时长
		wp.recordTaskDuration(duration)
	}()

	task()
}

// recordTaskDuration 记录任务时长
func (wp *WorkerPool) recordTaskDuration(duration time.Duration) {
	wp.taskDurMu.Lock()
	defer wp.taskDurMu.Unlock()

	wp.taskDurations = append(wp.taskDurations, duration)

	// 保持最近1000个任务的记录
	if len(wp.taskDurations) > 1000 {
		wp.taskDurations = wp.taskDurations[len(wp.taskDurations)-1000:]
	}
}

// tryCreateWorker 尝试创建新的worker
func (wp *WorkerPool) tryCreateWorker() bool {
	currentWorkers := atomic.LoadInt32(&wp.workers)
	if currentWorkers < wp.maxWorkers {
		// 检查队列压力
		queueLen := len(wp.taskQueue)
		if queueLen > int(currentWorkers)*2 {
			wp.startWorker()
			return true
		}
	}
	return false
}

// monitor 监控协程池状态
func (wp *WorkerPool) monitor() {
	ticker := time.NewTicker(wp.monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			wp.updateStats()
		case <-wp.ctx.Done():
			return
		}
	}
}

// updateStats 更新统计信息
func (wp *WorkerPool) updateStats() {
	wp.statsMu.Lock()
	defer wp.statsMu.Unlock()

	now := time.Now()

	// 基础统计
	wp.stats.Workers = atomic.LoadInt32(&wp.workers)
	wp.stats.QueueLength = len(wp.taskQueue)
	wp.stats.TotalTasks = atomic.LoadInt64(&wp.stats.TotalTasks)
	wp.stats.ActiveTasks = atomic.LoadInt64(&wp.activeTask)
	wp.stats.CompletedTasks = atomic.LoadInt64(&wp.stats.CompletedTasks)
	wp.stats.FailedTasks = atomic.LoadInt64(&wp.stats.FailedTasks)

	// 计算吞吐量
	if !wp.lastCheckTime.IsZero() {
		duration := now.Sub(wp.lastCheckTime).Minutes()
		if duration > 0 {
			tasksSinceLastCheck := wp.stats.CompletedTasks - wp.lastTaskCount
			wp.stats.TaskThroughputMin = float64(tasksSinceLastCheck) / duration
		}
	}
	wp.lastTaskCount = wp.stats.CompletedTasks
	wp.lastCheckTime = now

	// 计算任务时长统计
	wp.calculateTaskDurationStats()

	// 计算利用率
	wp.stats.QueueUtilization = float64(wp.stats.QueueLength) / float64(wp.queueSize) * 100
	wp.stats.WorkerUtilization = float64(wp.stats.ActiveTasks) / float64(wp.stats.Workers) * 100

	logger.Debugf("pool '%s' stats: workers=%d, queue=%d/%d, total=%d, active=%d, completed=%d, failed=%d, throughput=%.2f/min",
		wp.name, wp.stats.Workers, wp.stats.QueueLength, wp.queueSize,
		wp.stats.TotalTasks, wp.stats.ActiveTasks, wp.stats.CompletedTasks, wp.stats.FailedTasks,
		wp.stats.TaskThroughputMin)
}

// calculateTaskDurationStats 计算任务时长统计
func (wp *WorkerPool) calculateTaskDurationStats() {
	wp.taskDurMu.Lock()
	defer wp.taskDurMu.Unlock()

	if len(wp.taskDurations) == 0 {
		return
	}

	var total time.Duration
	var maxDuration time.Duration

	for _, duration := range wp.taskDurations {
		total += duration
		if duration > maxDuration {
			maxDuration = duration
		}
	}

	wp.stats.AvgTaskDuration = total / time.Duration(len(wp.taskDurations))
	wp.stats.MaxTaskDuration = maxDuration
}

// GetStats 获取统计信息
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	wp.statsMu.RLock()
	defer wp.statsMu.RUnlock()

	return wp.stats
}

// Close 关闭协程池
func (wp *WorkerPool) Close() {
	logger.Infof("closing worker pool '%s'", wp.name)

	wp.cancel()
	close(wp.taskQueue)
	wp.wg.Wait()

	logger.Infof("worker pool '%s' closed", wp.name)
}

// Resize 动态调整协程池大小
func (wp *WorkerPool) Resize(minWorkers, maxWorkers int) {
	if minWorkers > 0 {
		atomic.StoreInt32(&wp.minWorkers, int32(minWorkers))
	}
	if maxWorkers > 0 {
		atomic.StoreInt32(&wp.maxWorkers, int32(maxWorkers))
	}

	logger.Infof("pool '%s' resized: min=%d, max=%d", wp.name, wp.minWorkers, wp.maxWorkers)
}
