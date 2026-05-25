package llm

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

/* ─── 声明式多 Provider：从 JSON 配置文件加载额外 Provider ───

   核心 4 个 Provider（deepseek / xiaomi / openai / local）仍然是硬编码的。
   配置文件中的 Provider 合并进同一个 providers map，共享同一套路由逻辑。

   硬化校验：
   - endpoint 必须 HTTPS 或 localhost（防止中间人）
   - type 必须在白名单内
   - name 不能与硬编码 Provider 冲突
   - 校验失败时仅日志警告，不阻塞启动（回退到默认 Provider）

   配置文件格式（LLM_PROVIDERS_CONFIG 环境变量指定路径）：
   {
     "providers": [
       {
         "name": "ollama",
         "endpoint": "http://localhost:11434/v1",
         "model": "qwen3:8b",
         "type": "openai-compatible",
         "api_key_env": "OLLAMA_API_KEY"
       }
     ]
   }
*/

// allowedTypes 是 Provider type 字段的白名单。
var allowedTypes = map[string]bool{
	"openai-compatible": true, // OpenAI 兼容 API
	"deepseek":          true, // DeepSeek 兼容
	"local":             true, // 本地模型
}

// defaultRouterPrompt 是自定义 Provider 使用的通用 Router prompt。
// 它不包含 DeepSeek 特有的路由规则，只做最基础的插件路由。
// 自定义 Provider 的路由决策质量可能低于原生 DeepSeek。
const defaultRouterPrompt = `Output JSON: {"recipient":"","msg_type":"","payload":"","reason":"","confidence":0.0}
Recipient is the plugin to handle this request. Available plugins:
%s
Rules:
- chat/greetings → think_plugin:chat
- web search / look up → search_plugin:web_search
- knowledge search / memory → memory_plugin:knowledge_search
- tool usage / command → terminal_plugin:terminal_exec
- file operations → write_plugin:write_file or read_file
- Default → think_plugin:chat
Output ONLY valid JSON, no markdown. Input: %s`

// ExtraProviderConfig 是配置文件中一个自定义 Provider 的声明。
type ExtraProviderConfig struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	Type      string `json:"type"`
	APIKeyEnv string `json:"api_key_env"` // 该 Provider 的 API key 对应的环境变量名
}

// extraProvidersFile 是配置文件的顶层结构。
type extraProvidersFile struct {
	Providers []ExtraProviderConfig `json:"providers"`
}

// extraProviderAPIKeys 记录自定义 Provider 的 API key 环境变量映射。
var extraProviderAPIKeys map[string]string

// LoadProviderConfig 加载自定义 Provider 配置文件并合并到全局 providers map。
//
// 硬化校验：
//   - endpoint 必须 HTTPS 或 localhost
//   - type 必须在 allowedTypes 白名单内
//   - name 不能与硬编码 Provider 冲突
//
// 校验失败时仅日志警告，不阻塞启动。
func LoadProviderConfig(path string) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 Provider 配置文件失败: %w", err)
	}

	var file extraProvidersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("解析 Provider 配置文件失败: %w", err)
	}

	if extraProviderAPIKeys == nil {
		extraProviderAPIKeys = make(map[string]string)
	}

	var errs []string
	for _, cfg := range file.Providers {
		if err := validateExtraProvider(cfg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", cfg.Name, err))
			continue
		}

		// 修复前缀：如果 endpoint 不以 /v1 结尾，加上
		baseURL := strings.TrimRight(cfg.Endpoint, "/")
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL += "/v1"
		}

		providers[cfg.Name] = Provider{
			Name:         cfg.Name,
			BaseURL:      baseURL,
			Model:        cfg.Model,
			RouterPrompt: defaultRouterPrompt,
		}
		if cfg.APIKeyEnv != "" {
			extraProviderAPIKeys[cfg.Name] = cfg.APIKeyEnv
		}

		fmt.Printf("[llm] 加载自定义 Provider: %s (%s, %s)\n", cfg.Name, cfg.Type, cfg.Model)
	}

	if len(errs) > 0 {
		return fmt.Errorf("部分 Provider 校验失败:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// validateExtraProvider 对单个自定义 Provider 配置做硬化校验。
func validateExtraProvider(cfg ExtraProviderConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	if _, exists := builtinProviders[cfg.Name]; exists {
		return fmt.Errorf("name %q 与硬编码 Provider 冲突", cfg.Name)
	}
	if cfg.Endpoint == "" {
		return fmt.Errorf("endpoint 不能为空")
	}
	if !isValidEndpoint(cfg.Endpoint) {
		return fmt.Errorf("endpoint 不安全: %s（需要 HTTPS 或 localhost）", cfg.Endpoint)
	}
	if cfg.Model == "" {
		return fmt.Errorf("model 不能为空")
	}
	if cfg.Type != "" && !allowedTypes[cfg.Type] {
		return fmt.Errorf("不支持的 type: %s（允许: openai-compatible, deepseek, local）", cfg.Type)
	}
	return nil
}

// isValidEndpoint 硬化校验：端点必须 HTTPS 或 localhost。
// 本地开发场景允许 HTTP（localhost/127.0.0.1）。
func isValidEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme == "http" {
		host := strings.ToLower(u.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return false
}

// builtinProviders 是硬编码 Provider 的名字集合，用于冲突检测。
var builtinProviders = map[string]bool{
	"deepseek": true,
	"xiaomi":   true,
	"openai":   true,
	"local":    true,
}
