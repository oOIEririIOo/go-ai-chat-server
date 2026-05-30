package routes

import (
	"ai-chat/config"
	"ai-chat/models"
	"ai-chat/service"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 30 * time.Second
	wsPingPeriod = 20 * time.Second
)

var (
	upGrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	nextWSConnectionID uint64
)

type wsConnection struct {
	id             uint64
	conn           *websocket.Conn
	db             *gorm.DB
	writeMu        sync.Mutex
	sessionMu      sync.Mutex
	activeSessions map[int64]struct{}
	done           chan struct{}
	closeOnce      sync.Once
}

func newWSConnection(conn *websocket.Conn, db *gorm.DB) *wsConnection {
	return &wsConnection{
		id:             atomic.AddUint64(&nextWSConnectionID, 1),
		conn:           conn,
		db:             db,
		activeSessions: make(map[int64]struct{}),
		done:           make(chan struct{}),
	}
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

	client := newWSConnection(conn, db)
	defer client.shutdown()

	client.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	client.conn.SetPongHandler(func(appData string) error {
		log.Printf("[WebSocket] conn=%d recv pong", client.id)
		return client.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	log.Printf("[WebSocket] connected conn=%d", client.id)
	go client.startPingLoop()

	for {
		messageType, message, err := client.conn.ReadMessage()
		if err != nil {
			logWebSocketDisconnect(fmt.Sprintf("conn=%d read message failed", client.id), err)
			return
		}

		client.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		log.Printf("[WebSocket] conn=%d recv(type=%d): %s", client.id, messageType, string(message))

		var chatRequest models.WebSocketChatRequest
		if err := json.Unmarshal(message, &chatRequest); err != nil {
			log.Printf("[WebSocket] conn=%d decode request failed: %v", client.id, err)
			if err := client.sendError("消息格式错误"); err != nil {
				logWebSocketDisconnect(fmt.Sprintf("conn=%d send error failed", client.id), err)
				return
			}
			continue
		}

		requestCopy := chatRequest
		switch requestCopy.Type {
		case "chat":
			go client.handleChatMessage(&requestCopy)
		case "recover":
			go client.handleRecoverMessage(&requestCopy)
		default:
			if err := client.sendError("未知的消息类型"); err != nil {
				logWebSocketDisconnect(fmt.Sprintf("conn=%d send error failed", client.id), err)
				return
			}
		}
	}
}

func (c *wsConnection) handleChatMessage(request *models.WebSocketChatRequest) {
	if !c.tryStartSession(request.SessionID) {
		log.Printf("[WebSocket] conn=%d reject duplicate chat, sessionId=%d", c.id, request.SessionID)
		if err := c.sendError("当前会话正在回复中，请稍后再试"); err != nil {
			logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d send busy error failed", c.id, request.SessionID), err)
		}
		return
	}
	defer c.finishSession(request.SessionID)

	log.Printf("[WebSocket] conn=%d chat start, sessionId=%d, userMessageId=%s", c.id, request.SessionID, request.UserMessageID)

	modelCfg, ok := resolveWebSocketModelConfig(request)
	if !ok {
		c.writeErrorOrDisconnect(request.SessionID, "model_key 无效或后端未配置完整模型凭据")
		return
	}

	dynamicAiService := service.NewAiService(
		modelCfg.ApiKey,
		modelCfg.BaseURL,
		modelCfg.ModelID,
	)
	chatService := service.NewChatService(c.db, dynamicAiService, nil)

	userMessage, err := chatService.GetMessageByID(uint(request.SessionID), request.UserMessageID)
	if err != nil {
		log.Printf("[WebSocket] conn=%d session=%d load user message failed: %v", c.id, request.SessionID, err)
		c.writeErrorOrDisconnect(request.SessionID, "未找到用户消息")
		return
	}

	contextMessages, err := chatService.GetAIContextMessages(uint(request.SessionID), request.UserMessageID, service.AIContextWindowSize)
	if err != nil {
		log.Printf("[WebSocket] conn=%d session=%d load ai context failed: %v", c.id, request.SessionID, err)
		c.writeErrorOrDisconnect(request.SessionID, "读取上下文失败")
		return
	}

	aiMsg, err := chatService.CreateAssistantPlaceholder(uint(request.SessionID), request.AIMessageID)
	if err != nil {
		log.Printf("[WebSocket] conn=%d session=%d create ai placeholder failed: %v", c.id, request.SessionID, err)
		c.writeErrorOrDisconnect(request.SessionID, "创建AI消息占位失败")
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
				log.Printf("[WebSocket] conn=%d session=%d decode stream response failed: %v", c.id, request.SessionID, err)
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

			if err := c.writeJSON(chatResponse); err != nil {
				logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d send message failed", c.id, request.SessionID), err)
				return
			}

		case err := <-errChan:
			if err != nil {
				log.Printf("[WebSocket] conn=%d session=%d ai error: %v", c.id, request.SessionID, err)
				c.writeErrorOrDisconnect(request.SessionID, err.Error())
				return
			}
		}
	}

finish:
	if err := c.sendDone(request.SessionID, request.AIMessageID); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d send done failed", c.id, request.SessionID), err)
		return
	}

	c.db.Model(aiMsg).Updates(map[string]interface{}{
		"content":           fullContent,
		"reasoning_content": fullReasoningContent,
		"is_streaming":      false,
		"is_reasoning":      false,
		"updated_at":        time.Now().Format("2006-01-02 15:04:05"),
	})

	c.db.Model(&models.ChatSession{}).Where("id = ?", request.SessionID).Updates(map[string]interface{}{
		"last_message": fullContent,
		"update_time":  time.Now().Unix(),
	})

	log.Printf("[WebSocket] conn=%d chat finish, sessionId=%d, userMessageId=%s", c.id, request.SessionID, userMessage.ID)
}

