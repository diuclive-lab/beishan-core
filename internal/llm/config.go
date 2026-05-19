package llm

import "os"

type Provider struct {
	Name       string
	BaseURL    string
	Model      string
	RouterPrompt string // 路由 prompt 模板，%s = pluginList, %s = input
}

var providers = map[string]Provider{
	"deepseek": {
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
		RouterPrompt: `Output JSON: {"recipient":"","msg_type":"","payload":"","reason":"","confidence":0.0}
Recipient is the plugin to handle this request. msg_type is the message type the plugin expects.
When routing to workflow_plugin, set msg_type to "workflow_run" and payload to {"workflow":"<name>"}
Available plugins:
%sInput: %s`,
	},
	"xiaomi": {
		Name:    "xiaomi",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
		RouterPrompt: `You are a router for beishan-core system. Analyze the user input and output ONLY valid JSON:

{"recipient":"search_plugin","msg_type":"web_search","reason":"User wants to search","confidence":0.9}

Available plugins with their types:
- search_plugin: web_search, web_fetch
- write_plugin: write_file, read_file, file_parse
- memory_plugin: knowledge_add, knowledge_search, knowledge_list
- terminal_plugin: terminal_exec
- todo_plugin: todo_add, todo_list, todo_done
- think_plugin: chat
- workflow_plugin: workflow_run

Rules:
- For chat, questions, greetings → {recipient: "think_plugin", msg_type: "chat"}
- For workflow execution → {recipient: "workflow_plugin", msg_type: "workflow_run", payload: {"workflow":"name"}}
- reply ONLY the JSON object, no markdown, no explanations

User input:
%s`,
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
}

func activeProvider() Provider {
	name := os.Getenv("LLM_PROVIDER")
	if p, ok := providers[name]; ok {
		return p
	}
	return providers["deepseek"] // 默认 DeepSeek
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
