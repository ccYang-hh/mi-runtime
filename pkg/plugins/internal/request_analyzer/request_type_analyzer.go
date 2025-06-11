package request_analyzer

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"sync"
	"time"

	"xfusion.com/tmatrix/runtime/pkg/common"
	"xfusion.com/tmatrix/runtime/pkg/common/logger"
	rcx "xfusion.com/tmatrix/runtime/pkg/context"
	"xfusion.com/tmatrix/runtime/pkg/pipelines"
)

// OpenAIRequestAnalyzer OpenAI请求分析器
type OpenAIRequestAnalyzer struct {
	ragPatterns  []*regexp.Regexp
	metaPatterns []*regexp.Regexp
	sessionStore sync.Map
	chunkPool    sync.Pool
}

func NewOpenAIRequestAnalyzer() *OpenAIRequestAnalyzer {
	analyzer := &OpenAIRequestAnalyzer{}

	// 预编译RAG识别模式
	ragPatterns := []string{
		RAGSystemPattern,     // 最常见：system message中的context注入
		RAGXMLPattern,        // 传统XML标记
		RAGStructuredPattern, // 现代structured prompt
		RAGFunctionPattern,   // Function/Tool calling
	}

	for _, pattern := range ragPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			analyzer.ragPatterns = append(analyzer.ragPatterns, compiled)
		}
	}

	// 预编译元数据提取模式
	metaPatterns := []string{
		MetadataSourcePattern,
		MetadataURLPattern,
	}

	for _, pattern := range metaPatterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			analyzer.metaPatterns = append(analyzer.metaPatterns, compiled)
		}
	}

	// 对象池
	analyzer.chunkPool = sync.Pool{
		New: func() interface{} {
			return &RAGChunk{Metadata: make(map[string]interface{})}
		},
	}

	return analyzer
}

func (a *OpenAIRequestAnalyzer) AnalyzeRequest(data map[string]interface{}) (
	common.RequestType, *RequestIdentifiers,
	[]*RAGChunk, error,
) {
	// 提取消息
	messages := a.extractMessages(data)

	// RAG检测
	chunks := a.detectRAGContent(messages)

	// 确定请求类型
	requestType := a.determineRequestType(messages, len(chunks) > 0)

	// 提取标识符
	identifiers := a.extractIdentifiers(data)

	return requestType, identifiers, chunks, nil
}

// detectRAGContent ...
func (a *OpenAIRequestAnalyzer) detectRAGContent(messages []map[string]interface{}) []*RAGChunk {
	var chunks []*RAGChunk
	chunkIndex := 0

	for _, msg := range messages {
		role, _ := msg["role"].(string)

		// 重点检查system message - 现代RAG主要注入点
		if role == "system" {
			if content, ok := msg["content"].(string); ok {
				chunks = append(chunks, a.extractFromContent(content, &chunkIndex)...)
			}
		}

		// 检查user message中的结构化内容
		if role == "user" {
			if content, ok := msg["content"].(string); ok {
				// 只有当内容明显包含检索标记时才处理
				if a.containsRAGMarkers(content) {
					chunks = append(chunks, a.extractFromContent(content, &chunkIndex)...)
				}
			}
		}

		// 检查工具调用 - OpenAI Assistant RAG模式
		if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
			chunks = append(chunks, a.extractFromToolCalls(toolCalls, &chunkIndex)...)
		}
	}

	return chunks
}

// containsRAGMarkers 快速检测是否包含RAG标记
func (a *OpenAIRequestAnalyzer) containsRAGMarkers(content string) bool {
	// 快速字符串检查，避免昂贵的正则匹配
	lowerContent := strings.ToLower(content)
	markers := []string{"context:", "sources:", "retrieved:", "knowledge:", "###", "<context>"}

	for _, marker := range markers {
		if strings.Contains(lowerContent, marker) {
			return true
		}
	}
	return false
}

func (a *OpenAIRequestAnalyzer) extractFromContent(content string, chunkIndex *int) []*RAGChunk {
	var chunks []*RAGChunk

	for _, pattern := range a.ragPatterns {
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				*chunkIndex++
				chunk := a.chunkPool.Get().(*RAGChunk)
				chunk.Content = strings.TrimSpace(match[2])
				chunk.Index = *chunkIndex
				chunk.Metadata = a.extractMetadata(chunk.Content)
				chunks = append(chunks, chunk)
			}
		}
	}

	return chunks
}

