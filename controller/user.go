package controller

import (
	"ai-chat/middleware"
	"ai-chat/models"
	"ai-chat/service"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterRequest 注册请求结构
type RegisterRequest struct {
	Username string `json:"username" binding:"required"` // 用户名
	Password string `json:"password" binding:"required"` // 密码
}

// Register 用户注册
// 功能: 处理用户注册请求
// 参数:
//   - c: Gin 上下文
//   - db: 数据库连接
func Register(c *gin.Context, db *gorm.DB) {
	fmt.Printf("[AuthDebug] Register: 收到注册请求，IP: %s\n", c.ClientIP())

	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[AuthDebug] Register: 请求参数绑定失败: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[AuthDebug] Register: 用户名=%s,", req.Username)

	user := &models.User{
		Username: req.Username,
		Password: req.Password,
	}

	createdUser, err := service.RegisterUser(db, user)
	if err != nil {
		fmt.Printf("[AuthDebug] Register: 注册失败: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[AuthDebug] Register: 注册成功，用户=%s\n", createdUser.Username)
	c.JSON(http.StatusCreated, gin.H{
		"message": "注册成功",
		"user": gin.H{
			"username": createdUser.Username,
		},
	})
}

// LoginRequest 登录请求结构
type LoginRequest struct {
	Account  string `json:"account" binding:"required"`  // 账号
	Password string `json:"password" binding:"required"` // 密码
}

// Login 用户登录
// 功能: 处理用户登录请求，成功返回 JWT Token
// 参数:
//   - c: Gin 上下文
//   - db: 数据库连接
func Login(c *gin.Context, db *gorm.DB) {
	fmt.Printf("[AuthDebug] Login: 收到登录请求，IP: %s\n", c.ClientIP())

	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[AuthDebug] Login: 请求参数绑定失败: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[AuthDebug] Login: 账号=%s\n", req.Account)

	user, err := service.LoginUser(db, req.Account, req.Password)
	if err != nil {
		fmt.Printf("[AuthDebug] Login: 登录失败: %v\n", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 生成 JWT Token，包含当前 TokenVersion
	token, err := service.GenerateToken(user.ID, user.Username, user.TokenVersion)
	if err != nil {
		fmt.Printf("[AuthDebug] Login: 生成 Token 失败: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成认证令牌失败"})
		return
	}

	fmt.Printf("[AuthDebug] Login: 登录成功，用户=%s，已生成 Token\n", user.Username)
	c.JSON(http.StatusOK, gin.H{
		"message": "登录成功",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// ChangePasswordRequest 修改密码请求结构
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"` // 旧密码
	NewPassword string `json:"new_password" binding:"required"` // 新密码
}

// ChangePassword 修改密码
// 功能: 验证旧密码，更新为新密码，并使所有旧 Token 失效
// 参数:
//   - c: Gin 上下文
//   - db: 数据库连接
func ChangePassword(c *gin.Context, db *gorm.DB) {
	fmt.Printf("[AuthDebug] ChangePassword: 收到修改密码请求\n")

	// 从上下文中获取当前用户ID
	userID := middleware.GetUserID(c)
	if userID == 0 {
		fmt.Printf("[AuthDebug] ChangePassword: 未获取到用户ID\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[AuthDebug] ChangePassword: 请求参数绑定失败: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[AuthDebug] ChangePassword: 用户ID=%d\n", userID)

	// 调用服务层修改密码
	if err := service.ChangePassword(db, userID, req.OldPassword, req.NewPassword); err != nil {
		fmt.Printf("[AuthDebug] ChangePassword: 修改密码失败: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[AuthDebug] ChangePassword: 密码修改成功，用户ID=%d，请重新登录\n", userID)
	c.JSON(http.StatusOK, gin.H{
		"message": "密码修改成功，请重新登录",
	})
}
