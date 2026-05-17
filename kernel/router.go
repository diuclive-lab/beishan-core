package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"beishan/internal/tools"
)

/* Decision 是 DeepSeek 路由决策的结构化输出。

   Recipient：目标插件名，必须是已注册的插件之一
   Reason：路由理由（仅用于日志审计）
   Confidence：置信度，低于阈值则拒绝该决策
*/
type Decision struct {
	Recipient  string  `json:"recipient"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

/* Router 封装 DeepSeek API 调用。

   它的职责只有一个：根据消息内容，返回应该发给哪个插件。
   不缓存决策，不做策略，不写规则。
   每次调用都是独立的，无状态。

   checkRecipient 由 Kernel 在 NewKernel 时注入，
   同时检查 Schema 注册中心（工具）和 Kernel 插件表（插件）。

   extraNames 由 Kernel.Register 自动追加，
   确保 DeepSeek 提示词中包含所有已注册的插件名。
*/
type Router struct {
	apiKey         string
	client         *http.Client
	checkRecipient func(name string) bool
	extraNames     []string
}

/* AddKnownName 添加一个已知插件名到路由提示词列表。

   由 Kernel.Register 自动调用，外部勿调。
*/
func (r *Router) AddKnownName(name string) {
	r.extraNames = append(r.extraNames, name)
}

func NewRouter(apiKey string) *Router {
	return &Router{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

/* SetRecipientValidator 设置收件人验证函数。

   由 Kernel.NewKernel 自动注入，同时检查工具和插件。
   外部勿调用。
*/
func (r *Router) SetRecipientValidator(fn func(name string) bool) {
	r.checkRecipient = fn
}

/* Route 强制经过 DeepSeek 做路由决策，不可绕过。

   提示词极短，只有三行：
   1. 输出格式约束（JSON Schema 描述）
   2. 可选插件的列表
   3. 输入消息

   没有角色扮演，没有思维链，没有"你是一个 xxx 专家"。
   所有逻辑判断在硬化层（parseDecision）里做，不在提示词里做。
*/
func (r *Router) Route(msg Message) (*Decision, error) {
	available := tools.GetAvailableTools()
	available = append(available, r.extraNames...)

	prompt := fmt.Sprintf(
		`Output JSON: {"recipient":"","reason":"","confidence":0.0}
Available: %v
Input: %s`, available, msg.Type+": "+string(msg.Payload))

	resp, err := r.callDeepSeek(prompt)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek 调用失败: %w", err)
	}

	return r.parseDecision(resp)
}

func (r *Router) callDeepSeek(prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, _ := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("DeepSeek 未返回结果")
	}

	return result.Choices[0].Message.Content, nil
}

/* parseDecision 是硬化层（确定性代码）。

   DeepSeek 返回的 JSON 必须经过三层验证：
   1. JSON 格式是否正确
   2. 置信度是否高于阈值（< 0.4 拒绝）
   3. Recipient 是否在 Schema 注册中心中存在

   任何一层不通过，返回错误，不走降级。
   这是防止"规则悄悄替代 AI"的关键防线。

   注意：Router 只查询工具是否存在（GetToolSchema），
   绝不接触 Payload 内容，遵守 Payload 对内核不透明铁律。
   真正的参数校验在 L3 ValidateAndExecute 中进行。
*/
func (r *Router) parseDecision(raw string) (*Decision, error) {
	var d Decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("路由决策解析失败: %w", err)
	}

	if d.Confidence < 0.4 {
		return nil, fmt.Errorf("置信度过低: %.2f，决策: %+v", d.Confidence, d)
	}

	// 验证 Recipient 合法（工具注册中心 + 内核插件表）
	if r.checkRecipient != nil {
		if !r.checkRecipient(d.Recipient) {
			return nil, fmt.Errorf("无效收件人: %s", d.Recipient)
		}
	} else if _, ok := tools.GetToolSchema(d.Recipient); !ok {
		return nil, fmt.Errorf("无效收件人: %s", d.Recipient)
	}

	log.Printf("[Router] 路由决策: %s (%.2f) — %s", d.Recipient, d.Confidence, d.Reason)
	return &d, nil
}