func (c *wsConnection) handleRecoverMessage(request *models.WebSocketChatRequest) {
	log.Printf("[WebSocket] conn=%d recover start, sessionId=%d", c.id, request.SessionID)

	var aiMsg models.ChatMessage
	result := c.db.Where("session_id = ? AND role = ? AND is_streaming = ?", uint(request.SessionID), "assistant", true).First(&aiMsg)
	if result.Error != nil {
		log.Printf("[WebSocket] conn=%d session=%d no streaming message found: %v", c.id, request.SessionID, result.Error)
		c.writeErrorOrDisconnect(request.SessionID, "没有找到正在流式输出的消息")
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
	if err := c.writeJSON(chatResponse); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d recover send content failed", c.id, request.SessionID), err)
		return
	}

	chatResponse = models.WebSocketChatResponse{
		Content:          aiMsg.Content,
		ReasoningContent: aiMsg.ReasoningContent,
		IsReasoning:      false,
		IsEnd:            true,
		SessionID:        request.SessionID,
		AIMessageID:      aiMsg.ID,
	}
	if err := c.writeJSON(chatResponse); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d recover send end failed", c.id, request.SessionID), err)
		return
	}
	if err := c.sendDone(request.SessionID, aiMsg.ID); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d recover send done failed", c.id, request.SessionID), err)
		return
	}

	c.db.Model(&aiMsg).Updates(map[string]interface{}{
		"is_streaming": false,
		"is_reasoning": false,
	})

	log.Printf("[WebSocket] conn=%d recover finish, sessionId=%d", c.id, request.SessionID)
}

func (c *wsConnection) tryStartSession(sessionID int64) bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if _, exists := c.activeSessions[sessionID]; exists {
		return false
	}
	c.activeSessions[sessionID] = struct{}{}
	return true
}

func (c *wsConnection) finishSession(sessionID int64) {
	c.sessionMu.Lock()
	delete(c.activeSessions, sessionID)
	c.sessionMu.Unlock()
}

func (c *wsConnection) startPingLoop() {
	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("[WebSocket] conn=%d send ping", c.id)
			if err := c.writeControl(websocket.PingMessage, []byte("ping")); err != nil {
				logWebSocketDisconnect(fmt.Sprintf("conn=%d ping failed", c.id), err)
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *wsConnection) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeMessage(websocket.TextMessage, data)
}

func (c *wsConnection) sendError(errorMsg string) error {
	errorMessage := fmt.Sprintf("[ERROR] %s", errorMsg)
	return c.writeMessage(websocket.TextMessage, []byte(errorMessage))
}

func (c *wsConnection) sendDone(sessionID int64, aiMessageID string) error {
	doneResponse := map[string]interface{}{
		"type":          "done",
		"session_id":    sessionID,
		"ai_message_id": aiMessageID,
	}
	return c.writeJSON(doneResponse)
}

func (c *wsConnection) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return err
	}
	return c.conn.WriteMessage(messageType, data)
}

func (c *wsConnection) writeControl(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteControl(messageType, data, time.Now().Add(wsWriteWait))
}

func (c *wsConnection) writeErrorOrDisconnect(sessionID int64, errorMsg string) {
	if err := c.sendError(errorMsg); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("conn=%d session=%d send error failed", c.id, sessionID), err)
	}
}

func (c *wsConnection) shutdown() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// 统一处理 WebSocket 读写异常的日志分级。
func logWebSocketDisconnect(action string, err error) {
	if err == nil {
		return
	}

	if isExpectedWebSocketDisconnect(err) {
		log.Printf("[WebSocket] client disconnected during %s: %v", action, err)
		return
	}

	log.Printf("[WebSocket] %s: %v", action, err)
}

// 判断一个 WebSocket 错误是否属于常见的对端断开场景。
func isExpectedWebSocketDisconnect(err error) bool {
	if websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
		websocket.CloseAbnormalClosure,
	) {
		return true
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unexpected eof") ||
		strings.Contains(errMsg, "wsasend") ||
		strings.Contains(errMsg, "wsarecv") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection reset by peer") ||
		strings.Contains(errMsg, "connection aborted")
}

func resolveWebSocketModelConfig(request *models.WebSocketChatRequest) (config.ModelRuntimeConfig, bool) {
	if trimmedKey := strings.TrimSpace(request.ModelKey); trimmedKey != "" {
		return config.GetModelConfig(trimmedKey)
	}

	return config.ModelRuntimeConfig{
		ApiKey:  config.FirstNonEmpty(request.ApiKey, config.GetAIAPIKey()),
		BaseURL: config.FirstNonEmpty(request.BaseUrl, config.GetAIBaseURL()),
		ModelID: config.FirstNonEmpty(request.ModelID, config.GetAIModelID()),
	}, true
}
