package llm

import "os"

const (
	DefaultBaseURL = "https://api.deepseek.com/v1"
	DefaultModel   = "deepseek-chat"
)

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
	return DefaultBaseURL
}

func Model() string {
	if m := os.Getenv("LLM_MODEL"); m != "" {
		return m
	}
	return DefaultModel
}

func ChatEndpoint() string {
	return BaseURL() + "/chat/completions"
}
