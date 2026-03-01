package middleware

import (
	"ai-chat/models"
	"ai-chat/service"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthMiddleware JWT 认证中间件
// 功能: 验证请求中的 JWT Token，并将用户信息注入上下文
// 使用方式: router.Use(middleware.AuthMiddleware(db))
func AuthMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Printf("[AuthDebug] AuthMiddleware: 收到请求 %s %s\n", c.Request.Method, c.Request.URL.Path)

		// 从 Header 中获取 Authorization
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			fmt.Printf("[AuthDebug] AuthMiddleware: 缺少 Authorization Header\n")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少认证信息"})
			c.Abort()
			return
		}

		// 检查 Bearer 前缀
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			fmt.Printf("[AuthDebug] AuthMiddleware: Authorization 格式错误: %s\n", authHeader)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证格式错误，应为 Bearer <token>"})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 解析 Token
		claims, err := service.ParseToken(tokenString)
		if err != nil {
			fmt.Printf("[AuthDebug] AuthMiddleware: Token 验证失败: %v\n", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		// 验证 Token 版本号
		var user models.User
		if err := db.First(&user, claims.UserID).Error; err != nil {
			fmt.Printf("[AuthDebug] AuthMiddleware: 用户不存在: %d\n", claims.UserID)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			c.Abort()
			return
		}

		if user.TokenVersion != claims.TokenVersion {
			fmt.Printf("[AuthDebug] AuthMiddleware: Token 版本不匹配，用户版本=%d，Token版本=%d\n",
				user.TokenVersion, claims.TokenVersion)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token 已失效，请重新登录"})
			c.Abort()
			return
		}

		fmt.Printf("[AuthDebug] AuthMiddleware: 用户认证成功，UserID=%d, Username=%s\n", claims.UserID, claims.Username)

		// 将用户信息存入上下文，供后续使用
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)

		c.Next()
	}
}

// GetUserID 从上下文中获取用户ID
// 使用方式: userID := middleware.GetUserID(c)
func GetUserID(c *gin.Context) uint {
	userID, exists := c.Get("userID")
	if !exists {
		return 0
	}
	return userID.(uint)
}

// GetUsername 从上下文中获取用户名
// 使用方式: username := middleware.GetUsername(c)
func GetUsername(c *gin.Context) string {
	username, exists := c.Get("username")
	if !exists {
		return ""
	}
	return username.(string)
}
