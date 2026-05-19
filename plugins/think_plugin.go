package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/tools"
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

	// 自动从知识库检索相关上下文
	knowledgeContext := retrieveKnowledge(userText)
	if knowledgeContext != "" {
		fmt.Printf("[思考] 已检索到知识上下文 (%d 字符)\n", len(knowledgeContext))
	}

	reply, err := callLLMWithContext(userText, msg.Provider, knowledgeContext)
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

// retrieveKnowledge 硬化检索：BOW 向量 + 类型权重。
// 返回格式化的知识上下文字符串，无结果时返回空串。
// 全程确定性代码，不走 LLM。
func retrieveKnowledge(userInput string) string {
	if len(userInput) < 2 {
		return ""
	}

	matches := tools.KnowledgeRetrieve(userInput, 5)
	if len(matches) == 0 {
		return ""
	}

	fmt.Printf("[思考] 知识检索命中 %d 条 (top: %.3f %s)\n",
		len(matches), matches[0].Score, matches[0].Title)

	return tools.FormatKnowledgeContext(matches)
}

// callLLM 调 LLM API 生成回答，provider 为空时使用默认 provider
func callLLM(prompt string, provider string) (string, error) {
	return callLLMWithContext(prompt, provider, "")
}

// callLLMWithContext 调 LLM API，可选注入知识上下文到 system prompt。
// knowledgeContext 为空时等同于 callLLM。
func callLLMWithContext(prompt string, provider string, knowledgeContext string) (string, error) {
	var apiKey, model, endpoint string
	if provider != "" {
		apiKey = llm.APIKeyFor(provider)
		model = llm.ModelFor(provider)
		endpoint = llm.ChatEndpointFor(provider)
	} else {
		apiKey = llm.APIKey()
		model = llm.Model()
		endpoint = llm.ChatEndpoint()
	}
	if apiKey == "" {
		return "", fmt.Errorf("LLM_API_KEY 未设置（可设 DEEPSEEK_API_KEY）")
	}

	// 构建 system prompt：有知识上下文时注入
	sysPrompt := systemPrompt
	if knowledgeContext != "" {
		sysPrompt += "\n\n## 相关知识库条目\n以下是与用户问题相关的已有知识，请参考这些信息来回答：\n\n" + knowledgeContext
	}

	fmt.Printf("[思考] LLM 调用: model=%s, endpoint=%s, prompt_len=%d, context_len=%d\n", model, endpoint, len(prompt), len(knowledgeContext))

	body, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": prompt},
		},
	})

	req, _ := http.NewRequest("POST", endpoint,
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

	// 解析响应
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		fmt.Printf("[思考] LLM 响应解析失败: %v, body=%s\n", err, string(respBody)[:min(len(respBody), 500)])
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Error != nil {
		fmt.Printf("[思考] LLM API 错误: %s (%s)\n", result.Error.Message, result.Error.Type)
		return "", fmt.Errorf("LLM API 错误: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		fmt.Printf("[思考] LLM 未返回结果, body=%s\n", string(respBody)[:min(len(respBody), 500)])
		return "", fmt.Errorf("LLM 未返回结果")
	}

	content := result.Choices[0].Message.Content
	fmt.Printf("[思考] LLM 响应长度: %d\n", len(content))
	return content, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
