package service

import (
	"ai-chat/models"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"time"

	"gorm.io/gorm"
)

// ChatService 负责会话、消息以及附件消息的业务逻辑。
type ChatService struct {
	db         *gorm.DB
	aiService  *AiService
	ossService *OSSService
}

const (
	DefaultMessageHistoryPageSize = 20
	MaxMessageHistoryPageSize     = 100
	AIContextWindowSize           = 20
)

type MessageHistoryPage struct {
	Messages            []models.ChatMessageDTO `json:"messages"`
	HasMore             bool                    `json:"has_more"`
	NextBeforeMessageID *string                 `json:"next_before_message_id"`
}

func NewChatService(db *gorm.DB, aiService *AiService, ossService *OSSService) *ChatService {
	return &ChatService{
		db:         db,
		aiService:  aiService,
		ossService: ossService,
	}
}

func (s *ChatService) GetOSSService() *OSSService {
	return s.ossService
}

func (s *ChatService) CreateSession(userID uint, title string) (*models.ChatSession, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	session := &models.ChatSession{
		Title:      title,
		UserID:     userID,
		Status:     models.SessionStatusDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
		UpdateTime: time.Now().Unix(),
	}
	if err := s.db.Create(session).Error; err != nil {
		return nil, fmt.Errorf("create session failed: %w", err)
	}
	return session, nil
}

func (s *ChatService) GetSessions(userID uint) ([]models.ChatSession, error) {
	var sessions []models.ChatSession
	if err := s.db.Where("user_id = ? AND status = ?", userID, models.SessionStatusActive).Order("updated_at DESC").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("get sessions failed: %w", err)
	}
	return sessions, nil
}

func (s *ChatService) GetSessionMessagesPage(userID, sessionID uint, beforeMessageID string, limit int) (*MessageHistoryPage, error) {
	if limit <= 0 {
		limit = DefaultMessageHistoryPageSize
	}
	if limit > MaxMessageHistoryPageSize {
		limit = MaxMessageHistoryPageSize
	}

	if _, err := s.GetOwnedSession(userID, sessionID); err != nil {
		return nil, err
	}

	query := s.db.Model(&models.ChatMessage{}).Where("session_id = ?", sessionID)
	if beforeMessageID != "" {
		var anchor models.ChatMessage
		if err := s.db.Where("session_id = ? AND id = ?", sessionID, beforeMessageID).First(&anchor).Error; err != nil {
			return nil, fmt.Errorf("get history anchor failed: %w", err)
		}
		query = query.Where("(timestamp < ?) OR (timestamp = ? AND id < ?)", anchor.Timestamp, anchor.Timestamp, anchor.ID)
	}

	var descending []models.ChatMessage
	if err := query.Order("timestamp DESC").Order("id DESC").Limit(limit + 1).Find(&descending).Error; err != nil {
		return nil, fmt.Errorf("get messages page failed: %w", err)
	}

	hasMore := len(descending) > limit
	if hasMore {
		descending = descending[:limit]
	}

	ascending := reverseMessages(descending)
	messages := make([]models.ChatMessageDTO, 0, len(ascending))
	for _, message := range ascending {
		messages = append(messages, toChatMessageDTO(message))
	}

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

func reverseMessages(messages []models.ChatMessage) []models.ChatMessage {
	reversed := make([]models.ChatMessage, len(messages))
	for i := range messages {
		reversed[len(messages)-1-i] = messages[i]
	}
	return reversed
}

// GetOwnedSession 校验会话是否属于当前用户。
func (s *ChatService) GetOwnedSession(userID, sessionID uint) (*models.ChatSession, error) {
	var session models.ChatSession
	if err := s.db.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return nil, fmt.Errorf("session not found")
	}
	return &session, nil
}

// GetMessageByID 查询一条已落库的消息。
func (s *ChatService) GetMessageByID(sessionID uint, messageID string) (*models.ChatMessage, error) {
	var message models.ChatMessage
	if err := s.db.Where("session_id = ? AND id = ?", sessionID, messageID).First(&message).Error; err != nil {
		return nil, fmt.Errorf("message not found")
	}
	return &message, nil
}

// GetAIContextMessages 读取某条消息之前（含该消息）的上下文窗口。
func (s *ChatService) GetAIContextMessages(sessionID uint, anchorMessageID string, limit int) ([]models.BusinessMessageInput, error) {
	if limit <= 0 {
		limit = AIContextWindowSize
	}

	anchor, err := s.GetMessageByID(sessionID, anchorMessageID)
	if err != nil {
		return nil, err
	}

	var descending []models.ChatMessage
	if err := s.db.Where(
		"session_id = ? AND ((timestamp < ?) OR (timestamp = ? AND id <= ?))",
		sessionID,
		anchor.Timestamp,
		anchor.Timestamp,
		anchor.ID,
	).Order("timestamp DESC").Order("id DESC").Limit(limit).Find(&descending).Error; err != nil {
		return nil, fmt.Errorf("get ai context failed: %w", err)
	}

	ascending := reverseMessages(descending)
	result := make([]models.BusinessMessageInput, 0, len(ascending))
	for _, message := range ascending {
		result = append(result, toBusinessMessageInput(message))
	}
	return result, nil
}

