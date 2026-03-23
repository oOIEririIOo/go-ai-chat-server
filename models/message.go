package models

import "gorm.io/gorm"

// ChatMessage 聊天消息表。
// 第一版附件能力直接把附件列表序列化进 attachments_json，避免单独建表。
type ChatMessage struct {
	ID               string         `json:"id" gorm:"primaryKey"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
	SessionID        uint           `json:"session_id" gorm:"index"`
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	MessageType      string         `json:"message_type" gorm:"default:text"`
	AttachmentsJSON  string         `json:"-" gorm:"column:attachments_json;type:TEXT;default:'[]'"`
	ReasoningContent string         `json:"reasoning_content"`
	Timestamp        int64          `json:"timestamp"`
	IsStreaming      bool           `json:"is_streaming" gorm:"default:false"`
	IsReasoning      bool           `json:"is_reasoning" gorm:"default:false"`
}

// AttachmentItem 是前后端对齐后的附件元数据。
type AttachmentItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	MimeType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	LocalURI  string `json:"local_uri,omitempty"`
	RemoteURL string `json:"remote_url"`
	ObjectKey string `json:"object_key"`
	Width     *int   `json:"width,omitempty"`
	Height    *int   `json:"height,omitempty"`
}

// ChatMessageDTO 是返回给前端的消息结构。
// 这里把 attachments_json 反解成 attachments，保持响应与 Android 端一致。
type ChatMessageDTO struct {
	ID               string           `json:"id"`
	SessionID        uint             `json:"session_id"`
	Role             string           `json:"role"`
	Content          string           `json:"content"`
	MessageType      string           `json:"message_type"`
	Attachments      []AttachmentItem `json:"attachments"`
	ReasoningContent string           `json:"reasoning_content"`
	Timestamp        int64            `json:"timestamp"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
}
