package controller

import (
	"ai-chat/middleware"
	"ai-chat/models"
	"ai-chat/service"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// WebSocketController WebSocket控制器
type WebSocketController struct {
	db          *gorm.DB
	wsManager   *service.WebSocketManager
	chatService *service.ChatService
}

// NewWebSocketController 创建WebSocket控制器
func NewWebSocketController(db *gorm.DB, chatService *service.ChatService) *WebSocketController {
	return &WebSocketController{
		db:          db,
		wsManager:   service.NewWebSocketManager(),
		chatService: chatService,
	}
}

// upgrader WebSocket升级器配置
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 生产环境应该更严格地检查Origin
		return true
	},
}

// HandleWebSocket 处理WebSocket连接
// 路径: /chat/ws/{sessionId}
func (w *WebSocketController) HandleWebSocket(ctx *gin.Context) {
	fmt.Printf("[WebSocket] 收到WebSocket连接请求\n")

	// 获取用户ID
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		fmt.Printf("[WebSocket] 认证失败: 未登录\n")
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// 获取会话ID
	sessionIDStr := ctx.Param("sessionId")
	sessionID, err := strconv.ParseUint(sessionIDStr, 10, 64)
	if err != nil {
		fmt.Printf("[WebSocket] 无效的会话ID: %s\n", sessionIDStr)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 升级HTTP连接为WebSocket
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		fmt.Printf("[WebSocket] 升级连接失败: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Printf("[WebSocket] 连接升级成功: sessionID=%d, userID=%d\n", sessionID, userID)

	// 验证会话是否属于当前用户
	var session *models.ChatSession
	if sessionID == 0 {
		// sessionId=0，自动创建新会话
		fmt.Printf("[WebSocket] sessionId=0，自动创建新会话\n")
		newSession := &models.ChatSession{
			UserID:      userID,
			Title:       "新会话",
			LastMessage: "",
		}
		if err := w.db.Create(newSession).Error; err != nil {
			fmt.Printf("[WebSocket] 创建会话失败: %v\n", err)
			return
		}
		session = newSession
		sessionID = uint64(newSession.ID)
		fmt.Printf("[WebSocket] 创建新会话成功: sessionID=%d\n", sessionID)
	} else {
		// 验证现有会话
		session = &models.ChatSession{}
		if err := w.db.Where("id = ? AND user_id = ?", sessionID, userID).First(session).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				fmt.Printf("[WebSocket] 会话不存在或无权访问: sessionID=%d, userID=%d\n", sessionID, userID)
				return
			}
			fmt.Printf("[WebSocket] 数据库错误: %v\n", err)
			return
		}
	}

	// 创建连接管理器
	connManager := service.NewConnectionManager(conn, uint(sessionID), userID)
	w.wsManager.AddConnection(uint(sessionID), connManager)
	defer func() {
		connManager.Close()
		w.wsManager.RemoveConnection(uint(sessionID))
	}()

	// 发送连接确认消息，包含实际的 sessionID
	connManager.SendMessage(&models.WebSocketMessage{
		Type:      "connected",
		SessionID: int64(sessionID),
	})

	// 启动心跳机制（每30秒发送一次，60秒超时）
	connManager.StartHeartbeat(30*time.Second, 60*time.Second)

	// 启动消息写入goroutine
	go w.handleMessageWrite(connManager)

	// 启动消息读取goroutine
	w.handleMessageRead(ctx, connManager, userID, uint(sessionID))
}

// handleMessageWrite 处理消息写入
func (w *WebSocketController) handleMessageWrite(cm *service.ConnectionManager) {
	for {
		select {
		case msg := <-cm.GetMessageQueue():
			if msg == nil {
				// 队列已关闭
				return
			}
			if err := cm.SendToClient(msg); err != nil {
				fmt.Printf("[WebSocket] 发送消息失败: %v\n", err)
				cm.Close()
				return
			}

		case <-cm.GetCloseChan():
			return
		}
	}
}

