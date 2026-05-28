package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// ChatCompletion 共享 LLM Chat API 调用。
// 返回响应文本，不注入任何额外上下文（纯文本补全）。
// think_plugin（主回答）和 findSemanticLinks（写入时语义建链）共用此函数。
func ChatCompletion(system, user string, timeout time.Duration) (string, error) {
	return ChatCompletionMulti([]ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, timeout)
}

// ChatMessage 表示多轮对话中的一条消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage 记录单次 LLM 调用的 token 消耗。
type Usage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Model            string `json:"model"`
}

// ChatCompletionMulti 多轮对话 LLM 调用。
// 接受完整 messages 数组（含 system + 历史 + 当前用户消息）。
func ChatCompletionMulti(messages []ChatMessage, timeout time.Duration) (string, error) {
	reply, _, err := ChatCompletionWithUsage(messages, timeout)
	return reply, err
}

// ChatCompletionWithUsage 多轮对话 LLM 调用，返回 token 使用量。
func ChatCompletionWithUsage(messages []ChatMessage, timeout time.Duration) (string, *Usage, error) {
	apiKey := APIKeyFor(ProviderName())
	if apiKey == "" {
		return "", nil, fmt.Errorf("LLM_API_KEY 未设置")
	}

	model := Model()
	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	if ProviderName() == "local" {
		reqBody["max_tokens"] = 4096
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", ChatEndpoint(), bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("API 调用失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
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
		return "", nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("LLM 未返回结果")
	}
	if result.Choices[0].FinishReason == "length" {
		log.Printf("[llm] 响应被 max_tokens 截断")
	}

	usage := &Usage{
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
		Model:            model,
	}
	// 某些 API 不返回 usage 字段，用 0 标记
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return result.Choices[0].Message.Content, usage, nil
}

type Provider struct {
	Name         string
	BaseURL      string
	Model        string
	RouterPrompt string
}

var (
	overrideProvider string
	providerMu       sync.RWMutex
)

// SetProvider overrides the active LLM provider. Use "" to revert to LLM_PROVIDER env.
func SetProvider(name string) {
	providerMu.Lock()
	defer providerMu.Unlock()
	overrideProvider = name
}

// FailoverProvider switches to the local provider for fallback.
// "failover" is not a separate provider entry; it maps directly to "local".
func FailoverProvider() { SetProvider("local") }

// canonicalRouterRules 是所有 OpenAI 兼容 provider 共享的路由规则集。
// 基于 DeepSeek 调优后验证的版本，更新此处对全部 provider 生效。
// 占位符：%s = 插件列表，%s = 输入消息。
const canonicalRouterRules = `Output JSON: {"recipient":"","msg_type":"","payload":{},"reason":"","confidence":0.0}
Recipient is the plugin to handle this request. msg_type is the message type the plugin expects.
IMPORTANT: payload must be a JSON object, not a string.
Available plugins:
%s

Routing rules with payload formats:
- "搜索知识库"/"查知识"/"我的笔记" → recipient:memory_plugin, msg_type:knowledge_search, payload:{"keyword":"user input"}
- "对比"/"区别"/"差异"/"不同" (comparison) → think_plugin:chat
- "搜索"/"搜一下"/"查找资料" (web) → recipient:search_plugin, msg_type:web_search, payload:{"query":"user input"}
- "列出知识"/"知识列表" → recipient:memory_plugin, msg_type:knowledge_list, payload:{}
- "添加知识"/"记录一下" → recipient:memory_plugin, msg_type:knowledge_add, payload:{"title":"...","summary":"..."}
- "创建工作流"/"新建工作流"/"生成工作流" → recipient:skill_factory_plugin, msg_type:skill_create, payload:{}
- "记住"/"记录" (memory requests like "记住我叫X") → recipient:think_plugin, msg_type:chat, payload:{}
- workflow (execute existing workflow) → recipient:workflow_plugin, msg_type:workflow_run, payload:{"workflow":"name"}
- "看桌面"/"桌面操作"/"帮我看看"/"操作电脑" (desktop/view/click) → recipient:memory_plugin, msg_type:desktop_actuator, payload:{"action":"get_window_tree"}
- ALL other queries (including questions about past discussions, decisions, code, etc.) → recipient:think_plugin, msg_type:chat, payload:{}
- think_plugin handles its own retrieval (knowledge + code + session history). Do NOT route conversational queries to memory_plugin.
- ONLY output the JSON, no markdown, no explanations

Input: %s`

