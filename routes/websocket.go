package routes

import (
	"ai-chat/models"
	"ai-chat/service"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var upGrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

// WebSocketRoutes 注册 WebSocket 路由
func WebSocketRoutes(r *gin.Engine, db *gorm.DB, aiService *service.AiService) {
	ws := r.Group("/ws")
	{
		ws.GET("/chat", func(c *gin.Context) {
			handleWebSocket(c, db, aiService)
		})
	}
}

// handleWebSocket 处理 WebSocket 连接
func handleWebSocket(c *gin.Context, db *gorm.DB, aiService *service.AiService) {
	// 升级 HTTP 连接为 WebSocket
	conn, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] 升级失败: %v", err)
		return
	}
	defer conn.Close()

	log.Println("[WebSocket] 连接已建立")

	// 持续监听消息
	for {
		// 读取消息
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[WebSocket] 读取消息失败: %v", err)
			break
		}

		log.Printf("[WebSocket] 收到消息 (type=%d): %s", messageType, string(message))

		// 解析消息
		var chatRequest models.WebSocketChatRequest
		if err := json.Unmarshal(message, &chatRequest); err != nil {
			log.Printf("[WebSocket] 解析消息失败: %v", err)
			sendError(conn, "消息格式错误")
			continue
		}

		// 根据消息类型处理
		switch chatRequest.Type {
		case "chat":
			handleChatMessage(conn, db, aiService, &chatRequest)
		case "recover":
			handleRecoverMessage(conn, db, aiService, &chatRequest)
		default:
			sendError(conn, "未知的消息类型")
		}
	}
}

// handleChatMessage 处理聊天消息
func handleChatMessage(conn *websocket.Conn, db *gorm.DB, aiService *service.AiService, request *models.WebSocketChatRequest) {
	log.Printf("[WebSocket] 处理聊天消息，会话ID=%d", request.SessionID)

	// 1. 获取最后一条用户消息
	var lastUserMessage string
	if len(request.Messages) > 0 {
		lastMsg := request.Messages[len(request.Messages)-1]
		if content, ok := lastMsg["content"].(string); ok {
			lastUserMessage = content
		}
	}

	// 2. 保存用户消息到数据库（从 messages 中获取 ID）
	var userMsgID string
	if len(request.Messages) > 0 {
		lastMsg := request.Messages[len(request.Messages)-1]
		if id, ok := lastMsg["id"].(string); ok {
			userMsgID = id
		}
	}

	now := time.Now()
	nowString := now.Format("2006-01-02 15:04:05")
	userMsg := models.ChatMessage{
		ID:        userMsgID, // 使用前端生成的 ID（从 messages 中获取）
		SessionID: uint(request.SessionID),
		Role:      "user",
		Content:   lastUserMessage,
		Timestamp: now.Unix(),
		CreatedAt: nowString,
		UpdatedAt: nowString,
	}
	if err := db.Create(&userMsg).Error; err != nil {
		log.Printf("[WebSocket] 保存用户消息失败: %v", err)
		sendError(conn, "保存用户消息失败")
		return
	}

	// 3. 创建 AI 消息占位（使用前端生成的 ID）
	aiMsg := models.ChatMessage{
		ID:          request.AIMessageID, // 使用前端生成的 ID
		SessionID:   uint(request.SessionID),
		Role:        "assistant",
		Content:     "",
		Timestamp:   now.Unix(),
		CreatedAt:   nowString,
		UpdatedAt:   nowString,
		IsStreaming: true,
	}
	if err := db.Create(&aiMsg).Error; err != nil {
		log.Printf("[WebSocket] 创建AI消息占位失败: %v", err)
		sendError(conn, "创建AI消息占位失败")
		return
	}

	// 4. 将 messages 转换为 AI 服务需要的格式
	messages := make([]models.AiMessage, len(request.Messages))
	for i, msg := range request.Messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		messages[i] = models.AiMessage{
			Role:    role,
			Content: content,
		}
	}

	// 5. 转换 tools 格式
	tools := make([]models.Tool, 0)
	for _, tool := range request.Tools {
		if toolType, ok := tool["type"].(string); ok {
			tools = append(tools, models.Tool{Type: toolType})
		}
	}

	// 6. 动态创建 AiService
	dynamicAiService := service.NewAiService(request.ApiKey, request.BaseUrl, request.ModelID)

	// 7. 调用 AI 服务（流式）
	dataChan, errChan := dynamicAiService.SendStreamRequest(messages, tools)

	// 8. 流式发送响应
	var fullContent string
	var fullReasoningContent string

	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				// 数据通道关闭，流式结束
				goto finish
			}

			// 解析响应
			var streamResp service.StreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				log.Printf("[WebSocket] 解析流式响应失败: %v", err)
				continue
			}

			// 累加内容
			fullContent = streamResp.Content
			fullReasoningContent = streamResp.ReasoningContent

			// 构建响应
			chatResponse := models.WebSocketChatResponse{
				Content:          fullContent,
				ReasoningContent: fullReasoningContent,
				IsReasoning:      streamResp.IsReasoning,
				IsEnd:            false,
				SessionID:        request.SessionID,   // 包含会话 ID 用于前端路由
				AIMessageID:      request.AIMessageID, // 包含 AI 消息 ID 用于前端路由
			}

			// 发送 JSON 响应
			jsonData, _ := json.Marshal(chatResponse)
			if err := conn.WriteMessage(websocket.TextMessage, jsonData); err != nil {
				log.Printf("[WebSocket] 发送消息失败: %v", err)
				return
			}

		case err := <-errChan:
			if err != nil {
				log.Printf("[WebSocket] AI 服务错误: %v", err)
				sendError(conn, err.Error())
				return
			}
		}
	}

