package routes

import (
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

const voiceWSWriteWait = 10 * time.Second

var nextVoiceConnectionID uint64

// voiceWSConnection 表示一条前端语音转写连接。
// 它负责持有当前登录用户、当前进行中的语音会话，以及串行写回客户端的能力。
type voiceWSConnection struct {
	id        uint64
	userID    uint
	conn      *websocket.Conn
	db        *gorm.DB
	rtasr     *service.XFYunRTASRService
	writeMu   sync.Mutex
	sessionMu sync.Mutex
	session   *voiceTranscriptionSession
}

// voiceTranscriptionSession 表示一轮语音转写会话。
type voiceTranscriptionSession struct {
	requestID string
	lang      string
	upstream  *service.XFYunRTASRSession
}

// VoiceWebSocketRoutes 注册语音转写 WebSocket 路由。
func VoiceWebSocketRoutes(r *gin.Engine, db *gorm.DB, rtasr *service.XFYunRTASRService) {
	ws := r.Group("/ws")
	{
		ws.GET("/voice", func(c *gin.Context) {
			handleVoiceWebSocket(c, db, rtasr)
		})
	}
}

// handleVoiceWebSocket 建立前端与后端之间的语音转写连接。
// 这里先做 JWT 鉴权，鉴权成功后再升级为 WebSocket。
func handleVoiceWebSocket(c *gin.Context, db *gorm.DB, rtasr *service.XFYunRTASRService) {
	userID, err := authenticateWebSocketUser(c, db)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	conn, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[VoiceWS] upgrade failed: %v", err)
		return
	}

	client := &voiceWSConnection{
		id:     atomic.AddUint64(&nextVoiceConnectionID, 1),
		userID: userID,
		conn:   conn,
		db:     db,
		rtasr:  rtasr,
	}
	defer client.shutdown()

	log.Printf("[VoiceWS] connected conn=%d user=%d", client.id, client.userID)

	for {
		messageType, payload, err := client.conn.ReadMessage()
		if err != nil {
			logWebSocketDisconnect(fmt.Sprintf("voice conn=%d read failed", client.id), err)
			return
		}

		// 文本消息负责控制 start / stop / cancel，二进制消息则承载 PCM 音频帧。
		switch messageType {
		case websocket.TextMessage:
			var msg models.VoiceWSClientMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				client.writeError("", "语音消息格式错误")
				continue
			}
			client.handleTextMessage(msg)
		case websocket.BinaryMessage:
			client.handleAudioFrame(payload)
		default:
			client.writeError("", "不支持的语音消息类型")
		}
	}
}

// handleTextMessage 分发前端的控制消息。
func (c *voiceWSConnection) handleTextMessage(msg models.VoiceWSClientMessage) {
	switch msg.Type {
	case "start":
		c.handleStart(msg)
	case "stop":
		c.handleStop(msg.RequestID)
	case "cancel":
		c.handleCancel(msg.RequestID)
	default:
		c.writeError(msg.RequestID, "未知的语音控制消息")
	}
}

// handleStart 开始一轮新的语音转写。
// 同一条连接在同一时刻只允许一个进行中的 requestId。
func (c *voiceWSConnection) handleStart(msg models.VoiceWSClientMessage) {
	if c.rtasr == nil {
		c.writeError(msg.RequestID, "语音转写服务未配置")
		return
	}

	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.session != nil {
		c.writeError(msg.RequestID, "当前已有进行中的语音转写")
		return
	}

	upstreamConn, err := c.rtasr.Dial()
	if err != nil {
		log.Printf("[VoiceWS] conn=%d start upstream failed: %v", c.id, err)
		c.writeError(msg.RequestID, "连接语音转写服务失败")
		return
	}

	// 与讯飞的上游会话建立成功后，立即启动结果读取协程。
	upstream := service.NewXFYunRTASRSession(upstreamConn)
	upstream.StartReading()

	c.session = &voiceTranscriptionSession{
		requestID: msg.RequestID,
		lang:      msg.Lang,
		upstream:  upstream,
	}

	c.writeJSON(models.VoiceWSServerMessage{
		Type:      "ready",
		RequestID: msg.RequestID,
		Message:   "voice session ready",
	})
}