var providers = map[string]Provider{
	// deepseek、openai、local 共用 canonicalRouterRules，保持路由规则同步。
	// 切换 provider 不需要单独维护 prompt，降低后续模型迁移成本。
	"deepseek": {
		Name:         "deepseek",
		BaseURL:      "https://api.deepseek.com/v1",
		Model:        "deepseek-chat",
		RouterPrompt: canonicalRouterRules,
	},
	"xiaomi": {
		// xiaomi 保留独立 prompt：该模型对中英混排规则集反应较好。
		Name:    "xiaomi",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
		RouterPrompt: "You are a router for beishan-core system. Output ONLY valid JSON.\n" +
			`{"recipient":"search_plugin","msg_type":"web_search","payload":{"query":"user input"},"confidence":0.9}` + "\n" +
			"Available plugins with payload formats:\n" +
			"- search_plugin: web_search (payload:{\"query\":\"...\"}), web_fetch (payload:{\"url\":\"...\"})\n" +
			"- write_plugin: write_file, read_file, file_parse\n" +
			"- memory_plugin: knowledge_search (payload:{\"keyword\":\"...\"}), knowledge_list (payload:{}), knowledge_add (payload:{\"title\":\"...\",\"summary\":\"...\"})\n" +
			"- terminal_plugin: terminal_exec\n" +
			"- todo_plugin: todo_add, todo_list, todo_done\n" +
			"- think_plugin: chat (payload:{})\n" +
			"- skill_factory_plugin: skill_create (payload:{})\n" +
			"- workflow_plugin: workflow_run (payload:{\"workflow\":\"<name>\"})\n" +
			"\nRules:\n" +
			"- chat/greetings -> recipient think_plugin, msg_type chat, payload {}\n" +
			"- 知识库搜索 -> recipient memory_plugin, msg_type knowledge_search, payload keyword:\"user input\"\n" +
			"- web search -> recipient search_plugin, msg_type web_search, payload query:\"user input\"\n" +
			"- 创建工作流/新建工作流/生成工作流 -> recipient skill_factory_plugin, msg_type skill_create, payload {}\n" +
			"- 记住/记录 (memory requests like \"记住我叫X\") -> recipient think_plugin, msg_type chat, payload {}\n" +
			"- workflow -> recipient workflow_plugin, msg_type workflow_run, payload workflow:\"name\"\n" +
			"- ALWAYS include payload as JSON object matching the msg_type format\n" +
			"- ONLY output the JSON, no markdown, no explanations\n" +
			"\nUser input:\n%s",
	},
	"openai": {
		Name:         "openai",
		BaseURL:      "https://api.openai.com/v1",
		Model:        "gpt-4o",
		RouterPrompt: canonicalRouterRules,
	},
	"local": {
		// local 也使用 canonicalRouterRules；LocalRouteStrategy.callLocal() 额外
		// 注入系统消息约束格式，两者叠加保证本地模型输出合法 JSON。
		Name:         "local",
		BaseURL:      "http://127.0.0.1:8090/v1",
		Model:        "gemma-4-E4B-it-Q4_K_M.gguf",
		RouterPrompt: canonicalRouterRules,
	},
}

// init 在包初始化时加载自定义 Provider 配置文件。
// 配置路径通过 LLM_PROVIDERS_CONFIG 环境变量指定。
// 加载失败不阻塞启动，回退到硬编码默认 Provider。
func init() {
	path := os.Getenv("LLM_PROVIDERS_CONFIG")
	if path == "" {
		return
	}
	if err := LoadProviderConfig(path); err != nil {
		log.Printf("[llm] 自定义 Provider 加载警告: %v（继续使用默认 Provider）", err)
	}
}

