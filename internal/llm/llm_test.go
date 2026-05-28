package llm

import (
	"os"
	"testing"
)

func TestRouterPrompt(t *testing.T) {
	p := RouterPrompt()
	if len(p) == 0 {
		t.Fatal("expected non-empty prompt")
	}
}

func TestModelDefault(t *testing.T) {
	m := Model()
	if m == "" {
		t.Fatal("expected non-empty model")
	}
}

func TestProviderName(t *testing.T) {
	n := ProviderName()
	if n == "" {
		t.Fatal("expected non-empty provider")
	}
}

func TestLoadProviderConfig(t *testing.T) {
	content := `{
		"providers": [
			{
				"name": "test-llama",
				"endpoint": "http://localhost:11434/v1",
				"model": "llama3:8b",
				"type": "openai-compatible",
				"api_key_env": "TEST_LLAMA_KEY"
			}
		]
	}`
	tmpFile := t.TempDir() + "/providers.json"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("TEST_LLAMA_KEY", "test-key-value")
	defer os.Unsetenv("TEST_LLAMA_KEY")

	if err := LoadProviderConfig(tmpFile); err != nil {
		t.Fatalf("LoadProviderConfig failed: %v", err)
	}

	p, ok := providers["test-llama"]
	if !ok {
		t.Fatal("test-llama provider not found in map")
	}
	if p.Model != "llama3:8b" {
		t.Fatalf("expected model llama3:8b, got %s", p.Model)
	}

	key := APIKeyFor("test-llama")
	if key != "test-key-value" {
		t.Fatalf("expected test-key-value, got %s", key)
	}
}

func TestLoadProviderConfig_RejectsUnsafeEndpoint(t *testing.T) {
	content := `{
		"providers": [
			{
				"name": "evil",
				"endpoint": "http://evil.com/v1",
				"model": "gpt-5",
				"type": "openai-compatible"
			}
		]
	}`
	tmpFile := t.TempDir() + "/bad_providers.json"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadProviderConfig(tmpFile)
	if err == nil {
		t.Fatal("expected error for unsafe endpoint")
	}
}

func TestLoadProviderConfig_RejectsBuiltinName(t *testing.T) {
	content := `{
		"providers": [
			{
				"name": "deepseek",
				"endpoint": "https://custom.com/v1",
				"model": "deepseek-chat",
				"type": "openai-compatible"
			}
		]
	}`
	tmpFile := t.TempDir() + "/conflict_providers.json"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadProviderConfig(tmpFile)
	if err == nil {
		t.Fatal("expected error for conflicting builtin name")
	}
}

func TestAPIKeyFor_FallsBack(t *testing.T) {
	os.Setenv("LLM_API_KEY", "global-key")
	defer os.Unsetenv("LLM_API_KEY")

	key := APIKeyFor("unknown-provider")
	if key != "global-key" {
		t.Fatalf("expected global-key, got %s", key)
	}
}

func TestAPIKey_LocalProvider(t *testing.T) {
	// 切换到 local provider 时，APIKey() 应返回 "local-dev"（或 LOCAL_API_KEY），
	// 而不是 DEEPSEEK_API_KEY。这保证 kernel/router.go 的 callDeepSeek
	// 在 LLM_PROVIDER=local 时能用正确的 key 访问本地端点。
	os.Unsetenv("LLM_API_KEY")
	os.Unsetenv("LOCAL_API_KEY")
	os.Setenv("LLM_PROVIDER", "local")
	defer os.Unsetenv("LLM_PROVIDER")

	key := APIKey()
	if key != "local-dev" {
		t.Fatalf("expected local-dev for local provider, got %q", key)
	}

	// 自定义 LOCAL_API_KEY 优先
	os.Setenv("LOCAL_API_KEY", "my-local-key")
	defer os.Unsetenv("LOCAL_API_KEY")

	key2 := APIKey()
	if key2 != "my-local-key" {
		t.Fatalf("expected my-local-key when LOCAL_API_KEY set, got %q", key2)
	}
}

func TestIsValidEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     bool
	}{
		{"https://api.openai.com/v1", true},
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:8080", true},
		{"http://[::1]:11434", true},
		{"http://evil.com/v1", false},
		{"http://192.168.1.1:8080", false},
		{"ftp://bad.com", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isValidEndpoint(tt.endpoint)
		if got != tt.want {
			t.Errorf("isValidEndpoint(%q) = %v, want %v", tt.endpoint, got, tt.want)
		}
	}
}
