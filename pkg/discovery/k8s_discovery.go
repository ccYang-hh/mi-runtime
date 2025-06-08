package discovery

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// K8SServiceDiscovery 基于Kubernetes的服务发现实现
type K8SServiceDiscovery struct {
	*CachedServiceDiscovery                       // 嵌入缓存服务发现
	clientset               *kubernetes.Clientset // Kubernetes客户端
	namespace               string                // 命名空间
	port                    int                   // 服务端口
	labelSelector           string                // 标签选择器
	enableModelProbe        bool                  // 是否启用模型探测
	watchContext            context.Context       // 监听上下文
	watchCancel             context.CancelFunc    // 监听取消函数
	watcherRunning          bool                  // 监听器运行状态
	watcherMu               sync.RWMutex          // 监听器状态锁
	closed                  bool                  // 关闭状态
	closeMu                 sync.RWMutex          // 关闭状态锁
}

// NewK8SServiceDiscovery 创建Kubernetes服务发现实例
// TODO 基于K8S的服务发现机制，功能开发中
func NewK8SServiceDiscovery(namespace string, port int, labelSelector string,
	cacheTTL time.Duration, enableModelProbe bool) (*K8SServiceDiscovery, error) {
	// 初始化Kubernetes客户端
	clientset, err := createK8SClient()
	if err != nil {
		logger.Errorf("failed to create kubernetes client: %v", err)
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// 创建监听上下文
	watchCtx, watchCancel := context.WithCancel(context.Background())

	k := &K8SServiceDiscovery{
		clientset:        clientset,
		namespace:        namespace,
		port:             port,
		labelSelector:    labelSelector,
		enableModelProbe: enableModelProbe,
		watchContext:     watchCtx,
		watchCancel:      watchCancel,
		watcherRunning:   true,
	}

	// 创建缓存服务发现
	k.CachedServiceDiscovery = NewCachedServiceDiscovery(cacheTTL, k.fetchK8SEndpoints)

	// 启动Pod监听
	go k.watchPods()

	logger.Infof("helm service discovery initialized for namespace: %s, port: %d", namespace, port)
	return k, nil
}

// createK8SClient 创建Kubernetes客户端
func createK8SClient() (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	// 尝试使用集群内配置
	config, err = rest.InClusterConfig()
	if err != nil {
		// 回退到本地配置
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to build config: %w", err)
		}
	}

	// 创建客户端
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return clientset, nil
}

// fetchK8SEndpoints 从Kubernetes获取端点
func (k *K8SServiceDiscovery) fetchK8SEndpoints(ctx context.Context) ([]*Endpoint, error) {
	// 列出符合条件的Pod
	listOptions := metav1.ListOptions{
		LabelSelector: k.labelSelector,
	}

	podList, err := k.clientset.CoreV1().Pods(k.namespace).List(ctx, listOptions)
	if err != nil {
		logger.Errorf("failed to list pods: %v", err)
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var endpoints []*Endpoint
	for _, pod := range podList.Items {
		if !k.isPodReady(&pod) {
			continue
		}

		endpoint := k.podToEndpoint(&pod)
		if endpoint != nil {
			endpoints = append(endpoints, endpoint)
		}
	}

	logger.Infof("fetched %d endpoints from kubernetes", len(endpoints))
	return endpoints, nil
}

// isPodReady 检查Pod是否就绪
func (k *K8SServiceDiscovery) isPodReady(pod *v1.Pod) bool {
	// 检查Pod阶段
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	// 检查Pod IP
	if pod.Status.PodIP == "" {
		return false
	}

	// 检查容器就绪状态
	if pod.Status.ContainerStatuses == nil {
		return false
	}

	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	return true
}

// podToEndpoint 将Pod转换为Endpoint
func (k *K8SServiceDiscovery) podToEndpoint(pod *v1.Pod) *Endpoint {
	labels := pod.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	// 提取模型名称
	modelName := labels[string(PodServiceLabelModelName)]
	if modelName == "" {
		modelName = "default"
	}

	// 提取传输类型
	transportTypeStr := labels[string(PodServiceLabelServiceTransportType)]
	if transportTypeStr == "" {
		transportTypeStr = "http"
	}

	var transportType TransportType
	switch strings.ToLower(transportTypeStr) {
	case "tcp":
		transportType = TransportTypeTCP
	case "https":
		transportType = TransportTypeHTTPS
	default:
		transportType = TransportTypeHTTP
	}

	// 提取优先级
	priorityStr := labels[string(PodServiceLabelServicePriority)]
	priority := 0
	if priorityStr != "" {
		if p, err := strconv.Atoi(priorityStr); err == nil {
			priority = p
		}
	}

	// 构建地址
	address := fmt.Sprintf("%s:%d", pod.Status.PodIP, k.port)

	// 创建端点
	endpoint := &Endpoint{
		EndpointID:     pod.Name,
		Address:        address,
		ModelName:      modelName,
		Priority:       priority,
		KVRole:         KVRoleBoth,
		InstanceName:   pod.Name,
		TransportType:  transportType,
		Status:         EndpointStatusHealthy,
		EndpointTypes:  []EndpointType{EndpointTypeChatCompletion},
		KVEventConfig:  DefaultKVEventsConfig(),
		AddedTimestamp: float64(time.Now().Unix()),
		Metadata:       convertLabelsToMetadata(labels),
	}

	// 模型探测（如果启用）
	if k.enableModelProbe {
		logger.Warnf("model probe is not implemented yet in K8SServiceDiscovery")
		// TODO: 实现模型探测逻辑
	}

	return endpoint
}

// convertLabelsToMetadata 将Kubernetes标签转换为元数据
func convertLabelsToMetadata(labels map[string]string) map[string]interface{} {
	metadata := make(map[string]interface{})
	for k, v := range labels {
		metadata[k] = v
	}
	return metadata
}

// watchPods 监听Pod变化
func (k *K8SServiceDiscovery) watchPods() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("helm watch goroutine panicked: %v", r)
		}
	}()

	for {
		select {
		case <-k.watchContext.Done():
			logger.Infof("helm pod watch stopped")
			return
		default:
			if err := k.doWatch(); err != nil {
				logger.Errorf("helm watch error: %v", err)
				time.Sleep(5 * time.Second) // 出错后等待重试
			}
		}
	}
}