// handleMessageRead 处理消息读取
func (w *WebSocketController) handleMessageRead(ctx *gin.Context, cm *service.ConnectionManager, userID uint, sessionID uint) {
	for {
		msg, err := cm.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[WebSocket] 连接错误: %v\n", err)
			} else {
				fmt.Printf("[WebSocket] 客户端断开连接: %v\n", err)
			}
			cm.Close()
			return
		}

		fmt.Printf("[WebSocket] 收到消息: type=%s, clientID=%s\n", msg.Type, msg.ClientID)

		switch msg.Type {
		case "heartbeat":
			// 响应心跳
			cm.UpdateLastHeartbeat()
			response := &models.WebSocketMessage{
				Type: "heartbeat",
			}
			if err := cm.SendMessage(response); err != nil {
				fmt.Printf("[WebSocket] 发送心跳响应失败: %v\n", err)
				cm.Close()
				return
			}

		case "recover":
			// 处理重连恢复请求
			lastSeq := msg.LastSeq
			fmt.Printf("[WebSocket] 处理重连恢复: lastSeq=%d, sessionID=%d\n", lastSeq, sessionID)
			w.handleRecover(cm, sessionID, lastSeq)

		case "ack":
			// 处理消息确认（后续可用于可靠传递）
			cm.SetLastSeq(msg.Seq)
			fmt.Printf("[WebSocket] 收到ACK: seq=%d, clientID=%s\n", msg.Seq, msg.ClientID)

		case "message":
			// 处理普通消息（用户输入，需要调用AI回复）
			fmt.Printf("[WebSocket] 处理消息: content=%s, clientID=%s\n", msg.Content, msg.ClientID)
			go w.handleStreamMessage(cm, userID, sessionID, msg)

		default:
			fmt.Printf("[WebSocket] 未知的消息类型: %s\n", msg.Type)
		}
	}
}

// handleRecover 处理重连恢复
// 根据lastSeq推送所有seq > lastSeq的消息
func (w *WebSocketController) handleRecover(cm *service.ConnectionManager, sessionID uint, lastSeq int64) {
	// 从数据库查询所有seq > lastSeq的消息
	var messages []models.ChatMessage
	if err := w.db.Where("session_id = ? AND seq > ?", sessionID, lastSeq).
		Order("seq ASC").
		Find(&messages).Error; err != nil {
		fmt.Printf("[WebSocket] 查询消息失败: %v\n", err)
		response := &models.WebSocketMessage{
			Type:  "error",
			Error: "无法恢复消息",
		}
		cm.SendMessage(response)
		return
	}

	fmt.Printf("[WebSocket] 恢复消息: 找到 %d 条消息\n", len(messages))

	// 逐条推送消息
	for _, dbMsg := range messages {
		response := &models.WebSocketMessage{
			Type:     "message",
			ID:       int64(dbMsg.ID),
			ClientID: dbMsg.ClientID,
			Seq:      dbMsg.Seq,
			Content:  dbMsg.Content,
			Data: gin.H{
				"role":              dbMsg.Role,
				"reasoning_content": dbMsg.ReasoningContent,
				"timestamp":         dbMsg.Timestamp,
			},
		}
		if err := cm.SendMessage(response); err != nil {
			fmt.Printf("[WebSocket] 推送恢复消息失败: %v\n", err)
			cm.Close()
			return
		}
	}

	// 发送恢复完成标记
	completeMsg := &models.WebSocketMessage{
		Type: "recover_complete",
		Seq:  lastSeq,
	}
	cm.SendMessage(completeMsg)
}

// SendStreamMessage 发送流式消息（用于AI回复）
func (w *WebSocketController) SendStreamMessage(sessionID uint, clientID string, content string, seq int64, isEnd bool) error {
	cm := w.wsManager.GetConnection(sessionID)
	if cm == nil {
		return fmt.Errorf("会话 %d 没有活跃连接", sessionID)
	}

	msg := &models.WebSocketMessage{
		Type:     "message",
		ClientID: clientID,
		Seq:      seq,
		Content:  content,
		IsEnd:    isEnd,
	}
	return cm.SendMessage(msg)
}

// NotifyConnectionClosed 通知客户端连接将关闭（如服务器重启）
func (w *WebSocketController) NotifyConnectionClosed(sessionID uint, reason string) error {
	cm := w.wsManager.GetConnection(sessionID)
	if cm == nil {
		return fmt.Errorf("会话 %d 没有活跃连接", sessionID)
	}

	msg := &models.WebSocketMessage{
		Type:  "close_reason",
		Error: reason,
	}
	if err := cm.SendMessage(msg); err != nil {
		return err
	}

	// 给客户端200ms时间接收消息后关闭
	time.Sleep(200 * time.Millisecond)
	cm.Close()
	return nil
}

