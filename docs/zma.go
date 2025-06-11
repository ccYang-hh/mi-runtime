
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromq/goczmq"
	"go.etcd.io/etcd/clientv3"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
)

// 存储介质类型
type StorageType int

const (
	StorageGPU StorageType = iota
	StorageCPU
	StorageDisk
)

func (s StorageType) String() string {
	switch s {
	case StorageGPU:
		return "GPU"
	case StorageCPU:
		return "CPU"
	case StorageDisk:
		return "Disk"
	default:
		return "Unknown"
	}
}

// 事件类型
type EventType string

const (
	EventBlockStored   EventType = "BlockStored"
	EventBlockRemoved  EventType = "BlockRemoved"
	EventBlocksCleared EventType = "AllBlocksCleared"
)

// KV缓存事件基类
type KVCacheEvent struct {
	Type      EventType   `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// 块存储事件
type BlockStoredEvent struct {
	BlockHashes     []uint64    `json:"block_hashes"`
	ParentBlockHash *uint64     `json:"parent_block_hash,omitempty"`
	TokenIDs        []int32     `json:"token_ids"`
	BlockSize       int32       `json:"block_size"`
	LoraID          *int32      `json:"lora_id,omitempty"`
	StorageType     StorageType `json:"storage_type"`
}

// 块移除事件
type BlockRemovedEvent struct {
	BlockHashes []uint64    `json:"block_hashes"`
	StorageType StorageType `json:"storage_type"`
}

// 块清除事件
type BlocksClearedEvent struct {
	StorageType StorageType `json:"storage_type"`
}

// 事件批次
type EventBatch struct {
	Timestamp float64        `json:"ts"`
	Events    []KVCacheEvent `json:"events"`
}

// 块信息
type BlockInfo struct {
	InstanceID  string      `json:"instance_id"`
	BlockHash   uint64      `json:"block_hash"`
	TokenIDs    []int32     `json:"token_ids"`
	ParentHash  *uint64     `json:"parent_hash,omitempty"`
	BlockSize   int32       `json:"block_size"`
	LoraID      *int32      `json:"lora_id,omitempty"`
	Timestamp   int64       `json:"timestamp"`
	StorageType StorageType `json:"storage_type"`
}

// 前缀匹配结果
type PrefixMatchResult struct {
	StorageType StorageType `json:"storage_type"`
	InstanceID  string      `json:"instance_id"`
	BlockHash   uint64      `json:"block_hash"`
	MatchLength int         `json:"match_length"`
	BlockInfo   *BlockInfo  `json:"block_info"`
}

// 多存储介质前缀匹配结果
type MultiStoragePrefixResult struct {
	GPUResult  *PrefixMatchResult `json:"gpu_result,omitempty"`
	CPUResult  *PrefixMatchResult `json:"cpu_result,omitempty"`
	DiskResult *PrefixMatchResult `json:"disk_result,omitempty"`
}

// Trie节点 - 支持多存储介质
type TrieNode struct {
	TokenID  int32
	Children map[int32]*TrieNode
	// 每个存储介质的块信息映射：instanceID -> BlockInfo
	GPUBlocks  map[string]*BlockInfo
	CPUBlocks  map[string]*BlockInfo
	DiskBlocks map[string]*BlockInfo
	mu         sync.RWMutex
}

// 创建新的Trie节点
func NewTrieNode(tokenID int32) *TrieNode {
	return &TrieNode{
		TokenID:    tokenID,
		Children:   make(map[int32]*TrieNode),
		GPUBlocks:  make(map[string]*BlockInfo),
		CPUBlocks:  make(map[string]*BlockInfo),
		DiskBlocks: make(map[string]*BlockInfo),
	}
}

// 获取指定存储类型的块信息
func (n *TrieNode) getBlocksForStorage(storageType StorageType) map[string]*BlockInfo {
	switch storageType {
	case StorageGPU:
		return n.GPUBlocks
	case StorageCPU:
		return n.CPUBlocks
	case StorageDisk:
		return n.DiskBlocks
	default:
		return nil
	}
}

// 高性能KV块索引器
type KVBlockIndexer struct {
	root *TrieNode
	// 实例ID到块哈希集合的映射，按存储类型分组
	instanceBlocks map[StorageType]map[string]map[uint64]*BlockInfo
	// 块哈希到块信息的映射
	blockRegistry map[uint64]*BlockInfo
	// 统计信息
	stats struct {
		totalBlocks   int64
		gpuBlocks     int64
		cpuBlocks     int64
		diskBlocks    int64
		queryCount    int64
		lastQueryTime int64
	}
	mu sync.RWMutex
}

// 创建新的索引器
func NewKVBlockIndexer() *KVBlockIndexer {
	indexer := &KVBlockIndexer{
		root:           NewTrieNode(-1), // 根节点使用-1作为标识
		instanceBlocks: make(map[StorageType]map[string]map[uint64]*BlockInfo),
		blockRegistry:  make(map[uint64]*BlockInfo),
	}

	// 初始化存储类型映射
	for _, storageType := range []StorageType{StorageGPU, StorageCPU, StorageDisk} {
		indexer.instanceBlocks[storageType] = make(map[string]map[uint64]*BlockInfo)
	}

	return indexer
}

// 添加块到索引
func (idx *KVBlockIndexer) AddBlock(blockInfo *BlockInfo) error {
	if blockInfo == nil {
		return fmt.Errorf("block info cannot be nil")
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 添加到块注册表
	idx.blockRegistry[blockInfo.BlockHash] = blockInfo

	// 添加到实例块映射
	if idx.instanceBlocks[blockInfo.StorageType][blockInfo.InstanceID] == nil {
		idx.instanceBlocks[blockInfo.StorageType][blockInfo.InstanceID] = make(map[uint64]*BlockInfo)
	}
	idx.instanceBlocks[blockInfo.StorageType][blockInfo.InstanceID][blockInfo.BlockHash] = blockInfo

	// 添加到Trie树
	current := idx.root
	for _, tokenID := range blockInfo.TokenIDs {
		if current.Children[tokenID] == nil {
			current.Children[tokenID] = NewTrieNode(tokenID)
		}
		current = current.Children[tokenID]
	}

	// 在最后一个节点添加块信息
	current.mu.Lock()
	blocks := current.getBlocksForStorage(blockInfo.StorageType)
	if blocks != nil {
		blocks[blockInfo.InstanceID] = blockInfo
	}
	current.mu.Unlock()

	// 更新统计信息
	atomic.AddInt64(&idx.stats.totalBlocks, 1)
	switch blockInfo.StorageType {
	case StorageGPU:
		atomic.AddInt64(&idx.stats.gpuBlocks, 1)
	case StorageCPU:
		atomic.AddInt64(&idx.stats.cpuBlocks, 1)
	case StorageDisk:
		atomic.AddInt64(&idx.stats.diskBlocks, 1)
	}

	logger.Debugf("added block %d to %s storage for instance %s",
		blockInfo.BlockHash, blockInfo.StorageType.String(), blockInfo.InstanceID)

	return nil
}

// 移除块
func (idx *KVBlockIndexer) RemoveBlock(blockHash uint64) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	blockInfo, exists := idx.blockRegistry[blockHash]
	if !exists {
		return fmt.Errorf("block %d not found", blockHash)
	}

	// 从实例块映射中移除
	if instanceBlocks := idx.instanceBlocks[blockInfo.StorageType][blockInfo.InstanceID]; instanceBlocks != nil {
		delete(instanceBlocks, blockHash)
		if len(instanceBlocks) == 0 {
			delete(idx.instanceBlocks[blockInfo.StorageType], blockInfo.InstanceID)
		}
	}

	// 从Trie树中移除
	idx.removeFromTrie(blockInfo)

	// 从块注册表中移除
	delete(idx.blockRegistry, blockHash)

	// 更新统计信息
	atomic.AddInt64(&idx.stats.totalBlocks, -1)
	switch blockInfo.StorageType {
	case StorageGPU:
		atomic.AddInt64(&idx.stats.gpuBlocks, -1)
	case StorageCPU:
		atomic.AddInt64(&idx.stats.cpuBlocks, -1)
	case StorageDisk:
		atomic.AddInt64(&idx.stats.diskBlocks, -1)
	}

	logger.Debugf("removed block %d from %s storage for instance %s",
		blockHash, blockInfo.StorageType.String(), blockInfo.InstanceID)

	return nil
}

// 从Trie树中移除块信息
func (idx *KVBlockIndexer) removeFromTrie(blockInfo *BlockInfo) {
	current := idx.root
	path := make([]*TrieNode, 0, len(blockInfo.TokenIDs)+1)
	path = append(path, current)

	// 找到对应的节点路径
	for _, tokenID := range blockInfo.TokenIDs {
		if current.Children[tokenID] == nil {
			return // 路径不存在
		}
		current = current.Children[tokenID]
		path = append(path, current)
	}

	// 从最后一个节点移除块信息
	current.mu.Lock()
	blocks := current.getBlocksForStorage(blockInfo.StorageType)
	if blocks != nil {
		delete(blocks, blockInfo.InstanceID)
	}
	current.mu.Unlock()

	// 清理空节点（从叶子节点向上）
	for i := len(path) - 1; i > 0; i-- {
		node := path[i]
		node.mu.RLock()
		isEmpty := len(node.Children) == 0 &&
			len(node.GPUBlocks) == 0 &&
			len(node.CPUBlocks) == 0 &&
			len(node.DiskBlocks) == 0
		node.mu.RUnlock()

		if isEmpty {
			parent := path[i-1]
			parent.mu.Lock()
			delete(parent.Children, node.TokenID)
			parent.mu.Unlock()
		} else {
			break // 如果节点不为空，停止清理
		}
	}
}

// 清除实例的所有块
func (idx *KVBlockIndexer) ClearInstance(instanceID string, storageType StorageType) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	instanceBlocks := idx.instanceBlocks[storageType][instanceID]
	if instanceBlocks == nil {
		return nil // 实例没有块
	}

	// 移除所有块
	for blockHash := range instanceBlocks {
		if blockInfo := idx.blockRegistry[blockHash]; blockInfo != nil {
			idx.removeFromTrie(blockInfo)
			delete(idx.blockRegistry, blockHash)

			// 更新统计信息
			atomic.AddInt64(&idx.stats.totalBlocks, -1)
			switch storageType {
			case StorageGPU:
				atomic.AddInt64(&idx.stats.gpuBlocks, -1)
			case StorageCPU:
				atomic.AddInt64(&idx.stats.cpuBlocks, -1)
			case StorageDisk:
				atomic.AddInt64(&idx.stats.diskBlocks, -1)
			}
		}
	}

	// 清除实例映射
	delete(idx.instanceBlocks[storageType], instanceID)

	logger.Infof("cleared %d blocks from %s storage for instance %s",
		len(instanceBlocks), storageType.String(), instanceID)

	return nil
}

// 查找最长前缀匹配（所有存储介质）
func (idx *KVBlockIndexer) FindLongestPrefixMatch(tokenIDs []int32) *MultiStoragePrefixResult {
	atomic.AddInt64(&idx.stats.queryCount, 1)
	atomic.StoreInt64(&idx.stats.lastQueryTime, time.Now().UnixNano())

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := &MultiStoragePrefixResult{}

	// 查找每种存储介质的最长前缀
	result.GPUResult = idx.findLongestPrefixForStorage(tokenIDs, StorageGPU)
	result.CPUResult = idx.findLongestPrefixForStorage(tokenIDs, StorageCPU)
	result.DiskResult = idx.findLongestPrefixForStorage(tokenIDs, StorageDisk)

	return result
}

// 查找指定存储介质的最长前缀匹配
func (idx *KVBlockIndexer) findLongestPrefixForStorage(tokenIDs []int32, storageType StorageType) *PrefixMatchResult {
	var bestMatch *PrefixMatchResult
	maxLength := 0

	current := idx.root
	for i, tokenID := range tokenIDs {
		if current.Children[tokenID] == nil {
			break
		}
		current = current.Children[tokenID]

		// 检查当前节点是否有指定存储类型的块
		current.mu.RLock()
		blocks := current.getBlocksForStorage(storageType)
		if blocks != nil && len(blocks) > 0 {
			// 找到一个匹配，选择时间戳最新的
			var latestBlock *BlockInfo
			for _, blockInfo := range blocks {
				if latestBlock == nil || blockInfo.Timestamp > latestBlock.Timestamp {
					latestBlock = blockInfo
				}
			}

			if latestBlock != nil && i+1 > maxLength {
				maxLength = i + 1
				bestMatch = &PrefixMatchResult{
					StorageType: storageType,
					InstanceID:  latestBlock.InstanceID,
					BlockHash:   latestBlock.BlockHash,
					MatchLength: maxLength,
					BlockInfo:   latestBlock,
				}
			}
		}
		current.mu.RUnlock()
	}

	return bestMatch
}

// 获取统计信息
func (idx *KVBlockIndexer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"total_blocks":    atomic.LoadInt64(&idx.stats.totalBlocks),
		"gpu_blocks":      atomic.LoadInt64(&idx.stats.gpuBlocks),
		"cpu_blocks":      atomic.LoadInt64(&idx.stats.cpuBlocks),
		"disk_blocks":     atomic.LoadInt64(&idx.stats.diskBlocks),
		"query_count":     atomic.LoadInt64(&idx.stats.queryCount),
		"last_query_time": atomic.LoadInt64(&idx.stats.lastQueryTime),
	}
}

// 状态构建器
type StateBuilder struct {
	indexer       *KVBlockIndexer
	eventCh       chan *EventBatch
	stopCh        chan struct{}
	wg            sync.WaitGroup
	etcdClient    *clientv3.Client
	snapshotTimer *time.Timer

	// 统计信息
	stats struct {
		eventsProcessed int64
		eventsDropped   int64
		lastEventTime   int64
	}
}

// 创建状态构建器
func NewStateBuilder(indexer *KVBlockIndexer, etcdEndpoints []string) (*StateBuilder, error) {
	sb := &StateBuilder{
		indexer: indexer,
		eventCh: make(chan *EventBatch, 1000), // 缓冲1000个事件批次
		stopCh:  make(chan struct{}),
	}

	// 连接ETCD
	if len(etcdEndpoints) > 0 {
		client, err := clientv3.New(clientv3.Config{
			Endpoints:   etcdEndpoints,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			logger.Errorf("failed to connect to etcd: %v", err)
			return nil, err
		}
		sb.etcdClient = client
		logger.Infof("connected to etcd endpoints: %v", etcdEndpoints)
	}

	return sb, nil
}

// 启动状态构建器
func (sb *StateBuilder) Start() {
	sb.wg.Add(1)
	go sb.eventProcessor()

	// 启动定时快照
	if sb.etcdClient != nil {
		sb.startSnapshotTimer()
	}

	logger.Infof("state builder started")
}

// 停止状态构建器
func (sb *StateBuilder) Stop() {
	close(sb.stopCh)
	sb.wg.Wait()

	if sb.snapshotTimer != nil {
		sb.snapshotTimer.Stop()
	}

	if sb.etcdClient != nil {
		sb.etcdClient.Close()
	}

	logger.Infof("state builder stopped")
}

// 处理事件批次
func (sb *StateBuilder) ProcessEventBatch(batch *EventBatch, instanceID string) error {
	select {
	case sb.eventCh <- batch:
		return nil
	default:
		atomic.AddInt64(&sb.stats.eventsDropped, 1)
		logger.Warnf("event queue full, dropped event batch from instance %s", instanceID)
		return fmt.Errorf("event queue full")
	}
}

// 事件处理器
func (sb *StateBuilder) eventProcessor() {
	defer sb.wg.Done()

	for {
		select {
		case batch := <-sb.eventCh:
			sb.processBatch(batch)
		case <-sb.stopCh:
			// 处理剩余事件
			for len(sb.eventCh) > 0 {
				batch := <-sb.eventCh
				sb.processBatch(batch)
			}
			return
		}
	}
}

// 处理单个事件批次
func (sb *StateBuilder) processBatch(batch *EventBatch) {
	for _, event := range batch.Events {
		atomic.AddInt64(&sb.stats.eventsProcessed, 1)
		atomic.StoreInt64(&sb.stats.lastEventTime, time.Now().UnixNano())

		switch event.Type {
		case EventBlockStored:
			sb.handleBlockStored(event)
		case EventBlockRemoved:
			sb.handleBlockRemoved(event)
		case EventBlocksCleared:
			sb.handleBlocksCleared(event)
		default:
			logger.Warnf("unknown event type: %s", event.Type)
		}
	}
}

// 处理块存储事件
func (sb *StateBuilder) handleBlockStored(event KVCacheEvent) {
	var storedEvent BlockStoredEvent
	eventData, _ := json.Marshal(event.Data)
	if err := json.Unmarshal(eventData, &storedEvent); err != nil {
		logger.Errorf("failed to unmarshal block stored event: %v", err)
		return
	}

	for _, blockHash := range storedEvent.BlockHashes {
		blockInfo := &BlockInfo{
			BlockHash:   blockHash,
			TokenIDs:    storedEvent.TokenIDs,
			ParentHash:  storedEvent.ParentBlockHash,
			BlockSize:   storedEvent.BlockSize,
			LoraID:      storedEvent.LoraID,
			Timestamp:   event.Timestamp,
			StorageType: storedEvent.StorageType,
		}

		if err := sb.indexer.AddBlock(blockInfo); err != nil {
			logger.Errorf("failed to add block %d: %v", blockHash, err)
		}
	}
}

// 处理块移除事件
func (sb *StateBuilder) handleBlockRemoved(event KVCacheEvent) {
	var removedEvent BlockRemovedEvent
	eventData, _ := json.Marshal(event.Data)
	if err := json.Unmarshal(eventData, &removedEvent); err != nil {
		logger.Errorf("failed to unmarshal block removed event: %v", err)
		return
	}

	for _, blockHash := range removedEvent.BlockHashes {
		if err := sb.indexer.RemoveBlock(blockHash); err != nil {
			logger.Errorf("failed to remove block %d: %v", blockHash, err)
		}
	}
}

// 处理块清除事件
func (sb *StateBuilder) handleBlocksCleared(event KVCacheEvent) {
	var clearedEvent BlocksClearedEvent
	eventData, _ := json.Marshal(event.Data)
	if err := json.Unmarshal(eventData, &clearedEvent); err != nil {
		logger.Errorf("failed to unmarshal blocks cleared event: %v", err)
		return
	}

	// 这里需要实例ID，但