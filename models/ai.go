package models

type AiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Type string `json:"type"`
}

type ExtraBody struct {
	EnableThinking bool `json:"enable_thinking"`
}

type AiRequest struct {
	Model          string      `json:"model"`
	Messages       []AiMessage `json:"messages"`
	Stream         bool        `json:"stream"`
	EnableSearch   *bool       `json:"enable_search,omitempty"`
	EnableThinking *bool       `json:"enable_thinking,omitempty"`
}

type AiChoice struct {
	Index   int       `json:"index"`
	Message AiMessage `json:"message"`
	Delta   AiDelta   `json:"delta,omitempty"`
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
	SessionID      string      `json:"session_id"`
	Messages       []AiMessage `json:"messages"`
	Model          string      `json:"model,omitempty"`
	Tools          []Tool      `json:"tools,omitempty"` // 接收前端传来的 tools 列表
	EnableThinking bool        `json:"enable_thinking,omitempty"`
}