// handleStreamMessage 处理流式AI回复
// 保存用户消息、调用AI、通过WebSocket逐块推送回复
func (w *WebSocketController) handleStreamMessage(cm *service.ConnectionManager, userID uint, sessionID uint, msg *models.WebSocketMessage) {
	fmt.Printf("[WebSocket] 开始处理流式消息: sessionID=%d, clientID=%s\n", sessionID, msg.ClientID)

	// 1. 先保存用户消息到数据库（含ClientID）
	userMsg, err := w.chatService.SaveMessageWithClientID(sessionID, "user", msg.Content, "", msg.ClientID)
	if err != nil {
		fmt.Printf("[WebSocket] 保存用户消息失败: %v\n", err)
		errMsg := &models.WebSocketMessage{
			Type:     "error",
			ClientID: msg.ClientID,
			Error:    "保存消息失败",
		}
		cm.SendMessage(errMsg)
		return
	}
	fmt.Printf("[WebSocket] 用户消息已保存: id=%d, seq=%d, clientID=%s\n", userMsg.ID, userMsg.Seq, userMsg.ClientID)

	// 2. 准备消息列表（优先使用前端传来的messages，否则从数据库查询）
	var messages []models.AiMessage
	if len(msg.Messages) > 0 {
		// 使用前端传来的历史消息上下文
		fmt.Printf("[WebSocket] 使用前端传来的历史消息: %d条\n", len(msg.Messages))
		messages = msg.Messages
	} else {
		// 从数据库获取会话消息历史
		session, err := w.chatService.GetSession(sessionID)
		if err != nil {
			fmt.Printf("[WebSocket] 获取会话失败: %v\n", err)
			errMsg := &models.WebSocketMessage{
				Type:     "error",
				ClientID: msg.ClientID,
				Error:    "获取会话失败",
			}
			cm.SendMessage(errMsg)
			return
		}

		// 构建消息列表（转换为AI服务需要的格式）
		messages = make([]models.AiMessage, 0, len(session.Messages))
		for _, m := range session.Messages {
			messages = append(messages, models.AiMessage{
				Role:     m.Role,
				Content:  m.Content,
				ClientID: m.ClientID,
			})
		}
		fmt.Printf("[WebSocket] 从数据库加载历史消息: %d条\n", len(messages))
	}

	// 3. 准备工具列表（从前端传来的tools字段）
	var tools []models.Tool
	if len(msg.Tools) > 0 {
		tools = make([]models.Tool, 0, len(msg.Tools))
		for _, toolType := range msg.Tools {
			tools = append(tools, models.Tool{Type: toolType})
		}
		fmt.Printf("[WebSocket] 使用前端传来的工具列表: %v\n", msg.Tools)
	}

	// 4. 调用AI流式处理（使用前端传来的modelId和baseUrl）
	modelId := msg.ModelId
	baseUrl := msg.BaseUrl
	fmt.Printf("[WebSocket] 使用模型: %s, BaseURL: %s\n", modelId, baseUrl)
	fmt.Printf("[WebSocket] BaseURL长度: %d, 内容: %q\n", len(baseUrl), baseUrl)
	dataChan, errChan := w.chatService.StreamChat(sessionID, messages, tools, modelId, baseUrl)

	// 5. 获取AI回复的下一个seq
	aiSeq, err := w.chatService.GetNextSeq(sessionID)
	if err != nil {
		fmt.Printf("[WebSocket] 获取seq失败: %v\n", err)
		errMsg := &models.WebSocketMessage{
			Type:     "error",
			ClientID: msg.ClientID,
			Error:    "获取序列号失败",
		}
		cm.SendMessage(errMsg)
		return
	}

	// 6. 使用前端传来的ClientID作为AI消息的ClientID（完全统一）
	aiClientID := msg.ClientID

	// 7. 逐块发送AI回复到客户端
	for {
		select {
		case content, ok := <-dataChan:
			if !ok {
				dataChan = nil
				break
			}

			// 将增量内容发送给客户端（不需要解析JSON，直接转发）
			deltaMsg := &models.WebSocketMessage{
				Type:     "message_delta",
				ClientID: aiClientID,
				Seq:      aiSeq,
				Content:  content,
				IsEnd:    false,
			}
			if err := cm.SendMessage(deltaMsg); err != nil {
				fmt.Printf("[WebSocket] 发送消息增量失败: %v\n", err)
				cm.Close()
				return
			}

		case err, ok := <-errChan:
			if !ok {
				errChan = nil
				break
			}
			if err != nil {
				fmt.Printf("[WebSocket] AI流式处理错误: %v\n", err)
				errMsg := &models.WebSocketMessage{
					Type:     "error",
					ClientID: aiClientID,
					Error:    fmt.Sprintf("AI处理失败: %v", err),
				}
				cm.SendMessage(errMsg)
				return
			}

		case <-cm.GetCloseChan():
			// 连接已关闭
			fmt.Printf("[WebSocket] 连接已关闭，停止发送AI回复\n")
			return
		}

		// 检查是否两个channel都已关闭
		if dataChan == nil && errChan == nil {
			break
		}
	}

	// 8. 发送完成标记
	fmt.Printf("[WebSocket] AI回复完成，准备保存到数据库\n")

	// AI响应已在ChatService中自动保存，这里只需发送完成信号
	completeMsg := &models.WebSocketMessage{
		Type:     "message_complete",
		ClientID: aiClientID,
		Seq:      aiSeq,
		IsEnd:    true,
	}
	if err := cm.SendMessage(completeMsg); err != nil {
		fmt.Printf("[WebSocket] 发送完成标记失败: %v\n", err)
	}

	fmt.Printf("[WebSocket] 流式消息处理完成: sessionID=%d, aiSeq=%d\n", sessionID, aiSeq)
}
