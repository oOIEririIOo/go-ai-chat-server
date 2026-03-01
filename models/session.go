package models

import (
	"gorm.io/gorm"
)

// ChatSession 会话表
type ChatSession struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	UserID      uint           `json:"user_id"`
	Title       string         `json:"title"`
	LastMessage string         `json:"last_message"`
	Messages    []ChatMessage  `gorm:"foreignKey:SessionID" json:"messages"` // 一对多
}
