package routes

import (
	"ai-chat/controller"
	"ai-chat/middleware"
	"ai-chat/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChatRoutes 注册聊天相关路由。
func ChatRoutes(r *gin.Engine, db *gorm.DB, aiService *service.AiService) {
	chatGroup := r.Group("/chat")
	chatGroup.Use(middleware.AuthMiddleware(db))
	{
		chatController := controller.NewChatController(db, aiService)

		chatGroup.POST("/sessions", chatController.CreateSession)
		chatGroup.GET("/sessions", chatController.GetSessions)
		chatGroup.GET("/sessions/:id/messages", chatController.GetSessionMessages)
		chatGroup.DELETE("/sessions/:id", chatController.DeleteSession)
		chatGroup.DELETE("/sessions", chatController.DeleteAllSessions)
		chatGroup.POST("/stream", chatController.StreamChat)
		chatGroup.POST("/generate-title", chatController.GenerateTitle)
	}
}