finish:
	// 9. 流式完成，发送 [DONE] 消息（包含 session_id 和 ai_message_id）
	// 注意：不再发送 is_end=true 的响应，统一使用 type=done 消息表示完成
	sendDone(conn, request.SessionID, request.AIMessageID)

	// 更新数据库中的 AI 消息
	db.Model(&aiMsg).Updates(map[string]interface{}{
		"content":           fullContent,
		"reasoning_content": fullReasoningContent,
		"is_streaming":      false,
		"is_reasoning":      false,
		"updated_at":        time.Now().Format("2006-01-02 15:04:05"),
	})

	// 更新会话的最后消息和时间
	db.Model(&models.ChatSession{}).Where("id = ?", request.SessionID).Updates(map[string]interface{}{
		"last_message": fullContent,
		"update_time":  time.Now().Unix(),
	})

	log.Printf("[WebSocket] 聊天完成，会话ID=%d", request.SessionID)
}

// handleRecoverMessage 处理恢复会话消息
func handleRecoverMessage(conn *websocket.Conn, db *gorm.DB, aiService *service.AiService, request *models.WebSocketChatRequest) {
	log.Printf("[WebSocket] 处理恢复会话，会话ID=%d", request.SessionID)

	// 1. 查找正在流式输出的消息
	var aiMsg models.ChatMessage
	result := db.Where("session_id = ? AND role = ? AND is_streaming = ?", uint(request.SessionID), "assistant", true).First(&aiMsg)
	if result.Error != nil {
		log.Printf("[WebSocket] 没有找到正在流式输出的消息: %v", result.Error)
		sendError(conn, "没有找到正在流式输出的消息")
		return
	}

	// 2. 发送已有的内容
	chatResponse := models.WebSocketChatResponse{
		Content:          aiMsg.Content,
		ReasoningContent: aiMsg.ReasoningContent,
		IsReasoning:      aiMsg.IsReasoning,
		IsEnd:            false,
		SessionID:        request.SessionID,
		AIMessageID:      aiMsg.ID,
	}
	jsonData, _ := json.Marshal(chatResponse)
	conn.WriteMessage(websocket.TextMessage, jsonData)

	// 3. TODO: 继续流式输出（需要从上次中断的地方继续）
	// 这里需要根据实际的 AI 服务实现来处理
	// 可能需要保存更多的状态信息，比如当前的流式位置等

	// 暂时标记为完成
	chatResponse = models.WebSocketChatResponse{
		Content:          aiMsg.Content,
		ReasoningContent: aiMsg.ReasoningContent,
		IsReasoning:      false,
		IsEnd:            true,
		SessionID:        request.SessionID,
		AIMessageID:      aiMsg.ID,
	}
	jsonData, _ = json.Marshal(chatResponse)
	conn.WriteMessage(websocket.TextMessage, jsonData)
	sendDone(conn, request.SessionID, aiMsg.ID)

	// 更新数据库中的 AI 消息
	db.Model(&aiMsg).Updates(map[string]interface{}{
		"is_streaming": false,
		"is_reasoning": false,
	})

	log.Printf("[WebSocket] 恢复会话完成，会话ID=%d", request.SessionID)
}

// sendError 发送错误消息
func sendError(conn *websocket.Conn, errorMsg string) {
	errorMessage := fmt.Sprintf("[ERROR] %s", errorMsg)
	conn.WriteMessage(websocket.TextMessage, []byte(errorMessage))
}

// sendDone 发送完成消息（包含 session_id 和 ai_message_id）
func sendDone(conn *websocket.Conn, sessionID int64, aiMessageID string) {
	doneResponse := map[string]interface{}{
		"type":          "done",
		"session_id":    sessionID,
		"ai_message_id": aiMessageID,
	}
	jsonData, _ := json.Marshal(doneResponse)
	conn.WriteMessage(websocket.TextMessage, jsonData)
}
