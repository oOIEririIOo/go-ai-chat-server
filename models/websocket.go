package models

// WebSocketChatRequest WebSocket 聊天请求
type WebSocketChatRequest struct {
	Type        string                   `json:"type"`          // chat 或 recover
	SessionID   int64                    `json:"session_id"`    // 会话 ID
	Messages    []map[string]interface{} `json:"messages"`      // 消息列表
	Tools       []map[string]interface{} `json:"tools"`         // 工具列表
	ModelID     string                   `json:"model_id"`      // 模型 ID
	ApiKey      string                   `json:"api_key"`       // API Key
	BaseUrl     string                   `json:"base_url"`      // Base URL
	AIMessageID string                   `json:"ai_message_id"` // AI 消息 ID（用于前后端 ID 统一）
}

// WebSocketChatResponse WebSocket 聊天响应
type WebSocketChatResponse struct {
	Content          string `json:"content"`           // 内容
	ReasoningContent string `json:"reasoning_content"` // 思考内容
	IsReasoning      bool   `json:"is_reasoning"`      // 是否正在思考
	IsEnd            bool   `json:"is_end"`            // 是否结束
	SessionID        int64  `json:"session_id"`        // 会话 ID（用于前端路由）
	AIMessageID      string `json:"ai_message_id"`     // AI 消息 ID（用于前端路由）
}
