package service

import (
	"ai-chat/models"
	"log"
	"strings"
)

// BuildAiMessagesFromBusinessMessages 把业务消息转换成 chat/completions 可接受的消息结构。
// 当前策略是：用户消息如果带图片附件，则把 content 转成多模态内容块数组。
func BuildAiMessagesFromBusinessMessages(messages []models.BusinessMessageInput) []models.AiChatMessage {
	result := make([]models.AiChatMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, buildAiMessageFromBusinessMessage(message))
	}
	return result
}

func buildAiMessageFromBusinessMessage(message models.BusinessMessageInput) models.AiChatMessage {
	if message.Role != "user" || len(message.Attachments) == 0 {
		return models.AiChatMessage{
			Role:    message.Role,
			Content: message.Content,
		}
	}

	parts := make([]models.AiContentPart, 0, len(message.Attachments)+1)
	if strings.TrimSpace(message.Content) != "" {
		parts = append(parts, models.AiContentPart{
			Type: "text",
			Text: message.Content,
		})
	}

	for _, attachment := range message.Attachments {
		if attachment.Type != "image" {
			continue
		}
		if strings.TrimSpace(attachment.RemoteURL) == "" {
			log.Printf("[AiBuilder] skip attachment without remote_url, messageId=%s", message.ID)
			continue
		}
		parts = append(parts, models.AiContentPart{
			Type: "image_url",
			ImageURL: &models.AiImageURL{
				URL: attachment.RemoteURL,
			},
		})
	}

	if len(parts) == 0 {
		return models.AiChatMessage{
			Role:    message.Role,
			Content: message.Content,
		}
	}

	return models.AiChatMessage{
		Role:    message.Role,
		Content: parts,
	}
}
