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

	userText := extractPrompt(msg.Payload)

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

var systemPrompt = `你是 beishan-core 智能助手。你所在的系统提供以下能力：

- 搜索网络（search_plugin）
- 读写文件（write_plugin）
- 执行终端命令（terminal_plugin）
- 浏览器导航与网页提取（browser_plugin）
- 记忆存储与检索（memory_plugin）
- 待办管理（todo_plugin）
- 文本转语音（tts_plugin）
- 图片生成（image_gen_plugin）
- 法律审查工作流（workflow_plugin）

当用户需要上述能力时，请明确告知用户可以发送指定请求。
例如："你可以说'帮我搜索XX'来完成搜索"。

对于你能直接回答的问题（闲聊、知识、创作等），直接回答。
不需要说明自己是AI助手。`

// callDeepSeek 调 DeepSeek API 生成回答
func callDeepSeek(prompt string) (string, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("DEEPSEEK_API_KEY 未设置")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
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

// extractPrompt 从 Payload 中提取用户提示文本。
// 支持两种格式：
//   - JSON 对象：提取 message 字段（workflow 场景）
//   - JSON 字符串：直接返回（普通场景）
func extractPrompt(payload []byte) string {
	s := string(payload)
	// 尝试解析为 JSON 对象，提取 message 字段
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) == nil {
		if msg, ok := obj["message"].(string); ok {
			return msg
		}
	}
	// 回退：去掉外层引号
	return strings.TrimFunc(s, func(r rune) bool { return r == '"' })
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
