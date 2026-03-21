package service

import (
	"ai-chat/models"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChatService 聊天服务。
type ChatService struct {
	db        *gorm.DB
	aiService *AiService
}

const (
	DefaultMessageHistoryPageSize = 20
	MaxMessageHistoryPageSize     = 100
)

// MessageHistoryPage 表示一页历史消息结果。
type MessageHistoryPage struct {
	Messages            []models.ChatMessage `json:"messages"`
	HasMore             bool                 `json:"has_more"`
	NextBeforeMessageID *string              `json:"next_before_message_id"`
}

// NewChatService 创建聊天服务实例。
func NewChatService(db *gorm.DB, aiService *AiService) *ChatService {
	return &ChatService{
		db:        db,
		aiService: aiService,
	}
}

// CreateSession 创建聊天会话。
func (s *ChatService) CreateSession(userId uint, title string) (*models.ChatSession, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	session := &models.ChatSession{
		Title:     title,
		UserID:    userId,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.db.Create(session).Error; err != nil {
		return nil, fmt.Errorf("创建会话失败: %v", err)
	}
	return session, nil
}

// GetSessions 获取用户的所有会话。
func (s *ChatService) GetSessions(userId uint) ([]models.ChatSession, error) {
	var sessions []models.ChatSession
	if err := s.db.Where("user_id = ?", userId).Order("updated_at DESC").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("获取会话列表失败: %v", err)
	}
	return sessions, nil
}

// GetSessionMessagesPage 按页获取会话历史消息。
func (s *ChatService) GetSessionMessagesPage(sessionId uint, beforeMessageID string, limit int) (*MessageHistoryPage, error) {
	if limit <= 0 {
		limit = DefaultMessageHistoryPageSize
	}
	if limit > MaxMessageHistoryPageSize {
		limit = MaxMessageHistoryPageSize
	}

	var session models.ChatSession
	if err := s.db.First(&session, sessionId).Error; err != nil {
		return nil, fmt.Errorf("获取会话失败: %v", err)
	}

	query := s.db.Model(&models.ChatMessage{}).Where("session_id = ?", sessionId)
	if beforeMessageID != "" {
		var anchor models.ChatMessage
		if err := s.db.Where("session_id = ? AND id = ?", sessionId, beforeMessageID).First(&anchor).Error; err != nil {
			return nil, fmt.Errorf("获取历史锚点消息失败: %v", err)
		}
		query = query.Where("(timestamp < ?) OR (timestamp = ? AND id < ?)", anchor.Timestamp, anchor.Timestamp, anchor.ID)
	}

	var descendingMessages []models.ChatMessage
	if err := query.
		Order("timestamp DESC").
		Order("id DESC").
		Limit(limit + 1).
		Find(&descendingMessages).Error; err != nil {
		return nil, fmt.Errorf("获取历史消息分页失败: %v", err)
	}

	hasMore := len(descendingMessages) > limit
	if hasMore {
		descendingMessages = descendingMessages[:limit]
	}

	messages := reverseMessages(descendingMessages)
	var nextBeforeMessageID *string
	if hasMore && len(messages) > 0 {
		nextBeforeMessageID = &messages[0].ID
	}

	return &MessageHistoryPage{
		Messages:            messages,
		HasMore:             hasMore,
		NextBeforeMessageID: nextBeforeMessageID,
	}, nil
}

// reverseMessages 把倒序查询结果翻转成时间升序。
func reverseMessages(messages []models.ChatMessage) []models.ChatMessage {
	reversed := make([]models.ChatMessage, len(messages))
	for i := range messages {
		reversed[len(messages)-1-i] = messages[i]
	}
	return reversed
}

// SaveMessage 保存消息。
func (s *ChatService) SaveMessage(sessionId uint, role, content, reasoningContent string) (*models.ChatMessage, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	message := &models.ChatMessage{
		SessionID:        sessionId,
		Role:             role,
		Content:          content,
		ReasoningContent: reasoningContent,
		Timestamp:        time.Now().Unix(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.db.Create(message).Error; err != nil {
		return nil, fmt.Errorf("保存消息失败: %v", err)
	}
	return message, nil
}

// UpdateSessionTitle 更新会话标题。
func (s *ChatService) UpdateSessionTitle(sessionId uint, title string) error {
	fmt.Printf("[ChatDebug] Service: 开始更新会话标题，sessionId=%d, title=%s\n", sessionId, title)

	now := time.Now().Format("2006-01-02 15:04:05")
	result := s.db.Model(&models.ChatSession{}).Where("id = ?", sessionId).Updates(map[string]interface{}{
		"title":      title,
		"updated_at": now,
	})

	if result.Error != nil {
		fmt.Printf("[ChatDebug] Service: 更新会话标题失败: %v\n", result.Error)
		return fmt.Errorf("更新会话标题失败: %v", result.Error)
	}

	fmt.Printf("[ChatDebug] Service: 更新会话标题成功，sessionId=%d, 影响行数=%d\n", sessionId, result.RowsAffected)
	return nil
}

// UpdateSessionLastMessage 更新会话的最后一条消息。
func (s *ChatService) UpdateSessionLastMessage(sessionId uint, lastMessage string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if err := s.db.Model(&models.ChatSession{}).Where("id = ?", sessionId).Updates(map[string]interface{}{
		"last_message": lastMessage,
		"updated_at":   now,
	}).Error; err != nil {
		return fmt.Errorf("更新会话最后一条消息失败: %v", err)
	}
	return nil
}

// DeleteSession 删除会话。
func (s *ChatService) DeleteSession(sessionId uint) error {
	if err := s.db.Where("id = ?", sessionId).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("删除会话失败: %v", err)
	}
	return nil
}

// DeleteAllSessions 删除用户的所有会话。
func (s *ChatService) DeleteAllSessions(userId uint) error {
	if err := s.db.Where("session_id IN (SELECT id FROM chat_sessions WHERE user_id = ?)", userId).Delete(&models.ChatMessage{}).Error; err != nil {
		return fmt.Errorf("删除消息失败: %v", err)
	}
	if err := s.db.Where("user_id = ?", userId).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("删除会话失败: %v", err)
	}
	return nil
}

// StreamChat 流式聊天。
func (s *ChatService) StreamChat(sessionId uint, messages []models.AiMessage, tools []models.Tool) (<-chan string, <-chan error) {
	fmt.Printf("[ChatDebug] Service: 开始流式聊天，会话ID=%d\n", sessionId)

	if len(messages) > 0 {
		lastMessage := messages[len(messages)-1]
		if lastMessage.Role == "user" {
			_, err := s.SaveMessage(sessionId, "user", lastMessage.Content, "")
			if err != nil {
				fmt.Printf("[ChatDebug] Service: 保存用户消息失败: %v\n", err)
			} else {
				fmt.Printf("[ChatDebug] Service: 保存用户消息成功\n")
			}
		}
	}

	dataChan, errChan := s.aiService.SendStreamRequest(messages, tools)
	outChan := make(chan string)
	outErrChan := make(chan error)

	go func() {
		defer close(outChan)
		defer close(outErrChan)

		var lastResponse StreamResponse
		for {
			select {
			case content, ok := <-dataChan:
				if !ok {
					dataChan = nil
				} else {
					var resp StreamResponse
					if err := json.Unmarshal([]byte(content), &resp); err == nil {
						lastResponse = resp
					}
					outChan <- content
				}
			case err, ok := <-errChan:
				if !ok {
					errChan = nil
				} else {
					outErrChan <- err
					return
				}
			}
			if dataChan == nil && errChan == nil {
				break
			}
		}

		if len(lastResponse.Content) > 0 {
			_, err := s.SaveMessage(sessionId, "assistant", lastResponse.Content, lastResponse.ReasoningContent)
			if err != nil {
				fmt.Printf("[ChatDebug] Service: 保存 AI 消息失败: %v\n", err)
			}
			if err := s.UpdateSessionLastMessage(sessionId, lastResponse.Content); err != nil {
				fmt.Printf("[ChatDebug] Service: 更新会话最后一条消息失败: %v\n", err)
			}
		}
	}()

	return outChan, outErrChan
}

// GenerateTitle 生成会话标题。
func (s *ChatService) GenerateTitle(userMessage string) (string, error) {
	fmt.Printf("[ChatDebug] Service: 开始生成标题\n")

	title, err := s.aiService.GenerateTitle(userMessage)
	if err != nil {
		return "", fmt.Errorf("生成标题失败: %v", err)
	}

	fmt.Printf("[ChatDebug] Service: 标题生成成功: %s\n", title)
	return title, nil
}
