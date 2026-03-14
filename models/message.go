package models

import (
	"gorm.io/gorm"
)

// ChatMessage 消息明细表
type ChatMessage struct {
	ID               string         `json:"id" gorm:"primaryKey"` // 使用前端生成的字符串 ID
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
	SessionID        uint           `json:"session_id" gorm:"index"`
	Role             string         `json:"role"` // "user" or "assistant"
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"` // 思考内容
	Timestamp        int64          `json:"timestamp"`
	// 是否正在流式输出中
	IsStreaming bool `json:"is_streaming" gorm:"default:false"`
	// 是否正在思考
	IsReasoning bool `json:"is_reasoning" gorm:"default:false"`
}
