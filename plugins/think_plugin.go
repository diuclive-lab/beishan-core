package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"beishan/kernel"
)

/* ThinkPlugin （思考插件）

   处理一般对话、问答、闲聊、创意写作。
   收到 chat 消息后调 DeepSeek 生成回答。

   当用户直接说"你好"、"帮我写一首诗"等通用请求时，
   DeepSeek 路由到本插件，由本插件再调 DeepSeek 生成回答。
*/
type ThinkPlugin struct{}

func (p *ThinkPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "chat" {
		return kernel.Message{}, fmt.Errorf("think_plugin: 未知类型 %s", msg.Type)
	}

	userText := string(msg.Payload)
	userText = strings.TrimFunc(userText, func(r rune) bool { return r == '"' })

	reply, err := callDeepSeek(userText)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	replyJSON, _ := json.Marshal(reply)
	fmt.Printf("[思考] %s\n", truncate(reply, 120))
	return kernel.Message{
		Type:    "chat.response",
		Payload: replyJSON,
	}, nil
}

// callDeepSeek 调 DeepSeek API 生成回答
func callDeepSeek(prompt string) (string, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未设置")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, _ := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API 调用失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("DeepSeek 未返回结果")
	}

	return result.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
