package routes

import (
	"ai-chat/controller"
	"ai-chat/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func UserRoutes(r *gin.Engine, db *gorm.DB) {
	userGroup := r.Group("/user")
	{
		// 公开接口（不需要登录）
		userGroup.POST("/register", func(c *gin.Context) {
			controller.Register(c, db)
		})
		userGroup.POST("/login", func(c *gin.Context) {
			controller.Login(c, db)
		})

		// 需要登录的接口
		authorized := userGroup.Group("/")
		authorized.Use(middleware.AuthMiddleware(db))
		{
			authorized.POST("/change-password", func(c *gin.Context) {
				controller.ChangePassword(c, db)
			})
		}
	}
}