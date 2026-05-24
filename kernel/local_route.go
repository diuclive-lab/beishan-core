package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"beishan/internal/llm"
)

// LocalRouteStrategy uses a local LLM for routing decisions when API is unreachable.
// Reuses the Router's plugin list and parseDecision for hardening consistency.
type LocalRouteStrategy struct {
	router   *Router
	endpoint string // e.g. "http://127.0.0.1:8080"
	model    string // e.g. "qwen3.6-27B"
	apiKey   string
}

// NewLocalRouteStrategy creates a strategy that talks to a local model for routing.
func NewLocalRouteStrategy(router *Router, endpoint, model string) *LocalRouteStrategy {
	apiKey := llm.APIKeyFor("local")
	return &LocalRouteStrategy{
		router:   router,
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
	}
}

// Route sends the routing prompt to the local model, then validates via parseDecision.
func (s *LocalRouteStrategy) Route(msg Message) (*Decision, error) {
	pluginList := s.router.buildPluginList()

	promptTmpl := llm.RouterPrompt()
	prompt := fmt.Sprintf(promptTmpl,
		pluginList,
		msg.Type+": "+string(msg.Payload),
	)

	resp, err := s.callLocal(prompt)
	if err != nil {
		return nil, fmt.Errorf("本地路由调用失败: %w", err)
	}

	return s.router.parseDecision(resp)
}

func (s *LocalRouteStrategy) callLocal(prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":    s.model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})

	req, err := http.NewRequest("POST", s.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("本地模型不可达: %w", err)
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
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("本地模型未返回结果")
	}
	return result.Choices[0].Message.Content, nil
}
