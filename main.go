package main

import (
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

func main() {
	fmt.Println("[AuthDebug] 服务启动中...")
	DB = initDB()

	aiService := service.NewAiService(
		"95d464dd97d54610b132e8bdf10a3bc8.RI5F6mgZqz2nneLN",
		"https://open.bigmodel.cn/api/paas/v4/",
		"glm-4.7-flash",
	)

	ossService, err := service.NewOSSServiceFromEnv()
	if err != nil {
		fmt.Printf("[ChatDebug] OSS 未启用: %v\n", err)
	}

	r := gin.Default()
	routes.UserRoutes(r, DB)
	routes.ChatRoutes(r, DB, aiService, ossService)
	routes.WebSocketRoutes(r, DB, aiService)

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
