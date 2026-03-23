package models

import (
	"gorm.io/gorm"
)

const (
	SessionStatusDraft  = "draft"
	SessionStatusActive = "active"
)

// ChatSession 会话表。
type ChatSession struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	UserID      uint           `json:"user_id"`
	Status      string         `json:"status" gorm:"default:active;index"`
	Title       string         `json:"title"`
	LastMessage string         `json:"last_message"`
	UpdateTime  int64          `json:"update_time"`
	Messages    []ChatMessage  `gorm:"foreignKey:SessionID" json:"messages"`
}
