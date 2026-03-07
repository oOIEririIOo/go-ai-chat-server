package controller

import (
	"ai-chat/middleware"
	"ai-chat/models"
	"ai-chat/service"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChatController 聊天控制器
type ChatController struct {
	chatService *service.ChatService
	db          *gorm.DB
}

// NewChatController 创建聊天控制器实例
// 参数:
//   - db: 数据库连接
//   - aiService: AI 服务实例（用于标题生成等默认功能）
//
// 返回值:
//   - *ChatController: 聊天控制器实例
func NewChatController(db *gorm.DB, aiService *service.AiService) *ChatController {
	return &ChatController{
		chatService: service.NewChatService(db, aiService),
		db:          db,
	}
}

// CreateSessionRequest 创建会话请求结构
type CreateSessionRequest struct {
	Title string `json:"title" binding:"required"` // 会话标题
}

// CreateSession 创建聊天会话
// 功能: 处理创建新聊天会话的请求
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) CreateSession(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到创建会话请求\n")

	// 从 JWT 获取用户ID
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req CreateSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[ChatDebug] Controller: 请求参数绑定失败: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := c.chatService.CreateSession(userID, req.Title)
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 创建会话失败: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 会话创建成功，ID=%d\n", session.ID)
	ctx.JSON(http.StatusCreated, session)
}

// GetSessions 获取会话列表
// 功能: 处理获取用户所有聊天会话的请求
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) GetSessions(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到获取会话列表请求\n")

	// 从 JWT 获取用户ID
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessions, err := c.chatService.GetSessions(userID)
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 获取会话列表失败: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 获取到 %d 个会话\n", len(sessions))
	ctx.JSON(http.StatusOK, sessions)
}

// GetSession 获取单个会话
// 功能: 处理获取指定聊天会话详情的请求
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) GetSession(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到获取会话请求\n")

	sessionIdStr := ctx.Param("id")
	sessionId, err := strconv.ParseUint(sessionIdStr, 10, 64)
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 会话ID格式错误: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "无效的会话ID"})
		return
	}

	session, err := c.chatService.GetSession(uint(sessionId))
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 获取会话失败: %v\n", err)
		ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 获取会话成功，ID=%d\n", session.ID)
	ctx.JSON(http.StatusOK, session)
}

// StreamChatRequest 流式聊天请求结构
type StreamChatRequest struct {
	SessionID uint               `json:"session_id" binding:"required"` // 会话ID
	Messages  []models.AiMessage `json:"messages" binding:"required"`   // 消息列表
	Tools     []models.Tool      `json:"tools,omitempty"`               // 工具列表（如 enable_search, enable_thinking, code_interpreter）
	// 模型配置
	ModelId string `json:"model_id" binding:"required"` // 模型 ID
	ApiKey  string `json:"api_key" binding:"required"`  // API Key
	BaseUrl string `json:"base_url" binding:"required"` // Base URL
}

// StreamChat 流式聊天
// 功能: 处理流式聊天请求，与AI模型进行实时对话
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) StreamChat(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到流式聊天请求\n")

	var req StreamChatRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[ChatDebug] Controller: 请求参数绑定失败: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 开始流式聊天，会话ID=%d，消息数=%d，模型=%s\n", req.SessionID, len(req.Messages), req.ModelId)
	fmt.Printf("[ChatDebug] Controller: BaseUrl=%s\n", req.BaseUrl)
	fmt.Printf("[ChatDebug] Controller: Tools=%v\n", req.Tools)

	// 动态创建 AiService
	aiService := service.NewAiService(req.ApiKey, req.BaseUrl, req.ModelId)
	chatService := service.NewChatService(c.db, aiService)

	dataChan, errChan := chatService.StreamChat(req.SessionID, req.Messages, req.Tools)

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Transfer-Encoding", "chunked")

	ctx.Stream(func(w io.Writer) bool {
		select {
		case content := <-dataChan:
			// 使用 SSE 多行格式：每行前面加 "data: "，空行表示结束
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprint(w, "\n") // 空行表示消息结束
			// 尝试类型断言获取 Flusher 接口
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return true
		case err := <-errChan:
			if err != nil {
				fmt.Printf("[ChatDebug] Controller: 流式聊天错误: %v\n", err)
				fmt.Fprintf(w, "data: [ERROR] %s\n\n", err.Error())
			}
			return false
		}
	})

	fmt.Printf("[ChatDebug] Controller: 流式聊天完成\n")
}

// DeleteSession 删除会话
// 功能: 处理删除指定聊天会话的请求
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) DeleteSession(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到删除会话请求\n")

	sessionIdStr := ctx.Param("id")
	sessionId, err := strconv.ParseUint(sessionIdStr, 10, 64)
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 会话ID格式错误: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "无效的会话ID"})
		return
	}

	if err := c.chatService.DeleteSession(uint(sessionId)); err != nil {
		fmt.Printf("[ChatDebug] Controller: 删除会话失败: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 会话删除成功，ID=%d\n", sessionId)
	ctx.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// DeleteAllSessions 删除所有会话
// 功能: 处理删除用户所有聊天会话的请求
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) DeleteAllSessions(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到删除所有会话请求\n")

	// 从 JWT 获取用户ID
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	if err := c.chatService.DeleteAllSessions(userID); err != nil {
		fmt.Printf("[ChatDebug] Controller: 删除所有会话失败: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 所有会话删除成功，用户ID=%d\n", userID)
	ctx.JSON(http.StatusOK, gin.H{"message": "所有会话已删除"})
}

// GenerateTitleRequest 生成标题请求结构
type GenerateTitleRequest struct {
	SessionId   uint   `json:"session_id" binding:"required"`   // 会话 ID
	UserMessage string `json:"user_message" binding:"required"` // 用户消息内容
}

// GenerateTitleResponse 生成标题响应结构
type GenerateTitleResponse struct {
	Title string `json:"title"` // 生成的标题
}

// GenerateTitle 生成会话标题
// 功能: 根据用户消息生成会话标题，并更新数据库
// 参数:
//   - ctx: Gin 上下文
func (c *ChatController) GenerateTitle(ctx *gin.Context) {
	fmt.Printf("[ChatDebug] Controller: 收到生成标题请求\n")

	var req GenerateTitleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		fmt.Printf("[ChatDebug] Controller: 请求参数绑定失败: %v\n", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("[ChatDebug] Controller: 开始生成标题，会话ID=%d, 用户消息长度=%d\n", req.SessionId, len(req.UserMessage))

	title, err := c.chatService.GenerateTitle(req.UserMessage)
	if err != nil {
		fmt.Printf("[ChatDebug] Controller: 生成标题失败: %v\n", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新数据库中的会话标题
	if err := c.chatService.UpdateSessionTitle(req.SessionId, title); err != nil {
		fmt.Printf("[ChatDebug] Controller: 更新会话标题失败: %v\n", err)
		// 不影响返回结果，继续返回生成的标题
	}

	fmt.Printf("[ChatDebug] Controller: 标题生成成功: %s\n", title)
	ctx.JSON(http.StatusOK, GenerateTitleResponse{Title: title})
}
