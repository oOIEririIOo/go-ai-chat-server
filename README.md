# AiChatBackend

Go 后端，负责 XMLAPP 的用户认证、聊天会话、附件上传、SSE 流式聊天和 WebSocket 流式聊天。

## 当前模型配置方式

- 前端不再持有真实模型 `apiKey` 和 `baseUrl`
- 前端只传受控的 `model_key`
- 后端根据 `.env` 中的白名单模型配置解析真实凭据

当前支持的 `model_key`：

- `qwen3.5-flash`
- `zhipu-glm-4-flash`
- `openai-gpt-3.5`
- `deepseek-chat`

## 环境配置

1. 复制 `.env.example` 为 `.env`
2. 填入默认模型配置
3. 按需填入各 `model_key` 对应的模型凭据

```powershell
Copy-Item .env.example .env
```

## 运行

```powershell
go run .
```

## 注意事项

- 真实模型 key 只应保存在后端 `.env`
- `.env`、证书、数据库和本地二进制都不应提交到公开仓库
- 如果前端历史上暴露过真实 key，迁移后应立即轮换
