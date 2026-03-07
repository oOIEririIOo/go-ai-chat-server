package service

import (
	"ai-chat/models"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ChatService 聊天服务
type ChatService struct {
	db        *gorm.DB
	aiService *AiService
}

// NewChatService 创建聊天服务实例
func NewChatService(db *gorm.DB, aiService *AiService) *ChatService {
	return &ChatService{
		db:        db,
		aiService: aiService,
	}
}

// CreateSession 创建聊天会话
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

// GetSessions 获取用户的所有会话
func (s *ChatService) GetSessions(userId uint) ([]models.ChatSession, error) {
	var sessions []models.ChatSession
	if err := s.db.Where("user_id = ?", userId).Order("updated_at DESC").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("获取会话列表失败: %v", err)
	}
	return sessions, nil
}

// GetSession 获取单个会话
func (s *ChatService) GetSession(sessionId uint) (*models.ChatSession, error) {
	fmt.Printf("[ChatDebug] Service: 获取会话，ID=%d\n", sessionId)
	var session models.ChatSession
	if err := s.db.Preload("Messages").First(&session, sessionId).Error; err != nil {
		return nil, fmt.Errorf("获取会话失败: %v", err)
	}
	fmt.Printf("[ChatDebug] Service: 获取会话成功，消息数量=%d\n", len(session.Messages))
	return &session, nil
}

// SaveMessage 保存消息
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

// UpdateSessionTitle 更新会话标题
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

// UpdateSessionLastMessage 更新会话的最后一条消息
func (s *ChatService) UpdateSessionLastMessage(sessionId uint, lastMessage string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if err := s.db.Model(&models.ChatSession{}).Where("id = ?", sessionId).Updates(map[string]interface{}{
		"last_message": lastMessage,
		"updated_at":   now,
	}).Error; err != nil {
		return fmt.Errorf("更新会话最后消息失败: %v", err)
	}
	return nil
}

// DeleteSession 删除会话
func (s *ChatService) DeleteSession(sessionId uint) error {
	if err := s.db.Where("id = ?", sessionId).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("删除会话失败: %v", err)
	}
	return nil
}

// DeleteAllSessions 删除用户的所有会话
func (s *ChatService) DeleteAllSessions(userId uint) error {
	// 先删除该用户所有会话的消息
	if err := s.db.Where("session_id IN (SELECT id FROM chat_sessions WHERE user_id = ?)", userId).Delete(&models.ChatMessage{}).Error; err != nil {
		return fmt.Errorf("删除消息失败: %v", err)
	}
	// 再删除会话
	if err := s.db.Where("user_id = ?", userId).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("删除会话失败: %v", err)
	}
	return nil
}

// StreamChat 流式聊天
func (s *ChatService) StreamChat(sessionId uint, messages []models.AiMessage, tools []models.Tool) (<-chan string, <-chan error) {
	fmt.Printf("[ChatDebug] Service: 开始流式聊天，会话ID=%d\n", sessionId)

	// 保存用户消息到数据库（最后一条消息是用户的新消息）
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

	// 创建新的 channel 用于返回给调用者
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
					// 解析JSON获取最新的内容和思考内容
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

		// 保存完整消息到数据库（使用解析后的content和reasoningContent）
		if len(lastResponse.Content) > 0 {
			_, err := s.SaveMessage(sessionId, "assistant", lastResponse.Content, lastResponse.ReasoningContent)
			if err != nil {
				fmt.Printf("[ChatDebug] Service: 保存AI消息失败: %v\n", err)
			}
			// 更新会话的最后一条消息
			if err := s.UpdateSessionLastMessage(sessionId, lastResponse.Content); err != nil {
				fmt.Printf("[ChatDebug] Service: 更新会话最后消息失败: %v\n", err)
			}
		}
	}()

	return outChan, outErrChan
}

// GenerateTitle 生成会话标题
func (s *ChatService) GenerateTitle(userMessage string) (string, error) {
	fmt.Printf("[ChatDebug] Service: 开始生成标题\n")

	title, err := s.aiService.GenerateTitle(userMessage)
	if err != nil {
		return "", fmt.Errorf("生成标题失败: %v", err)
	}

	fmt.Printf("[ChatDebug] Service: 标题生成成功: %s\n", title)
	return title, nil
}
