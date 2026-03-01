package service

import (
	"ai-chat/models"
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

// SendStreamRequest 发送流式请求到AI服务
func (s *AiService) SendStreamRequest(messages []models.AiMessage, enableWebSearch, enableThinking, enableCodeInterpreter bool) (<-chan string, <-chan error) {
	dataChan := make(chan string)
	errChan := make(chan error)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		req := models.AiRequest{
			Model:    s.ModelId,
			Messages: messages,
			Stream:   true,
		}

		// 添加 tools
		var tools []models.Tool
		if enableWebSearch {
			tools = append(tools, models.Tool{Type: "web_search"})
		}
		if enableCodeInterpreter {
			tools = append(tools, models.Tool{Type: "code_interpreter"})
		}
		if len(tools) > 0 {
			req.Tools = tools
		}

		// 禁用思考模式（前端暂不支持设置，默认禁用）
		enableThinking := false
		req.EnableThinking = &enableThinking

		jsonData, err := json.Marshal(req)
		if err != nil {
			errChan <- fmt.Errorf("JSON 编码失败: %v", err)
			return
		}

		fmt.Printf("[AiDebug] 请求URL: %schat/completions\n", s.BaseUrl)
		fmt.Printf("[AiDebug] 请求体: %s\n", string(jsonData))

		httpReq, err := http.NewRequest("POST", s.BaseUrl+"chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			errChan <- fmt.Errorf("创建请求失败: %v", err)
			return
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+s.ApiKey)
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")

		client := &http.Client{}
		resp, err := client.Do(httpReq)
		if err != nil {
			errChan <- fmt.Errorf("请求失败: %v", err)
			return
		}
		defer resp.Body.Close()

		fmt.Printf("[AiDebug] AI响应状态码: %d\n", resp.StatusCode)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("[AiDebug] AI错误响应: %s\n", string(body))
			errChan <- fmt.Errorf("API 错误: %d - %s", resp.StatusCode, string(body))
			return
		}

		reader := resp.Body
		buf := make([]byte, 1024)
		var buffer strings.Builder

		for {
			n, err := reader.Read(buf)
			if err != nil && err != io.EOF {
				errChan <- fmt.Errorf("读取响应失败: %v", err)
				return
			}
			if n == 0 {
				break
			}

			data := string(buf[:n])
			lines := strings.Split(data, "\n")

			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data: ") {
					jsonData := strings.TrimPrefix(line, "data: ")
					if jsonData == "[DONE]" {
						fmt.Printf("[AiDebug] 收到 [DONE] 信号\n")
						return
					}

					var streamResp models.AiResponse
					if err := json.Unmarshal([]byte(jsonData), &streamResp); err != nil {
						fmt.Printf("[AiDebug] JSON解析失败: %v, 原始数据: %s\n", err, jsonData)
						continue
					}

					if len(streamResp.Choices) > 0 {
						// 处理思考内容（reasoning_content）
						if streamResp.Choices[0].Delta.ReasoningContent != "" {
							// 保留原始内容（包括换行符），Controller 会处理 SSE 格式
							reasoningContent := streamResp.Choices[0].Delta.ReasoningContent
							buffer.WriteString(reasoningContent)
							fmt.Printf("[AiDebug] 思考内容: %s\n", reasoningContent)
							dataChan <- buffer.String()
						}
						// 处理正常内容
						if streamResp.Choices[0].Delta.Content != "" {
							// 保留原始内容（包括换行符），Controller 会处理 SSE 格式
							content := streamResp.Choices[0].Delta.Content
							buffer.WriteString(content)
							fmt.Printf("[AiDebug] 正常内容: %s\n", content)
							dataChan <- buffer.String()
						}
					}
				}
			}
		}
	}()

	return dataChan, errChan
}

// GenerateTitle 根据用户消息生成会话标题
func (s *AiService) GenerateTitle(userMessage string) (string, error) {
	prompt := fmt.Sprintf(`请为以下用户消息生成一个简短的会话标题（不超过10个字），只需要返回标题内容，不要有任何解释或标点符号：

用户消息：%s

标题：`, userMessage)

	messages := []models.AiMessage{
		{Role: "user", Content: prompt},
	}

	req := models.AiRequest{
		Model:    s.ModelId,
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("JSON 编码失败: %v", err)
	}

	httpReq, err := http.NewRequest("POST", s.BaseUrl+"chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.ApiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API 错误: %d - %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	var aiResp models.AiResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if len(aiResp.Choices) > 0 {
		title := strings.TrimSpace(aiResp.Choices[0].Message.Content)
		title = strings.Trim(title, `"'"'"'

`)
		if title != "" {
			return title, nil
		}
	}

	return "新会话", nil
}
