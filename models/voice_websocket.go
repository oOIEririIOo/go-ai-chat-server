package models

// VoiceWSClientMessage 是客户端发给语音转写 WebSocket 的控制消息。
//
// 第一版协议只保留最小控制面：
// - start: 开始一轮语音转写
// - stop: 结束本轮语音转写
// - cancel: 取消本轮语音转写
type VoiceWSClientMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Lang      string `json:"lang,omitempty"`
}

// VoiceWSServerMessage 是服务端回给客户端的语音转写消息。
//
// text 只在最终结果返回时使用；message 用于 ready / error 等提示性消息。
type VoiceWSServerMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Message   string `json:"message,omitempty"`
}
