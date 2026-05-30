package controller

import (
	"ai-chat/config"
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

type ChatController struct {
	chatService *service.ChatService
	db          *gorm.DB
}

func NewChatController(db *gorm.DB, aiService *service.AiService, ossService *service.OSSService) *ChatController {
	return &ChatController{
		chatService: service.NewChatService(db, aiService, ossService),
		db:          db,
	}
}

type CreateSessionRequest struct {
	Title string `json:"title" binding:"required"`
}

type StreamChatRequest struct {
	SessionID uint                          `json:"session_id" binding:"required"`
	Messages  []models.BusinessMessageInput `json:"messages" binding:"required"`
	Tools     []models.Tool                 `json:"tools,omitempty"`
	ModelKey  string                        `json:"model_key"`
	ModelId   string                        `json:"model_id"`
	ApiKey    string                        `json:"api_key"`
	BaseUrl   string                        `json:"base_url"`
}

type GenerateTitleRequest struct {
	SessionID   uint   `json:"session_id" binding:"required"`
	UserMessage string `json:"user_message" binding:"required"`
}

type GenerateTitleResponse struct {
	Title string `json:"title"`
}

type CreateChatMessageRequest struct {
	ID          string                  `json:"id" binding:"required"`
	Text        string                  `json:"text"`
	Attachments []models.AttachmentItem `json:"attachments"`
}

func (c *ChatController) CreateSession(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req CreateSessionRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := c.chatService.CreateSession(userID, req.Title)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusCreated, session)
}

func (c *ChatController) GetSessions(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessions, err := c.chatService.GetSessions(userID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, sessions)
}

func (c *ChatController) GetSessionMessages(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessionID, err := parseUintParam(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid session id"})
		return
	}

	limit := service.DefaultMessageHistoryPageSize
	if limitStr := ctx.Query("limit"); limitStr != "" {
		parsedLimit, parseErr := strconv.Atoi(limitStr)
		if parseErr != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = parsedLimit
	}

	page, err := c.chatService.GetSessionMessagesPage(userID, sessionID, ctx.Query("before_message_id"), limit)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"messages":               page.Messages,
		"has_more":               page.HasMore,
		"next_before_message_id": page.NextBeforeMessageID,
	})
}

// UploadAttachments 由后端接收 multipart 文件并转传到 OSS。
func (c *ChatController) UploadAttachments(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessionIDValue := ctx.PostForm("session_id")
	sessionID, err := parseUintParam(sessionIDValue)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid session id"})
		return
	}

	if _, err := c.chatService.GetOwnedSession(userID, sessionID); err != nil {
		ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	form, err := ctx.MultipartForm()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
		return
	}

	files := form.File["files[]"]
	if len(files) == 0 {
		files = form.File["files"]
	}
	if len(files) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "no files uploaded"})
		return
	}

	items := make([]models.AttachmentItem, 0, len(files))
	for _, fileHeader := range files {
		uploaded, uploadErr := c.chatService.UploadAttachment(ctx, userID, sessionID, fileHeader)
		if uploadErr != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": uploadErr.Error()})
			return
		}
		items = append(items, uploaded.Item)
	}

	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateChatMessage 创建正式用户消息，不在这里触发 AI。
func (c *ChatController) CreateChatMessage(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessionID, err := parseUintParam(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid session id"})
		return
	}

	var req CreateChatMessageRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message, err := c.chatService.CreateUserMessage(userID, sessionID, req.ID, req.Text, req.Attachments)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusCreated, message)
}

func (c *ChatController) StreamChat(ctx *gin.Context) {
	var req StreamChatRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	modelCfg, err := resolveRuntimeModelConfig(req.ModelKey, req.ModelId, req.ApiKey, req.BaseUrl)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	aiService := service.NewAiService(modelCfg.ApiKey, modelCfg.BaseURL, modelCfg.ModelID)
	chatService := service.NewChatService(c.db, aiService, c.chatService.GetOSSService())
	dataChan, errChan := chatService.StreamChat(req.SessionID, req.Messages, req.Tools)

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Transfer-Encoding", "chunked")

	ctx.Stream(func(w io.Writer) bool {
		select {
		case content := <-dataChan:
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				fmt.Fprintf(w, "data: %s\n", line)
			}
			fmt.Fprint(w, "\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return true
		case err := <-errChan:
			if err != nil {
				fmt.Fprintf(w, "data: [ERROR] %s\n\n", err.Error())
			}
			return false
		}
	})
}

func resolveRuntimeModelConfig(modelKey, requestedModelID, requestedAPIKey, requestedBaseURL string) (config.ModelRuntimeConfig, error) {
	if trimmedKey := strings.TrimSpace(modelKey); trimmedKey != "" {
		cfg, ok := config.GetModelConfig(trimmedKey)
		if !ok {
			return config.ModelRuntimeConfig{}, fmt.Errorf("unsupported or incomplete model_key: %s", trimmedKey)
		}
		return cfg, nil
	}

	return config.ModelRuntimeConfig{
		ApiKey:  config.FirstNonEmpty(requestedAPIKey, config.GetAIAPIKey()),
		BaseURL: config.FirstNonEmpty(requestedBaseURL, config.GetAIBaseURL()),
		ModelID: config.FirstNonEmpty(requestedModelID, config.GetAIModelID()),
	}, nil
}

func (c *ChatController) DeleteSession(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	sessionID, err := parseUintParam(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "无效的会话ID"})
		return
	}

	if err := c.chatService.DeleteSession(userID, sessionID); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

func (c *ChatController) DeleteAllSessions(ctx *gin.Context) {
	userID := middleware.GetUserID(ctx)
	if userID == 0 {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	if err := c.chatService.DeleteAllSessions(userID); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "所有会话已删除"})
}

func (c *ChatController) GenerateTitle(ctx *gin.Context) {
	var req GenerateTitleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	title, err := c.chatService.GenerateTitle(req.UserMessage)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := c.chatService.UpdateSessionTitle(req.SessionID, title); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, GenerateTitleResponse{Title: title})
}

func parseUintParam(raw string) (uint, error) {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(value), nil
}
