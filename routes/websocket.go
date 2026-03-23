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
		return true
	},
}

// WebSocketRoutes 注册 WebSocket 路由。
func WebSocketRoutes(r *gin.Engine, db *gorm.DB, aiService *service.AiService) {
	ws := r.Group("/ws")
	{
		ws.GET("/chat", func(c *gin.Context) {
			handleWebSocket(c, db)
		})
	}
}

func handleWebSocket(c *gin.Context, db *gorm.DB) {
	conn, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Println("[WebSocket] connected")

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[WebSocket] read message failed: %v", err)
			break
		}

		log.Printf("[WebSocket] recv(type=%d): %s", messageType, string(message))

		var chatRequest models.WebSocketChatRequest
		if err := json.Unmarshal(message, &chatRequest); err != nil {
			log.Printf("[WebSocket] decode request failed: %v", err)
			sendError(conn, "消息格式错误")
			continue
		}

		switch chatRequest.Type {
		case "chat":
			handleChatMessage(conn, db, &chatRequest)
		case "recover":
			handleRecoverMessage(conn, db, &chatRequest)
		default:
			sendError(conn, "未知的消息类型")
		}
	}
}

func handleChatMessage(conn *websocket.Conn, db *gorm.DB, request *models.WebSocketChatRequest) {
	log.Printf("[WebSocket] handle chat, sessionId=%d, userMessageId=%s", request.SessionID, request.UserMessageID)

	dynamicAiService := service.NewAiService(request.ApiKey, request.BaseUrl, request.ModelID)
	chatService := service.NewChatService(db, dynamicAiService, nil)

	userMessage, err := chatService.GetMessageByID(uint(request.SessionID), request.UserMessageID)
	if err != nil {
		log.Printf("[WebSocket] load user message failed: %v", err)
		sendError(conn, "未找到用户消息")
		return
	}

	contextMessages, err := chatService.GetAIContextMessages(uint(request.SessionID), request.UserMessageID, service.AIContextWindowSize)
	if err != nil {
		log.Printf("[WebSocket] load ai context failed: %v", err)
		sendError(conn, "读取上下文失败")
		return
	}

	aiMsg, err := chatService.CreateAssistantPlaceholder(uint(request.SessionID), request.AIMessageID)
	if err != nil {
		log.Printf("[WebSocket] create ai placeholder failed: %v", err)
		sendError(conn, "创建AI消息占位失败")
		return
	}

	tools := make([]models.Tool, 0)
	for _, tool := range request.Tools {
		if toolType, ok := tool["type"].(string); ok {
			tools = append(tools, models.Tool{Type: toolType})
		}
	}

	aiMessages := service.BuildAiMessagesFromBusinessMessages(contextMessages)
	dataChan, errChan := dynamicAiService.SendStreamRequest(aiMessages, tools)

	var fullContent string
	var fullReasoningContent string

	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				goto finish
			}

			var streamResp service.StreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				log.Printf("[WebSocket] decode stream response failed: %v", err)
				continue
			}

			fullContent = streamResp.Content
			fullReasoningContent = streamResp.ReasoningContent

			chatResponse := models.WebSocketChatResponse{
				Content:          fullContent,
				ReasoningContent: fullReasoningContent,
				IsReasoning:      streamResp.IsReasoning,
				IsEnd:            false,
				SessionID:        request.SessionID,
				AIMessageID:      request.AIMessageID,
			}

			jsonData, _ := json.Marshal(chatResponse)
			if err := conn.WriteMessage(websocket.TextMessage, jsonData); err != nil {
				log.Printf("[WebSocket] send message failed: %v", err)
				return
			}

		case err := <-errChan:
			if err != nil {
				log.Printf("[WebSocket] ai error: %v", err)
				sendError(conn, err.Error())
				return
			}
		}
	}

finish:
	sendDone(conn, request.SessionID, request.AIMessageID)

	db.Model(aiMsg).Updates(map[string]interface{}{
		"content":           fullContent,
		"reasoning_content": fullReasoningContent,
		"is_streaming":      false,
		"is_reasoning":      false,
		"updated_at":        time.Now().Format("2006-01-02 15:04:05"),
	})

	db.Model(&models.ChatSession{}).Where("id = ?", request.SessionID).Updates(map[string]interface{}{
		"last_message": fullContent,
		"update_time":  time.Now().Unix(),
	})

	log.Printf("[WebSocket] finished, sessionId=%d, userMessageId=%s", request.SessionID, userMessage.ID)
}

func handleRecoverMessage(conn *websocket.Conn, db *gorm.DB, request *models.WebSocketChatRequest) {
	log.Printf("[WebSocket] handle recover, sessionId=%d", request.SessionID)

	var aiMsg models.ChatMessage
	result := db.Where("session_id = ? AND role = ? AND is_streaming = ?", uint(request.SessionID), "assistant", true).First(&aiMsg)
	if result.Error != nil {
		log.Printf("[WebSocket] no streaming message found: %v", result.Error)
		sendError(conn, "没有找到正在流式输出的消息")
		return
	}

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

	db.Model(&aiMsg).Updates(map[string]interface{}{
		"is_streaming": false,
		"is_reasoning": false,
	})

	log.Printf("[WebSocket] recover finished, sessionId=%d", request.SessionID)
}

func sendError(conn *websocket.Conn, errorMsg string) {
	errorMessage := fmt.Sprintf("[ERROR] %s", errorMsg)
	conn.WriteMessage(websocket.TextMessage, []byte(errorMessage))
}

func sendDone(conn *websocket.Conn, sessionID int64, aiMessageID string) {
	doneResponse := map[string]interface{}{
		"type":          "done",
		"session_id":    sessionID,
		"ai_message_id": aiMessageID,
	}
	jsonData, _ := json.Marshal(doneResponse)
	conn.WriteMessage(websocket.TextMessage, jsonData)
}
