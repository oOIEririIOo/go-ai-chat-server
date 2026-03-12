package models

// WebSocketMessage WebSocket消息基础结构
type WebSocketMessage struct {
	Type      string      `json:"type"`                 // 消息类型：message, ack, heartbeat, recover, close_reason, connected
	ID        int64       `json:"id,omitempty"`         // 消息ID
	ClientID  string      `json:"client_id,omitempty"`  // 客户端消息ID
	Seq       int64       `json:"seq,omitempty"`        // 消息序列号（服务器返回）
	Content   string      `json:"content,omitempty"`    // 消息内容
	LastSeq   int64       `json:"last_seq,omitempty"`   // 客户端重连时的最后序列号
	IsEnd     bool        `json:"is_end,omitempty"`     // 流消息是否结束
	Data      interface{} `json:"data,omitempty"`       // 通用数据字段
	Error     string      `json:"error,omitempty"`      // 错误信息
	SessionID int64       `json:"session_id,omitempty"` // 会话ID（连接确认时返回）

	// 新增字段：支持前端传递的参数
	ModelId  string      `json:"model_id,omitempty"` // 模型ID
	BaseUrl  string      `json:"base_url,omitempty"` // API Base URL
	Tools    []string    `json:"tools,omitempty"`    // 工具列表
	Messages []AiMessage `json:"messages,omitempty"` // 历史消息上下文
}

// RecoverRequest 重连恢复请求
type RecoverRequest struct {
	Type    string `json:"type"`     // "recover"
	LastSeq int64  `json:"last_seq"` // 最后的序列号
}

// AckMessage ACK确认消息
type AckMessage struct {
	Type     string `json:"type"`      // "ack"
	ID       int64  `json:"id"`        // 被确认的消息ID
	ClientID string `json:"client_id"` // 被确认的客户端消息ID
}

// HeartbeatMessage 心跳消息
type HeartbeatMessage struct {
	Type string `json:"type"` // "heartbeat"
}

// StreamingDelta 流式传输的增量消息
type StreamingDelta struct {
	Content          string `json:"content,omitempty"`           // 流式内容增量
	ReasoningContent string `json:"reasoning_content,omitempty"` // 思考内容增量
}
