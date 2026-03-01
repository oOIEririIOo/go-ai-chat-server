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

	// 自动迁移所有模型
	db.AutoMigrate(&models.User{}, &models.ChatSession{}, &models.ChatMessage{})
	fmt.Println("数据库迁移完成")

	return db
}

func main() {
	fmt.Println("[AuthDebug] 服务器启动中...")

	// 初始化数据库
	DB = initDB()

	// 初始化 AI 服务（使用智谱 GLM-4.7-Flash 配置）
	aiService := service.NewAiService(
		"95d464dd97d54610b132e8bdf10a3bc8.RI5F6mgZqz2nneLN",
		"https://open.bigmodel.cn/api/paas/v4/",
		"glm-4.7-flash",
	)

	// 初始化 Gin 引擎
	r := gin.Default()

	// 注册路由
	routes.UserRoutes(r, DB)
	routes.ChatRoutes(r, DB, aiService)

	// 读取环境变量以配置 TLS
	port := ":443"
	if p := os.Getenv("HTTPS_PORT"); p != "" {
		port = ":" + p
	}

	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")
	if certFile == "" {
		certFile = "server.crt" // 默认路径，可在开发环境生成自签名证书
	}
	if keyFile == "" {
		keyFile = "server.key"
	}

	fmt.Printf("[AuthDebug] HTTPS 服务器配置: 端口=%s, 证书=%s, 密钥=%s\n", port, certFile, keyFile)

	// 检查证书文件是否存在
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		log.Fatalf("[AuthDebug] 证书文件不存在: %s\n请运行以下命令生成自签名证书:\nopenssl genrsa -out server.key 2048\nopenssl req -new -key server.key -out server.csr -subj \"/C=CN/ST=State/L=City/O=Organization/CN=localhost\"\nopenssl x509 -req -days 365 -in server.csr -signkey server.key -out server.crt", certFile)
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		log.Fatalf("[AuthDebug] 密钥文件不存在: %s", keyFile)
	}

	fmt.Printf("[AuthDebug] HTTPS 服务器正在监听端口 %s\n", port)
	fmt.Printf("[AuthDebug] 可用接口:\n")
	fmt.Printf("[AuthDebug]   POST /user/register\n")
	fmt.Printf("[AuthDebug]   POST /user/login\n")
	fmt.Printf("[AuthDebug]   POST /chat/sessions\n")
	fmt.Printf("[AuthDebug]   GET  /chat/sessions\n")
	fmt.Printf("[AuthDebug]   GET  /chat/sessions/:id\n")
	fmt.Printf("[AuthDebug]   DELETE /chat/sessions/:id\n")
	fmt.Printf("[AuthDebug]   POST /chat/stream\n")

	if err := r.RunTLS(port, certFile, keyFile); err != nil {
		log.Fatalf("[AuthDebug] HTTPS 服务器启动失败: %v", err)
	}
}