func (a *OpenAIRequestAnalyzer) extractFromToolCalls(toolCalls []interface{}, chunkIndex *int) []*RAGChunk {
	var chunks []*RAGChunk

	for _, toolCall := range toolCalls {
		if tc, ok := toolCall.(map[string]interface{}); ok {
			if function, ok := tc["function"].(map[string]interface{}); ok {
				if argsStr, ok := function["arguments"].(string); ok {
					var args map[string]interface{}
					if json.Unmarshal([]byte(argsStr), &args) == nil {
						// 检查常见的RAG参数名
						ragKeys := []string{"context", "knowledge", "retrieved_docs", "sources"}
						for _, key := range ragKeys {
							if content, ok := args[key].(string); ok && content != "" {
								*chunkIndex++
								chunk := a.chunkPool.Get().(*RAGChunk)
								chunk.Content = content
								chunk.Index = *chunkIndex
								chunk.Metadata = map[string]interface{}{"source": "tool_call"}
								chunks = append(chunks, chunk)
							}
						}
					}
				}
			}
		}
	}

	return chunks
}

func (a *OpenAIRequestAnalyzer) extractMetadata(content string) map[string]interface{} {
	metadata := make(map[string]interface{})

	for _, pattern := range a.metaPatterns {
		if matches := pattern.FindStringSubmatch(content); len(matches) > 1 {
			if len(matches) == 3 { // source pattern
				metadata["source"] = strings.TrimSpace(matches[2])
			} else { // URL pattern
				metadata["url"] = matches[0]
			}
		}
	}

	return metadata
}

func (a *OpenAIRequestAnalyzer) determineRequestType(messages []map[string]interface{}, hasRAG bool) common.RequestType {
	if hasRAG {
		return common.RequestTypeRAG
	}

	userCount, assistantCount := 0, 0
	for _, msg := range messages {
		switch msg["role"] {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		}
	}

	if assistantCount == 0 && userCount <= 1 {
		return common.RequestTypeFirstTime
	}

	return common.RequestTypeHistory
}

func (a *OpenAIRequestAnalyzer) extractMessages(data map[string]interface{}) []map[string]interface{} {
	if messages, ok := data["messages"].([]interface{}); ok {
		result := make([]map[string]interface{}, 0, len(messages))
		for _, msg := range messages {
			if m, ok := msg.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	}
	return nil
}

func (a *OpenAIRequestAnalyzer) extractIdentifiers(data map[string]interface{}) *RequestIdentifiers {
	// 会话ID处理
	sessionID := a.getStringValue(data, "session_id")
	isNewSession := false

	if sessionID == "" {
		sessionID = uuid.New().String()
		isNewSession = true
	} else if _, exists := a.sessionStore.LoadOrStore(sessionID, true); !exists {
		isNewSession = true
	}

	// 对话ID和请求ID
	chatID := a.getStringValue(data, "chat_id")
	if chatID == "" {
		// TODO 客户端不支持的话，先固定一个ChatID
		chatID = "47c3b6a9-697b-4f12-a7f3-b9c6aa72a8cd" // 测试固定ID
	}

	requestID := a.getStringValue(data, "request_id")
	if requestID == "" {
		requestID = uuid.New().String()
	}

	return &RequestIdentifiers{
		ChatID:       chatID,
		RequestID:    requestID,
		SessionID:    sessionID,
		IsNewSession: isNewSession,
	}
}

func (a *OpenAIRequestAnalyzer) getStringValue(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}

// TypeAnalyzerStage 请求类型识别Stage
type TypeAnalyzerStage struct {
	*pipelines.BaseStage
	analyzer *OpenAIRequestAnalyzer
}

func NewTypeAnalyzeStage(name string) *TypeAnalyzerStage {
	return &TypeAnalyzerStage{
		BaseStage: pipelines.NewBaseStage(name),
		analyzer:  NewOpenAIRequestAnalyzer(),
	}
}

func (s *TypeAnalyzerStage) Process(rc *rcx.RequestContext) error {
	rc.SetState(rcx.StatePreprocessing)

	start := time.Now()
	requestType, identifiers, chunks, err := s.analyzer.AnalyzeRequest(rc.ParsedBody)
	if err != nil {
		return fmt.Errorf("request analyze failed: %w", err)
	}

	logger.Infof("request analyze finished: type=%s, chunks=%d, cost=%v",
		requestType, len(chunks), time.Since(start))

	// 更新上下文
	rc.RequestIdentifiers["chat_id"] = identifiers.ChatID
	rc.RequestIdentifiers["request_id"] = identifiers.RequestID
	rc.RequestIdentifiers["session_id"] = identifiers.SessionID
	rc.RequestIdentifiers["is_new_session"] = identifiers.IsNewSession

	return nil
}
