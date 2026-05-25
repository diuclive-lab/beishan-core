package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"beishan/internal/llm"
)

/* Decision 是 DeepSeek 路由决策的结构化输出。

   MsgType 让 DeepSeek 同时决定消息类型，避免"chat"类型送到所有插件。
   例如用户说"搜索新闻"→ Route 返回 {Recipient:"search_plugin", MsgType:"web_search"}。
*/
type Decision struct {
	Recipient       string           `json:"recipient"`
	MsgType         string           `json:"msg_type,omitempty"`
	Payload         json.RawMessage  `json:"payload,omitempty"`
	Reason          string           `json:"reason"`
	Confidence      float64          `json:"confidence"`
	SearchSuggestion string          `json:"search_suggestion,omitempty"`
}

/* pluginEntry 存储插件名 + 描述，用于构建路由 prompt。 */
type pluginEntry struct {
	name        string
	description string
	example     string
}

/* RouteStrategy 路由策略接口。可替换实现用于测试或降级。 */
type RouteStrategy interface {
	Route(msg Message) (*Decision, error)
}

/* Router 封装 DeepSeek API 调用。

   职责只有一个：根据消息内容，返回应该发给哪个插件。
   不缓存决策，不做策略，不写规则。每次调用都是独立的，无状态。

   checkRecipient 由 Kernel 在 NewKernel 时注入。
   knownPlugins 由 Kernel.Register 通过 AddKnownPlugin 自动维护，
   无需手动调用 SetPlugins。
*/
type Router struct {
	mu       sync.RWMutex
	strategy        RouteStrategy
	apiKey          string
	client          *http.Client
	knownPlugins    []pluginEntry
	workflowSummary string
}

/* AddKnownPlugin 添加插件名、描述和示例调用到路由列表。

   由 Kernel.Register 自动调用，外部勿调。
   description 和 example 合并后注入 Router prompt，帮 DeepSeek 理解用法。
*/
func (r *Router) AddKnownPlugin(name, description, example string) {
	r.knownPlugins = append(r.knownPlugins, pluginEntry{name, description, example})
}

/* SetWorkflowSummary 注入可用 workflow 列表，Router 会追加到插件列表之后。
   由 main.go 在启动时扫描 workflows/ 目录后调用。 */
func (r *Router) SetWorkflowSummary(summary string) {
	r.workflowSummary = summary
}

func (r *Router) SetStrategy(s RouteStrategy) { r.mu.Lock(); defer r.mu.Unlock(); r.strategy = s }

func NewRouter(apiKey string) *Router {
	return &Router{
		apiKey: apiKey,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

/* buildPluginList 构建给 DeepSeek 的插件列表字符串。

   有描述时：
     - search_plugin: 通用网络搜索，适用于查找资料、新闻、技术文档
     - write_plugin: 文本生成与写作，适用于生成报告、摘要、邮件

   无描述时退化为裸名字列表，向后兼容。
*/
func (r *Router) buildPluginList() string {
	var sb strings.Builder
	for _, e := range r.knownPlugins {
		sb.WriteString(fmt.Sprintf("- %s: %s", e.name, e.description))
		if e.example != "" {
			sb.WriteString(fmt.Sprintf("\n  示例: %s", e.example))
		}
		sb.WriteString("\n")
	}
	if r.workflowSummary != "" {
		sb.WriteString("\nAvailable workflows (via workflow_plugin):\n")
		sb.WriteString(r.workflowSummary)
	}
	return sb.String()
}

/* Route 强制经过 DeepSeek 做路由决策，不可绕过。

   prompt 结构：
   1. 输出格式约束
   2. 可选插件列表（名字 + 描述，从注册表自动读取）
   3. 输入消息

   没有角色扮演，没有思维链。
   所有逻辑判断在硬化层（parseDecision）里做。
*/
func (r *Router) Route(msg Message) (*Decision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.strategy != nil {
		return r.strategy.Route(msg)
	}
	pluginList := r.buildPluginList()

		promptTmpl := llm.RouterPrompt()
		prompt := fmt.Sprintf(promptTmpl,
			pluginList,
			msg.Type+": "+string(msg.Payload),
		)

	resp, err := r.callDeepSeek(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}

	return r.parseDecision(resp)
}

func (r *Router) callDeepSeek(prompt string) (string, error) {
	apiKey := llm.APIKey()
	if apiKey == "" {
		apiKey = r.apiKey
	}
	if apiKey == "" {
		return "", fmt.Errorf("LLM_API_KEY 未设置")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": llm.Model(),
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, _ := http.NewRequest("POST", llm.ChatEndpoint(),
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLM 未返回结果")
	}

	// 采集路由调用 token 消耗
	if result.Usage.TotalTokens > 0 {
		llm.RecordUsage("router", &llm.Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
			Model:            llm.Model(),
		})
	}

	return result.Choices[0].Message.Content, nil
}

/* parseDecision 硬化层，三层验证：格式、置信度、收件人合法性。

   先剥离可能的 markdown 标记（DeepSeek 有时返回 ```json、```、
   或单独的 ` 反引号包裹），再用标准 JSON 解析。
*/
func (r *Router) parseDecision(raw string) (*Decision, error) {
	// 剥离 markdown JSON 标记
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```js")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var d Decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("路由决策解析失败: %w", err)
	}

	if d.Confidence < 0.4 {
		return nil, fmt.Errorf("置信度过低: %.2f，决策: %+v", d.Confidence, d)
	}

	valid := false
	for _, e := range r.knownPlugins {
		if e.name == d.Recipient {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("无效收件人: %s", d.Recipient)
	}

	log.Printf("[Router] 路由决策: %s (%.2f) — %s", d.Recipient, d.Confidence, d.Reason)
	return &d, nil
}
