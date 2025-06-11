package request_analyzer

import "xfusion.com/tmatrix/runtime/pkg/common"

// RAG匹配表达式
const (
	// RAGSystemPattern 主流RAG模式：system message中注入检索内容
	RAGSystemPattern = `(?i)(context|retrieved|knowledge|sources?):\s*(.+?)(?:\n\n|\Z)`

	// RAGXMLPattern 传统XML标记模式
	RAGXMLPattern = `(?i)<(context|sources?|knowledge|retrieved)>(.*?)</\1>`

	// RAGStructuredPattern 现代structured prompt模式
	RAGStructuredPattern = `(?i)###\s*(context|sources?|retrieved)\s*\n(.*?)(?:\n###|\Z)`

	// RAGFunctionPattern OpenAI Assistant/Function calling中的RAG
	RAGFunctionPattern = `"(context|knowledge|retrieved_docs?)"\s*:\s*"([^"]+)"`

	// MetadataSourcePattern 元数据提取模式
	MetadataSourcePattern = `
$$
(source|来源):\s*([^
$$
]+)\]`

	// MetadataURLPattern URL
	MetadataURLPattern = `https?://[^\s\]]+`
)

// RAGChunk 表示一个RAG检索块的数据结构
type RAGChunk struct {
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata"`
	Index    int                    `json:"index"`
}

// RAGAnalysisResult RAG分析结果
type RAGAnalysisResult struct {
	ChunkCount int         `json:"chunk_count"`
	Chunks     []*RAGChunk `json:"chunks"`
}

// RequestIdentifiers 请求标识符
type RequestIdentifiers struct {
	ChatID       string `json:"chat_id"`
	RequestID    string `json:"request_id"`
	SessionID    string `json:"session_id"`
	IsNewSession bool   `json:"is_new_session"`
}

// RequestAnalysisResult 请求分析结果
type RequestAnalysisResult struct {
	RequestType common.RequestType  `json:"request_type"`
	Identifiers *RequestIdentifiers `json:"identifiers"`
	RAGInfo     *RAGAnalysisResult  `json:"rag_info,omitempty"`
}
