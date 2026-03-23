package models

// BusinessMessageInput 是业务层消息的统一输入结构。
// 它保留 content + attachments 的存储/展示语义，供 HTTP 和 WebSocket 两条链路复用。
type BusinessMessageInput struct {
	ID          string           `json:"id,omitempty"`
	Role        string           `json:"role"`
	Content     string           `json:"content"`
	MessageType string           `json:"message_type,omitempty"`
	Attachments []AttachmentItem `json:"attachments,omitempty"`
}

// AiImageURL 对应 OpenAI chat/completions 中 image_url 块的嵌套结构。
type AiImageURL struct {
	URL string `json:"url"`
}

// AiContentPart 对应 chat/completions 的多模态内容块。
type AiContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *AiImageURL `json:"image_url,omitempty"`
}

// AiChatMessage 是发给 AI 的最终消息结构。
// Content 既可能是纯文本字符串，也可能是内容块数组。
type AiChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type Tool struct {
	Type string `json:"type"`
}

type ExtraBody struct {
	EnableThinking bool `json:"enable_thinking"`
}

// AiChatCompletionRequest 对应当前使用的 chat/completions 请求体。
type AiChatCompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []AiChatMessage `json:"messages"`
	Stream         bool            `json:"stream"`
	EnableSearch   *bool           `json:"enable_search,omitempty"`
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
}

// AiResponseMessage 只用于解析非流式响应里的 message。
type AiResponseMessage struct {
	Content string `json:"content"`
}

type AiChoice struct {
	Index   int               `json:"index"`
	Message AiResponseMessage `json:"message"`
	Delta   AiDelta           `json:"delta,omitempty"`
}

type AiDelta struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type AiResponse struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []AiChoice `json:"choices"`
}

type ChatStreamRequest struct {
	SessionID      string                 `json:"session_id"`
	Messages       []BusinessMessageInput `json:"messages"`
	Model          string                 `json:"model,omitempty"`
	Tools          []Tool                 `json:"tools,omitempty"`
	EnableThinking bool                   `json:"enable_thinking,omitempty"`
}
