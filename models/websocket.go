package models

// WebSocketChatRequest 是 WebSocket 聊天请求。
// 在统一 REST 落库后，这里只负责“触发 AI 回复”，不再承载整段历史消息。
type WebSocketChatRequest struct {
	Type          string                   `json:"type"`
	SessionID     int64                    `json:"session_id"`
	UserMessageID string                   `json:"user_message_id"`
	Tools         []map[string]interface{} `json:"tools"`
	ModelKey      string                   `json:"model_key"`
	ModelID       string                   `json:"model_id"`
	ApiKey        string                   `json:"api_key"`
	BaseUrl       string                   `json:"base_url"`
	AIMessageID   string                   `json:"ai_message_id"`
}

// WebSocketChatResponse 是流式返回给前端的消息结构。
type WebSocketChatResponse struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	IsReasoning      bool   `json:"is_reasoning"`
	IsEnd            bool   `json:"is_end"`
	SessionID        int64  `json:"session_id"`
	AIMessageID      string `json:"ai_message_id"`
}
