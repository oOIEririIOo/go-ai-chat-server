package main

import (
	"ai-chat/config"
	"ai-chat/models"
	"ai-chat/routes"
	"ai-chat/service"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

// initDB 初始化 SQLite 数据库并完成基础表迁移。
func initDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		log.Fatalf("无法连接数据库: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.ChatSession{}, &models.ChatMessage{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	fmt.Println("数据库迁移完成")

	return db
}

// main 负责装配所有服务并启动 HTTPS 服务。
func main() {
	fmt.Println("[AuthDebug] 服务启动中...")
	DB = initDB()

	config.LoadEnv()

	aiService := service.NewAiService(
		config.GetAIAPIKey(),
		config.GetAIBaseURL(),
		config.GetAIModelID(),
	)

	ossService, err := service.NewOSSServiceFromEnv()
	if err != nil {
		fmt.Printf("[ChatDebug] OSS 未启用: %v\n", err)
	}

	// 语音转写是可选能力；如果环境变量未配置，则仅禁用该能力，不影响主聊天服务。
	rtasrService, err := service.NewXFYunRTASRServiceFromEnv()
	if err != nil {
		fmt.Printf("[VoiceDebug] RTASR 未启用: %v\n", err)
	}

	r := gin.Default()
	routes.UserRoutes(r, DB)
	routes.ChatRoutes(r, DB, aiService, ossService)
	routes.WebSocketRoutes(r, DB, aiService)
	routes.VoiceWebSocketRoutes(r, DB, rtasrService)

	port := ":443"
	if p := os.Getenv("HTTPS_PORT"); p != "" {
		port = ":" + p
	}

	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")
	if certFile == "" {
		certFile = "server.crt"
	}
	if keyFile == "" {
		keyFile = "server.key"
	}

	fmt.Printf("[AuthDebug] HTTPS 服务配置: 端口=%s, 证书=%s, 密钥=%s\n", port, certFile, keyFile)
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		log.Fatalf("[AuthDebug] 证书文件不存在: %s", certFile)
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		log.Fatalf("[AuthDebug] 密钥文件不存在: %s", keyFile)
	}

	fmt.Printf("[AuthDebug] HTTPS 服务正在监听端口%s\n", port)
	if err := r.RunTLS(port, certFile, keyFile); err != nil {
		log.Fatalf("[AuthDebug] HTTPS 服务启动失败: %v", err)
	}
}
