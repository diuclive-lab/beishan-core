package llm

import "os"

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
		RouterPrompt: "You are a router for beishan-core system. Output ONLY valid JSON.\n" +
			`{"recipient":"search_plugin","msg_type":"web_search","payload":{"query":"user input"},"confidence":0.9}` + "\n" +
			"Available plugins with payload formats:\n" +
			"- search_plugin: web_search (payload:{\"query\":\"...\"}), web_fetch (payload:{\"url\":\"...\"})\n" +
			"- write_plugin: write_file, read_file, file_parse\n" +
			"- memory_plugin: knowledge_add, knowledge_search, knowledge_list\n" +
			"- terminal_plugin: terminal_exec\n" +
			"- todo_plugin: todo_add, todo_list, todo_done\n" +
			"- think_plugin: chat (no payload needed)\n" +
			"- workflow_plugin: workflow_run (payload:{\"workflow\":\"<name>\"})\n" +
			"\nRules:\n" +
			"- chat/greetings -> recipient think_plugin, msg_type chat\n" +
			"- search -> recipient search_plugin, msg_type web_search, payload query:\"user input\"\n" +
			"- workflow -> recipient workflow_plugin, msg_type workflow_run, payload workflow:\"name\"\n" +
			"- ALWAYS include payload field matching the msg_type format\n" +
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
