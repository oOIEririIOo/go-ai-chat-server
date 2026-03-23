package service

import (
	"ai-chat/models"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type AiService struct {
	ApiKey  string
	BaseUrl string
	ModelId string
}

func NewAiService(apiKey, baseUrl, modelId string) *AiService {
	return &AiService{
		ApiKey:  apiKey,
		BaseUrl: baseUrl,
		ModelId: modelId,
	}
}

// StreamResponse 是返回给前端流式层的统一结构。
type StreamResponse struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	IsReasoning      bool   `json:"is_reasoning"`
}

// SendStreamRequest 把已经转换好的 AI 消息发送到 chat/completions。
func (s *AiService) SendStreamRequest(messages []models.AiChatMessage, tools []models.Tool) (<-chan string, <-chan error) {
	dataChan := make(chan string)
	errChan := make(chan error)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		reqMap := map[string]interface{}{
			"model":    s.ModelId,
			"messages": messages,
			"stream":   true,
		}

		var hasThinking bool
		for _, tool := range tools {
			reqMap[tool.Type] = true
			fmt.Printf("[AiDebug] enable tool: %s = true\n", tool.Type)
			if tool.Type == "enable_thinking" {
				hasThinking = true
			}
		}

		if !hasThinking {
			reqMap["enable_thinking"] = false
			fmt.Printf("[AiDebug] enable_thinking = false\n")
		}

		jsonData, err := json.Marshal(reqMap)
		if err != nil {
			errChan <- fmt.Errorf("JSON encode failed: %v", err)
			return
		}

		fmt.Printf("[AiDebug] request URL: %schat/completions\n", s.BaseUrl)
		fmt.Printf("[AiDebug] request body: %s\n", string(jsonData))

		httpReq, err := http.NewRequest("POST", s.BaseUrl+"chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			errChan <- fmt.Errorf("create request failed: %v", err)
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+s.ApiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")

		client := &http.Client{}
		resp, err := client.Do(httpReq)
		if err != nil {
			errChan <- fmt.Errorf("request failed: %v", err)
			return
		}
		defer resp.Body.Close()

		fmt.Printf("[AiDebug] response status: %d\n", resp.StatusCode)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("[AiDebug] error response: %s\n", string(body))
			errChan <- fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
			return
		}

		reader := bufio.NewReader(resp.Body)
		var reasoningBuffer strings.Builder
		var contentBuffer strings.Builder
		var eventData strings.Builder

		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				errChan <- fmt.Errorf("read response failed: %v", readErr)
				return
			}

			trimmedLine := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmedLine, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(payload)
			}

			if trimmedLine == "" && eventData.Len() > 0 {
				if s.processStreamEvent(eventData.String(), &reasoningBuffer, &contentBuffer, dataChan) {
					return
				}
				eventData.Reset()
			}

			if readErr == io.EOF {
				if eventData.Len() > 0 {
					if s.processStreamEvent(eventData.String(), &reasoningBuffer, &contentBuffer, dataChan) {
						return
					}
				}
				break
			}
		}
	}()

	return dataChan, errChan
}

func (s *AiService) processStreamEvent(event string, reasoningBuffer, contentBuffer *strings.Builder, dataChan chan<- string) bool {
	jsonData := strings.TrimSpace(event)
	if jsonData == "" {
		return false
	}

	if jsonData == "[DONE]" {
		fmt.Printf("[AiDebug] received [DONE]\n")
		return true
	}

	var streamResp models.AiResponse
	if err := json.Unmarshal([]byte(jsonData), &streamResp); err != nil {
		fmt.Printf("[AiDebug] decode stream event failed: %v, raw=%s\n", err, jsonData)
		return false
	}

	if len(streamResp.Choices) == 0 {
		return false
	}

	delta := streamResp.Choices[0].Delta
	if delta.ReasoningContent != "" {
		reasoningBuffer.WriteString(delta.ReasoningContent)

		response := StreamResponse{
			Content:          contentBuffer.String(),
			ReasoningContent: reasoningBuffer.String(),
			IsReasoning:      true,
		}
		respJSON, _ := json.Marshal(response)
		dataChan <- string(respJSON)
	}

	if delta.Content != "" {
		contentBuffer.WriteString(delta.Content)

		response := StreamResponse{
			Content:          contentBuffer.String(),
			ReasoningContent: reasoningBuffer.String(),
			IsReasoning:      false,
		}
		respJSON, _ := json.Marshal(response)
		dataChan <- string(respJSON)
	}

	return false
}

// GenerateTitle 继续使用纯文本请求，不引入图片上下文。
func (s *AiService) GenerateTitle(userMessage string) (string, error) {
	prompt := fmt.Sprintf(
		"请为以下用户消息生成一个简短的会话标题（不超过10个字），只需要返回标题内容，不要有任何解释或标点符号。用户消息：%s\n\n标题：",
		userMessage,
	)

	messages := []models.AiChatMessage{
		{Role: "user", Content: prompt},
	}

	req := models.AiChatCompletionRequest{
		Model:    s.ModelId,
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("JSON encode failed: %v", err)
	}

	httpReq, err := http.NewRequest("POST", s.BaseUrl+"chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.ApiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response failed: %v", err)
	}

	var aiResp models.AiResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", fmt.Errorf("decode response failed: %v", err)
	}

	if len(aiResp.Choices) > 0 {
		title := strings.TrimSpace(aiResp.Choices[0].Message.Content)
		title = strings.Trim(title, "\"'` \n\r\t")
		if title != "" {
			return title, nil
		}
	}

	return "新会话", nil
}