func (s *ChatService) SaveMessage(sessionID uint, role, content, reasoningContent string) (*models.ChatMessage, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	message := &models.ChatMessage{
		ID:               fmt.Sprintf("%d", time.Now().UnixNano()),
		SessionID:        sessionID,
		Role:             role,
		Content:          content,
		MessageType:      "text",
		AttachmentsJSON:  "[]",
		ReasoningContent: reasoningContent,
		Timestamp:        time.Now().UnixMilli(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.db.Create(message).Error; err != nil {
		return nil, fmt.Errorf("save message failed: %w", err)
	}
	return message, nil
}

// CreateAssistantPlaceholder 创建一条流式中的 assistant 占位消息。
func (s *ChatService) CreateAssistantPlaceholder(sessionID uint, aiMessageID string) (*models.ChatMessage, error) {
	now := time.Now()
	nowString := now.Format("2006-01-02 15:04:05")
	message := &models.ChatMessage{
		ID:               aiMessageID,
		SessionID:        sessionID,
		Role:             "assistant",
		Content:          "",
		MessageType:      "text",
		AttachmentsJSON:  "[]",
		ReasoningContent: "",
		Timestamp:        now.UnixMilli(),
		CreatedAt:        nowString,
		UpdatedAt:        nowString,
		IsStreaming:      true,
	}
	if err := s.db.Create(message).Error; err != nil {
		return nil, fmt.Errorf("create ai placeholder failed: %w", err)
	}
	return message, nil
}

// UploadAttachment 把控制器层收到的附件继续转给 OSSService 处理。
func (s *ChatService) UploadAttachment(
	ctx context.Context,
	userID uint,
	sessionID uint,
	fileHeader *multipart.FileHeader,
) (*UploadedAttachment, error) {
	if s.ossService == nil {
		return nil, fmt.Errorf("oss service not configured")
	}
	return s.ossService.UploadImage(ctx, userID, sessionID, fileHeader)
}

// CreateUserMessage 保存正式用户消息，当前支持 text / mixed 两种消息类型。
func (s *ChatService) CreateUserMessage(
	userID uint,
	sessionID uint,
	messageID string,
	text string,
	attachments []models.AttachmentItem,
) (*models.ChatMessageDTO, error) {
	session, err := s.GetOwnedSession(userID, sessionID)
	if err != nil {
		return nil, err
	}
	if err := validateAttachments(attachments); err != nil {
		return nil, err
	}

	now := time.Now()
	nowText := now.Format("2006-01-02 15:04:05")
	messageType := "text"
	if len(attachments) > 0 {
		messageType = "mixed"
	}

	attachmentsJSON, err := json.Marshal(attachments)
	if err != nil {
		return nil, fmt.Errorf("marshal attachments failed: %w", err)
	}

	message := &models.ChatMessage{
		ID:               messageID,
		SessionID:        sessionID,
		Role:             "user",
		Content:          text,
		MessageType:      messageType,
		AttachmentsJSON:  string(attachmentsJSON),
		ReasoningContent: "",
		Timestamp:        now.UnixMilli(),
		CreatedAt:        nowText,
		UpdatedAt:        nowText,
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return fmt.Errorf("create user message failed: %w", err)
		}

		updateFields := map[string]interface{}{
			"last_message": buildMessageSummary(text, attachments),
			"updated_at":   nowText,
			"update_time":  time.Now().Unix(),
		}
		if session.Status == models.SessionStatusDraft {
			updateFields["status"] = models.SessionStatusActive
			updateFields["title"] = buildSessionTitle(text, attachments)
		}

		if err := tx.Model(&models.ChatSession{}).Where("id = ?", sessionID).Updates(updateFields).Error; err != nil {
			return fmt.Errorf("update session after user message failed: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	dto := toChatMessageDTO(*message)
	return &dto, nil
}

func (s *ChatService) UpdateSessionTitle(sessionID uint, title string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if err := s.db.Model(&models.ChatSession{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"title":       title,
		"updated_at":  now,
		"update_time": time.Now().Unix(),
	}).Error; err != nil {
		return fmt.Errorf("update title failed: %w", err)
	}
	return nil
}

func (s *ChatService) UpdateSessionLastMessage(sessionID uint, lastMessage string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if err := s.db.Model(&models.ChatSession{}).Where("id = ?", sessionID).Updates(map[string]interface{}{
		"last_message": lastMessage,
		"updated_at":   now,
		"update_time":  time.Now().Unix(),
	}).Error; err != nil {
		return fmt.Errorf("update last message failed: %w", err)
	}
	return nil
}

func (s *ChatService) DeleteSession(userID uint, sessionID uint) error {
	if _, err := s.GetOwnedSession(userID, sessionID); err != nil {
		return err
	}
	if err := s.db.Where("session_id = ?", sessionID).Delete(&models.ChatMessage{}).Error; err != nil {
		return fmt.Errorf("delete session messages failed: %w", err)
	}
	if err := s.db.Where("id = ? AND user_id = ?", sessionID, userID).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("delete session failed: %w", err)
	}
	return nil
}

func (s *ChatService) DeleteAllSessions(userID uint) error {
	if err := s.db.Where("session_id IN (SELECT id FROM chat_sessions WHERE user_id = ?)", userID).Delete(&models.ChatMessage{}).Error; err != nil {
		return fmt.Errorf("delete messages failed: %w", err)
	}
	if err := s.db.Where("user_id = ?", userID).Delete(&models.ChatSession{}).Error; err != nil {
		return fmt.Errorf("delete sessions failed: %w", err)
	}
	return nil
}

// StreamChat 仅负责基于已落库上下文触发 AI，不再重复保存用户消息。
func (s *ChatService) StreamChat(sessionID uint, messages []models.BusinessMessageInput, tools []models.Tool) (<-chan string, <-chan error) {
	aiMessages := BuildAiMessagesFromBusinessMessages(messages)
	dataChan, errChan := s.aiService.SendStreamRequest(aiMessages, tools)
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

		if lastResponse.Content != "" {
			if _, err := s.SaveMessage(sessionID, "assistant", lastResponse.Content, lastResponse.ReasoningContent); err == nil {
				_ = s.UpdateSessionLastMessage(sessionID, lastResponse.Content)
			}
		}
	}()

	return outChan, outErrChan
}

func (s *ChatService) GenerateTitle(userMessage string) (string, error) {
	title, err := s.aiService.GenerateTitle(userMessage)
	if err != nil {
		return "", fmt.Errorf("generate title failed: %w", err)
	}
	return title, nil
}

// toChatMessageDTO 把数据库中的消息模型转换成接口返回结构。
func toChatMessageDTO(message models.ChatMessage) models.ChatMessageDTO {
	attachments := make([]models.AttachmentItem, 0)
	if stringsValue := message.AttachmentsJSON; stringsValue != "" {
		_ = json.Unmarshal([]byte(stringsValue), &attachments)
	}
	return models.ChatMessageDTO{
		ID:               message.ID,
		SessionID:        message.SessionID,
		Role:             message.Role,
		Content:          message.Content,
		MessageType:      fallbackString(message.MessageType, "text"),
		Attachments:      attachments,
		ReasoningContent: message.ReasoningContent,
		Timestamp:        message.Timestamp,
		CreatedAt:        message.CreatedAt,
		UpdatedAt:        message.UpdatedAt,
	}
}

func toBusinessMessageInput(message models.ChatMessage) models.BusinessMessageInput {
	attachments := make([]models.AttachmentItem, 0)
	if message.AttachmentsJSON != "" {
		_ = json.Unmarshal([]byte(message.AttachmentsJSON), &attachments)
	}
	return models.BusinessMessageInput{
		ID:          message.ID,
		Role:        message.Role,
		Content:     message.Content,
		MessageType: fallbackString(message.MessageType, "text"),
		Attachments: attachments,
	}
}

// buildMessageSummary 用于更新会话列表里的最后一条摘要。
func buildMessageSummary(text string, attachments []models.AttachmentItem) string {
	if text != "" {
		return text
	}
	if len(attachments) == 1 && attachments[0].Type == "image" {
		return "[图片]"
	}
	if len(attachments) > 1 {
		return fmt.Sprintf("[%d个附件]", len(attachments))
	}
	if len(attachments) == 1 {
		return "[附件]"
	}
	return ""
}

func buildSessionTitle(text string, attachments []models.AttachmentItem) string {
	if text != "" {
		runes := []rune(text)
		if len(runes) > 20 {
			return string(runes[:20])
		}
		return text
	}
	if len(attachments) > 0 {
		if len(attachments) == 1 && attachments[0].Type == "image" {
			return "图片消息"
		}
		return "新会话"
	}
	return "新会话"
}

func fallbackString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// validateAttachments 对附件元数据做最小校验，避免前端伪造不完整附件直接入库。
func validateAttachments(attachments []models.AttachmentItem) error {
	for _, attachment := range attachments {
		if attachment.ID == "" {
			return fmt.Errorf("attachment id is required")
		}
		if attachment.Type != "image" {
			return fmt.Errorf("unsupported attachment type: %s", attachment.Type)
		}
		if attachment.RemoteURL == "" || attachment.ObjectKey == "" {
			return fmt.Errorf("attachment upload info is incomplete")
		}
	}
	return nil
}
