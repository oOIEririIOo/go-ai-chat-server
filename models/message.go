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
	Role             string         `json:"role"` // "user" or "assistant"
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"` // 思考内容
	Timestamp        int64          `json:"timestamp"`
}
