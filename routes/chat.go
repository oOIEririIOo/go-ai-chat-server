package routes

import (
	"ai-chat/controller"
	"ai-chat/middleware"
	"ai-chat/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChatRoutes 注册聊天相关路由
func ChatRoutes(r *gin.Engine, db *gorm.DB, aiService *service.AiService) {
	chatGroup := r.Group("/chat")
	// 添加 JWT 认证中间件
	chatGroup.Use(middleware.AuthMiddleware(db))
	{
		chatController := controller.NewChatController(db, aiService)
		wsController := controller.NewWebSocketController(db, chatController.GetChatService())

		// 会话管理
		chatGroup.POST("/sessions", chatController.CreateSession)
		chatGroup.GET("/sessions", chatController.GetSessions)
		chatGroup.GET("/sessions/:id", chatController.GetSession)
		chatGroup.DELETE("/sessions/:id", chatController.DeleteSession)
		chatGroup.DELETE("/sessions", chatController.DeleteAllSessions) // 删除所有会话

		// 标题生成
		chatGroup.POST("/generate-title", chatController.GenerateTitle)

		// WebSocket 连接（用于实时聊天）
		// 路径: /chat/ws/{sessionId}
		// 支持消息同步、自动重连、心跳检测、实时AI回复
		chatGroup.GET("/ws/:sessionId", wsController.HandleWebSocket)
	}
}
