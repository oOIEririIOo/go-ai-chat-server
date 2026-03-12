package service

import (
	"ai-chat/models"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionManager 管理单个WebSocket连接的生命周期
type ConnectionManager struct {
	conn             *websocket.Conn
	sessionID        uint
	userID           uint
	closeOnce        sync.Once
	closeChan        chan struct{}
	messageQueue     chan *models.WebSocketMessage
	heartbeatTicker  *time.Ticker
	lastHeartbeat    time.Time
	mu               sync.RWMutex
	lastSeq          int64 // 客户端最后确认的序列号
}

// WebSocketManager 管理所有WebSocket连接
type WebSocketManager struct {
	connections map[uint]*ConnectionManager // 按会话ID存储连接
	mu          sync.RWMutex
	messageIDMap map[string]int64 // clientID -> seq 映射，用于去重
}

// NewWebSocketManager 创建WebSocket连接管理器
func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		connections:  make(map[uint]*ConnectionManager),
		messageIDMap: make(map[string]int64),
	}
}

// NewConnectionManager 创建连接管理器
func NewConnectionManager(conn *websocket.Conn, sessionID, userID uint) *ConnectionManager {
	return &ConnectionManager{
		conn:          conn,
		sessionID:     sessionID,
		userID:        userID,
		closeChan:     make(chan struct{}),
		messageQueue:  make(chan *models.WebSocketMessage, 100), // 缓冲队列
		lastHeartbeat: time.Now(),
	}
}

// AddConnection 添加新连接
func (wm *WebSocketManager) AddConnection(sessionID uint, cm *ConnectionManager) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.connections[sessionID] = cm
	fmt.Printf("[WebSocket] 添加连接: sessionID=%d, userID=%d\n", sessionID, cm.userID)
}

// RemoveConnection 移除连接
func (wm *WebSocketManager) RemoveConnection(sessionID uint) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	delete(wm.connections, sessionID)
	fmt.Printf("[WebSocket] 移除连接: sessionID=%d\n", sessionID)
}

// GetConnection 获取连接
func (wm *WebSocketManager) GetConnection(sessionID uint) *ConnectionManager {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return wm.connections[sessionID]
}

// BroadcastMessage 广播消息到指定会话（如果多个用户共享一个会话）
func (wm *WebSocketManager) BroadcastMessage(sessionID uint, msg *models.WebSocketMessage) error {
	cm := wm.GetConnection(sessionID)
	if cm == nil {
		return fmt.Errorf("会话 %d 没有活跃连接", sessionID)
	}
	return cm.SendMessage(msg)
}

// SendMessage 发送消息
func (cm *ConnectionManager) SendMessage(msg *models.WebSocketMessage) error {
	select {
	case cm.messageQueue <- msg:
		return nil
	case <-cm.closeChan:
		return fmt.Errorf("连接已关闭")
	default:
		return fmt.Errorf("消息队列已满")
	}
}

// StartHeartbeat 启动心跳机制
func (cm *ConnectionManager) StartHeartbeat(interval time.Duration, timeout time.Duration) {
	cm.heartbeatTicker = time.NewTicker(interval)

	go func() {
		timeoutTicker := time.NewTicker(timeout)
		defer timeoutTicker.Stop()
		defer cm.heartbeatTicker.Stop()

		for {
			select {
			case <-cm.heartbeatTicker.C:
				// 发送心跳
				heartbeat := &models.WebSocketMessage{
					Type: "heartbeat",
				}
				if err := cm.SendMessage(heartbeat); err != nil {
					fmt.Printf("[WebSocket] 发送心跳失败: %v\n", err)
					cm.Close()
					return
				}
				cm.mu.Lock()
				cm.lastHeartbeat = time.Now()
				cm.mu.Unlock()

			case <-timeoutTicker.C:
				// 检查是否超时（60秒未收到心跳响应）
				cm.mu.RLock()
				if time.Since(cm.lastHeartbeat) > timeout {
					cm.mu.RUnlock()
					fmt.Printf("[WebSocket] 心跳超时: sessionID=%d\n", cm.sessionID)
					cm.Close()
					return
				}
				cm.mu.RUnlock()

			case <-cm.closeChan:
				return
			}
		}
	}()
}

// UpdateLastHeartbeat 更新最后的心跳时间
func (cm *ConnectionManager) UpdateLastHeartbeat() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.lastHeartbeat = time.Now()
}

// GetLastSeq 获取最后的序列号
func (cm *ConnectionManager) GetLastSeq() int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.lastSeq
}

// SetLastSeq 设置最后的序列号
func (cm *ConnectionManager) SetLastSeq(seq int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.lastSeq = seq
}

// SendToClient 向客户端发送消息（内部写入连接）
func (cm *ConnectionManager) SendToClient(msg *models.WebSocketMessage) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.conn == nil {
		return fmt.Errorf("连接已关闭")
	}

	return cm.conn.WriteJSON(msg)
}

// ReadMessage 读取客户端消息
func (cm *ConnectionManager) ReadMessage() (*models.WebSocketMessage, error) {
	var msg models.WebSocketMessage
	err := cm.conn.ReadJSON(&msg)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// Close 优雅关闭连接
func (cm *ConnectionManager) Close() {
	cm.closeOnce.Do(func() {
		close(cm.closeChan)

		// 停止心跳
		if cm.heartbeatTicker != nil {
			cm.heartbeatTicker.Stop()
		}

		// 关闭消息队列
		close(cm.messageQueue)

		// 关闭WebSocket连接
		cm.mu.Lock()
		if cm.conn != nil {
			cm.conn.Close()
			cm.conn = nil
		}
		cm.mu.Unlock()

		fmt.Printf("[WebSocket] 连接已关闭: sessionID=%d\n", cm.sessionID)
	})
}

// IsConnected 检查连接是否还活跃
func (cm *ConnectionManager) IsConnected() bool {
	select {
	case <-cm.closeChan:
		return false
	default:
		return true
	}
}

// MessageQueueSize 获取消息队列长度
func (cm *ConnectionManager) MessageQueueSize() int {
	return len(cm.messageQueue)
}

// GetMessageQueue 获取消息队列（用于接收消息）
func (cm *ConnectionManager) GetMessageQueue() <-chan *models.WebSocketMessage {
	return cm.messageQueue
}

// GetCloseChan 获取关闭通道（用于监听关闭事件）
func (cm *ConnectionManager) GetCloseChan() <-chan struct{} {
	return cm.closeChan
}