// doWatch 执行实际的监听操作
func (k *K8SServiceDiscovery) doWatch() error {
	watchOptions := metav1.ListOptions{
		LabelSelector: k.labelSelector,
		Watch:         true,
	}

	watcher, err := k.clientset.CoreV1().Pods(k.namespace).Watch(k.watchContext, watchOptions)
	if err != nil {
		return fmt.Errorf("failed to create pod watcher: %w", err)
	}
	defer watcher.Stop()

	logger.Infof("started watching pods in namespace %s with selector %s", k.namespace, k.labelSelector)

	for {
		select {
		case <-k.watchContext.Done():
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}

			if err := k.handleWatchEvent(event); err != nil {
				logger.Errorf("failed to handle watch event: %v", err)
			}
		}
	}
}

// handleWatchEvent 处理监听事件
func (k *K8SServiceDiscovery) handleWatchEvent(event watch.Event) error {
	pod, ok := event.Object.(*v1.Pod)
	if !ok {
		return fmt.Errorf("unexpected object type: %T", event.Object)
	}

	shouldInvalidateCache := false

	switch event.Type {
	case watch.Added:
		logger.Infof("pod added: %s", pod.Name)
		if k.isPodReady(pod) {
			endpoint := k.podToEndpoint(pod)
			if endpoint != nil {
				k.AddEndpoint(endpoint)
			}
		}
		shouldInvalidateCache = true

	case watch.Modified:
		logger.Infof("pod modified: %s", pod.Name)
		// 检查是否为关键状态变化
		if k.isSignificantChange(pod) {
			if k.isPodReady(pod) {
				endpoint := k.podToEndpoint(pod)
				if endpoint != nil {
					k.AddEndpoint(endpoint)
				}
			} else {
				k.RemoveEndpointFromCache(pod.Name)
			}
			shouldInvalidateCache = true
		}

	case watch.Deleted:
		logger.Infof("pod deleted: %s", pod.Name)
		k.RemoveEndpointFromCache(pod.Name)
		shouldInvalidateCache = true

	default:
		logger.Infof("unhandled pod event type: %s for pod: %s", event.Type, pod.Name)
	}

	if shouldInvalidateCache {
		k.InvalidateCache()
	}

	return nil
}

// isSignificantChange 检查是否为显著变化（影响服务可用性）
func (k *K8SServiceDiscovery) isSignificantChange(pod *v1.Pod) bool {
	// 检查Pod阶段变化
	if pod.Status.Phase != v1.PodRunning {
		return true
	}

	// 检查容器就绪状态变化
	if pod.Status.ContainerStatuses != nil {
		for _, status := range pod.Status.ContainerStatuses {
			if !status.Ready {
				return true
			}
		}
	}

	// 检查Pod IP变化
	if pod.Status.PodIP == "" {
		return true
	}

	return false
}

// RegisterEndpoint K8S服务发现不支持手动注册端点
func (k *K8SServiceDiscovery) RegisterEndpoint(ctx context.Context, endpoint *Endpoint, options ...RegisterOption) error {
	return errors.New("RegisterEndpoint: helm service discovery does not support dynamic registration")
}

// RemoveEndpoint K8S服务发现不支持手动移除端点
func (k *K8SServiceDiscovery) RemoveEndpoint(ctx context.Context, endpointID string) error {
	return errors.New("RemoveEndpoint: helm service discovery does not support dynamic removal")
}

// Health 健康检查
func (k *K8SServiceDiscovery) Health(ctx context.Context) error {
	k.closeMu.RLock()
	closed := k.closed
	k.closeMu.RUnlock()

	if closed {
		err := fmt.Errorf("helm service discovery is closed")
		logger.Errorf("health check failed: %v", err)
		return err
	}

	// 测试Kubernetes API连接
	_, err := k.clientset.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		logger.Errorf("helm api health check failed: %v", err)
		return fmt.Errorf("helm api health check failed: %w", err)
	}

	return k.CachedServiceDiscovery.Health(ctx)
}

// Close 关闭K8S服务发现
func (k *K8SServiceDiscovery) Close() error {
	k.closeMu.Lock()
	defer k.closeMu.Unlock()

	if k.closed {
		return nil
	}

	k.closed = true

	// 停止监听
	k.watcherMu.Lock()
	if k.watcherRunning {
		k.watcherRunning = false
		k.watchCancel()
	}
	k.watcherMu.Unlock()

	logger.Infof("helm service discovery closed")
	return nil
}
