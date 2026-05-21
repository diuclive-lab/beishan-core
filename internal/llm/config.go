package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ChatCompletion 共享 LLM Chat API 调用。
// 返回响应文本，不注入任何额外上下文（纯文本补全）。
// think_plugin（主回答）和 findSemanticLinks（写入时语义建链）共用此函数。
func ChatCompletion(system, user string, timeout time.Duration) (string, error) {
	apiKey := APIKeyFor(ProviderName())
	if apiKey == "" {
		return "", fmt.Errorf("LLM_API_KEY 未设置")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": Model(),
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})

	req, err := http.NewRequest("POST", ChatEndpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
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
		return "", fmt.Errorf("LLM 未返回结果")
	}
	return result.Choices[0].Message.Content, nil
}

type Provider struct {
	Name         string
	BaseURL      string
	Model        string
	RouterPrompt string
}

var providers = map[string]Provider{
	"deepseek": {
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
		RouterPrompt: `Output JSON: {"recipient":"","msg_type":"","payload":{},"reason":"","confidence":0.0}
Recipient is the plugin to handle this request. msg_type is the message type the plugin expects.
IMPORTANT: payload must be a JSON object, not a string.
Available plugins:
%s

Routing rules with payload formats:
- "搜索知识库"/"查知识"/"我的笔记" → recipient:memory_plugin, msg_type:knowledge_search, payload:{"keyword":"user input"}
- "搜索"/"搜一下"/"查找资料" (web) → recipient:search_plugin, msg_type:web_search, payload:{"query":"user input"}
- "列出知识"/"知识列表" → recipient:memory_plugin, msg_type:knowledge_list, payload:{}
- "添加知识"/"记录一下" → recipient:memory_plugin, msg_type:knowledge_add, payload:{"title":"...","summary":"..."}
- workflow → recipient:workflow_plugin, msg_type:workflow_run, payload:{"workflow":"name"}
- ALL other queries (including questions about past discussions, decisions, code, etc.) → recipient:think_plugin, msg_type:chat, payload:{}
- think_plugin handles its own retrieval (knowledge + code + session history). Do NOT route conversational queries to memory_plugin.
- ONLY output the JSON, no markdown, no explanations

Input: %s`,
	},
	"xiaomi": {
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
			"- workflow_plugin: workflow_run (payload:{\"workflow\":\"<name>\"})\n" +
			"\nRules:\n" +
			"- chat/greetings -> recipient think_plugin, msg_type chat, payload {}\n" +
			"- 知识库搜索 -> recipient memory_plugin, msg_type knowledge_search, payload keyword:\"user input\"\n" +
			"- web search -> recipient search_plugin, msg_type web_search, payload query:\"user input\"\n" +
			"- workflow -> recipient workflow_plugin, msg_type workflow_run, payload workflow:\"name\"\n" +
			"- ALWAYS include payload as JSON object matching the msg_type format\n" +
			"- ONLY output the JSON, no markdown, no explanations\n" +
			"\nUser input:\n%s",
	},
	"openai": {
		Name:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o",
		RouterPrompt: `Output JSON: {"recipient":"","msg_type":""}
Available plugins:
%s
Input: %s`,
	},
	"local": {
		Name:    "local",
		BaseURL: "http://127.0.0.1:8090/v1",
		Model:   "qwen3.6-27B",
		RouterPrompt: `Output JSON: {"recipient":"","msg_type":"","payload":"","reason":"","confidence":0.0}
Recipient is the plugin to handle this request. msg_type is the message type the plugin expects.
When routing to workflow_plugin, set msg_type to "workflow_run" and payload to {"workflow":"<name>"}
Available plugins:
%sInput: %s`,
	},
}

func activeProvider() Provider {
	name := os.Getenv("LLM_PROVIDER")
	if p, ok := providers[name]; ok {
		return p
	}
	return providers["deepseek"]
}

func ProviderName() string {
	return activeProvider().Name
}

func APIKey() string {
	if k := os.Getenv("LLM_API_KEY"); k != "" {
		return k
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