// handleAudioFrame 把前端发来的 PCM 音频帧继续转发到讯飞。
func (c *voiceWSConnection) handleAudioFrame(payload []byte) {
	c.sessionMu.Lock()
	session := c.session
	c.sessionMu.Unlock()

	if session == nil {
		c.writeError("", "语音会话尚未开始")
		return
	}

	if err := session.upstream.SendAudio(payload); err != nil {
		log.Printf("[VoiceWS] conn=%d request=%s send audio failed: %v", c.id, session.requestID, err)
		c.writeError(session.requestID, "发送语音数据失败")
		c.clearSession()
	}
}

// handleStop 结束当前语音会话，并等待讯飞返回最终文本。
func (c *voiceWSConnection) handleStop(requestID string) {
	c.sessionMu.Lock()
	session := c.session
	c.sessionMu.Unlock()

	if session == nil || session.requestID != requestID {
		c.writeError(requestID, "没有匹配的语音会话")
		return
	}

	if err := session.upstream.SendEnd(); err != nil {
		log.Printf("[VoiceWS] conn=%d request=%s send end failed: %v", c.id, requestID, err)
		c.writeError(requestID, "结束语音转写失败")
		c.clearSession()
		return
	}

	go func(session *voiceTranscriptionSession) {
		defer c.clearSession()
		// 第一版只在会话结束时回给前端最终文本，不回推 partial result。
		text, err := session.upstream.AwaitFinalText(4 * time.Second)
		if err != nil && text == "" {
			log.Printf("[VoiceWS] conn=%d request=%s await final failed: %v", c.id, requestID, err)
			c.writeError(requestID, "语音转写失败")
			return
		}

		c.writeJSON(models.VoiceWSServerMessage{
			Type:      "final_text",
			RequestID: requestID,
			Text:      text,
		})
	}(session)
}

// handleCancel 取消当前会话，不回传文本结果。
func (c *voiceWSConnection) handleCancel(requestID string) {
	c.sessionMu.Lock()
	session := c.session
	c.sessionMu.Unlock()

	if session == nil || session.requestID != requestID {
		return
	}

	_ = session.upstream.Close()
	c.clearSession()
}

// clearSession 清理当前会话并释放上游连接。
func (c *voiceWSConnection) clearSession() {
	c.sessionMu.Lock()
	session := c.session
	c.session = nil
	c.sessionMu.Unlock()

	if session != nil {
		_ = session.upstream.Close()
	}
}

// writeJSON 是写回前端的统一出口，保证同一连接不会发生并发写冲突。
func (c *voiceWSConnection) writeJSON(message models.VoiceWSServerMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(voiceWSWriteWait)); err != nil {
		return err
	}
	return c.conn.WriteJSON(message)
}

// writeError 统一回写语音转写错误消息。
func (c *voiceWSConnection) writeError(requestID string, message string) {
	if err := c.writeJSON(models.VoiceWSServerMessage{
		Type:      "error",
		RequestID: requestID,
		Message:   message,
	}); err != nil {
		logWebSocketDisconnect(fmt.Sprintf("voice conn=%d send error failed", c.id), err)
	}
}

// shutdown 关闭客户端连接并回收上游语音会话。
func (c *voiceWSConnection) shutdown() {
	c.clearSession()
	_ = c.conn.Close()
}

// authenticateWebSocketUser 复用现有 JWT 逻辑，为语音 WebSocket 提供登录态校验。
func authenticateWebSocketUser(c *gin.Context, db *gorm.DB) (uint, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return 0, fmt.Errorf("缺少认证信息")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return 0, fmt.Errorf("认证格式错误")
	}

	claims, err := service.ParseToken(parts[1])
	if err != nil {
		return 0, err
	}

	var user models.User
	if err := db.First(&user, claims.UserID).Error; err != nil {
		return 0, fmt.Errorf("用户不存在")
	}

	if user.TokenVersion != claims.TokenVersion {
		return 0, fmt.Errorf("token 已失效")
	}

	return claims.UserID, nil
}
