package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/server/v3/embed"
)

// TestETCDServiceDiscovery_BasicOperations 测试基本操作
func TestETCDServiceDiscovery_BasicOperations(t *testing.T) {
	// 启动嵌入式ETCD服务器
	etcdServer := startEmbeddedETCD(t)
	defer etcdServer.Close()

	// 创建服务发现实例
	sd, err := NewETCDServiceDiscovery(
		[]string{"http://localhost:2379"},
		"/test/endpoints",
		100*time.Millisecond,
	)
	require.NoError(t, err)
	defer sd.Close()

	ctx := context.Background()

	// 测试初始状态
	endpoints, err := sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Empty(t, endpoints)

	// 注册端点
	endpoint1 := NewEndpoint("ep1", "http://localhost:8001", "gpt-3.5")
	endpoint1.Status = EndpointStatusHealthy

	err = sd.RegisterEndpoint(ctx, endpoint1)
	require.NoError(t, err)

	// 验证端点已注册
	endpoints, err = sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 1)
	assert.Equal(t, "ep1", endpoints[0].EndpointID)
	assert.Equal(t, "http://localhost:8001", endpoints[0].Address)

	// 按模型查询端点
	gpt35Endpoints, err := sd.GetEndpointsByModel(ctx, "gpt-3.5")
	require.NoError(t, err)
	assert.Len(t, gpt35Endpoints, 1)

	unknownEndpoints, err := sd.GetEndpointsByModel(ctx, "unknown")
	require.NoError(t, err)
	assert.Empty(t, unknownEndpoints)

	// 注册第二个端点
	endpoint2 := NewEndpoint("ep2", "http://localhost:8002", "gpt-4")
	endpoint2.Status = EndpointStatusHealthy

	err = sd.RegisterEndpoint(ctx, endpoint2)
	require.NoError(t, err)

	// 验证两个端点都存在
	endpoints, err = sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 2)

	// 移除端点
	err = sd.RemoveEndpoint(ctx, "ep1")
	require.NoError(t, err)

	// 验证端点已移除
	endpoints, err = sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 1)
	assert.Equal(t, "ep2", endpoints[0].EndpointID)
}

// TestETCDServiceDiscovery_TTLExpiration 测试TTL过期
func TestETCDServiceDiscovery_TTLExpiration(t *testing.T) {
	// 启动嵌入式ETCD服务器
	etcdServer := startEmbeddedETCD(t)
	defer etcdServer.Close()

	// 创建服务发现实例
	sd, err := NewETCDServiceDiscovery(
		[]string{"http://localhost:2379"},
		"/test/ttl",
		100*time.Millisecond,
	)
	require.NoError(t, err)
	defer sd.Close()

	ctx := context.Background()

	// 注册带TTL的端点
	endpoint := NewEndpoint("ttl-ep", "http://localhost:8003", "test-model")
	endpoint.Status = EndpointStatusHealthy

	err = sd.RegisterEndpoint(ctx, endpoint, WithTTL(1)) // 1秒TTL
	require.NoError(t, err)

	// 立即验证端点存在
	endpoints, err := sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 1)

	// 等待TTL过期
	time.Sleep(5 * time.Second)

	// 强制刷新缓存
	sd.InvalidateCache()

	// 验证端点已过期
	endpoints, err = sd.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 0)
}

// TestETCDServiceDiscovery_WatchEvents 测试监听事件
func TestETCDServiceDiscovery_WatchEvents(t *testing.T) {
	// 启动嵌入式ETCD服务器
	etcdServer := startEmbeddedETCD(t)
	defer etcdServer.Close()

	// 创建两个服务发现实例，模拟分布式环境
	sd1, err := NewETCDServiceDiscovery(
		[]string{"http://localhost:2379"},
		"/test/watch",
		100*time.Millisecond,
	)
	require.NoError(t, err)
	defer sd1.Close()

	sd2, err := NewETCDServiceDiscovery(
		[]string{"http://localhost:2379"},
		"/test/watch",
		100*time.Millisecond,
	)
	require.NoError(t, err)
	defer sd2.Close()

	// 设置事件处理器
	eventCh := make(chan string, 10)
	sd2.SetEventHandler(&testEventHandler{eventCh: eventCh})

	ctx := context.Background()

	// 在sd1中注册端点
	endpoint := NewEndpoint("watch-ep", "http://localhost:8004", "watch-model")
	endpoint.Status = EndpointStatusHealthy

	err = sd1.RegisterEndpoint(ctx, endpoint)
	require.NoError(t, err)

	// 等待事件传播
	select {
	case event := <-eventCh:
		assert.Contains(t, event, "added:watch-ep")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for add event")
	}

	// 验证sd2能看到端点
	time.Sleep(2000 * time.Millisecond) // 等待缓存更新
	endpoints, err := sd2.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Len(t, endpoints, 1)

	// 在sd1中移除端点
	err = sd1.RemoveEndpoint(ctx, "watch-ep")
	require.NoError(t, err)

	// 等待删除事件
	select {
	case event := <-eventCh:
		assert.Contains(t, event, "removed:watch-ep")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for remove event")
	}

	// 验证sd2中端点已被移除
	time.Sleep(2000 * time.Millisecond) // 等待缓存更新
	endpoints, err = sd2.GetEndpoints(ctx)
	require.NoError(t, err)
	assert.Empty(t, endpoints)
}

// testEventHandler 测试事件处理器
type testEventHandler struct {
	eventCh chan string
}

func (h *testEventHandler) OnEndpointAdded(endpoint *Endpoint) {
	h.eventCh <- "added:" + endpoint.EndpointID
}

func (h *testEventHandler) OnEndpointRemoved(endpointID string) {
	h.eventCh <- "removed:" + endpointID
}

func (h *testEventHandler) OnEndpointUpdated(endpoint *Endpoint) {
	h.eventCh <- "updated:" + endpoint.EndpointID
}

// startEmbeddedETCD 启动嵌入式ETCD服务器用于测试
func startEmbeddedETCD(t *testing.T) *embed.Etcd {
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error" // 减少日志输出

	e, err := embed.StartEtcd(cfg)
	require.NoError(t, err)

	select {
	case <-e.Server.ReadyNotify():
		t.Log("Embedded etcd is ready!")
	case <-time.After(10 * time.Second):
		t.Fatal("Embedded etcd took too long to start!")
	}

	return e
}
