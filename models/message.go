package models

import (
	"gorm.io/gorm"
)

// ChatMessage 消息明细表
type ChatMessage struct {
	ID               uint           `json:"id" gorm:"primaryKey"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
	SessionID        uint           `json:"session_id" gorm:"index"`
	ClientID         string         `json:"client_id" gorm:"uniqueIndex:idx_session_client_id"` // 客户端生成的消息ID，用于消息同步和去重
	Role             string         `json:"role"` // "user" or "assistant"
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"` // 思考内容
	Timestamp        int64          `json:"timestamp"`
	Seq              int64          `json:"seq" gorm:"index"` // 消息序列号，每个会话内递增，用于WebSocket重连恢复
}