func activeProvider() Provider {
	providerMu.RLock()
	override := overrideProvider
	providerMu.RUnlock()
	if override != "" {
		if p, ok := providers[override]; ok {
			return p
		}
	}
	name := os.Getenv("LLM_PROVIDER")
	if p, ok := providers[name]; ok {
		return p
	}
	return providers["deepseek"]
}

func ProviderName() string {
	return activeProvider().Name
}

// APIKey returns the API key for the currently active provider.
// For the local provider it returns LOCAL_API_KEY (or "local-dev"),
// so callers don't need to know which provider is active.
func APIKey() string {
	if k := os.Getenv("LLM_API_KEY"); k != "" {
		return k
	}
	name := ProviderName()
	if name == "local" || name == "failover" {
		if k := os.Getenv("LOCAL_API_KEY"); k != "" {
			return k
		}
		return "local-dev"
	}
	return os.Getenv("DEEPSEEK_API_KEY")
}

func BaseURL() string {
	if u := os.Getenv("LLM_BASE_URL"); u != "" {
		return u
	}
	return activeProvider().BaseURL
}

func Model() string {
	if m := os.Getenv("LLM_MODEL"); m != "" {
		return m
	}
	return activeProvider().Model
}

func ChatEndpoint() string {
	return BaseURL() + "/chat/completions"
}

func RouterPrompt() string {
	return activeProvider().RouterPrompt
}

/* ─── Provider Override: 按名称获取配置，用于 workflow 指定模型 ─── */

func providerByName(name string) (Provider, bool) {
	p, ok := providers[name]
	return p, ok
}

func ChatEndpointFor(provider string) string {
	if p, ok := providerByName(provider); ok {
		if u := os.Getenv("LLM_BASE_URL"); u != "" && provider == activeProvider().Name {
			return u + "/chat/completions"
		}
		return p.BaseURL + "/chat/completions"
	}
	return ChatEndpoint()
}

func APIKeyFor(provider string) string {
	// 自定义 Provider 可能有独立的环境变量
	if extraProviderAPIKeys != nil {
		if env, ok := extraProviderAPIKeys[provider]; ok && env != "" {
			if k := os.Getenv(env); k != "" {
				return k
			}
		}
	}
	// local provider 默认使用 local-dev key（llama-server --api-key）
	if provider == "local" {
		if k := os.Getenv("LOCAL_API_KEY"); k != "" {
			return k
		}
		return "local-dev"
	}
	return APIKey()
}

func ModelFor(provider string) string {
	if p, ok := providerByName(provider); ok {
		return p.Model
	}
	return Model()
}

/* ─── ChatCompletionWithProvider: 指定 provider 的 LLM 调用 ───
   用于 workflow per-step provider override，如 DeepSeek 做路由、Qwen3.6 做苦力。 */

func ChatCompletionWithProvider(provider string, messages []ChatMessage, timeout time.Duration) (string, *Usage, error) {
	apiKey := APIKeyFor(provider)
	if apiKey == "" {
		return "", nil, fmt.Errorf("LLM_API_KEY 未设置 (provider=%s)", provider)
	}

	model := ModelFor(provider)
	endpoint := ChatEndpointFor(provider)

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	// 本地模型需要显式指定 max_tokens，否则 llama-server 默认值太小
	if provider == "local" {
		reqBody["max_tokens"] = 4096
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("API 调用失败 (provider=%s): %w", provider, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
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
		return "", nil, fmt.Errorf("解析响应失败 (provider=%s): %w", provider, err)
	}
	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("LLM 未返回结果 (provider=%s)", provider)
	}

	usage := &Usage{
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
		Model:            model,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return result.Choices[0].Message.Content, usage, nil
}
