package tools

import (
	"os"
	"strings"
	"testing"
)

// TestIsSearchEngineURL 证明「浏览器抓搜索引擎」会被识别拦截（应改用 web_search），
// 而搜索引擎域名下的非搜索页（如 google docs）不误伤。纯函数、无网络。
func TestIsSearchEngineURL(t *testing.T) {
	block := []string{
		"https://www.google.com/search?q=foo",
		"https://www.bing.com/search?q=foo",
		"https://html.duckduckgo.com/html/?q=foo",
		"https://duckduckgo.com/?q=foo",
		"https://search.brave.com/search?q=foo",
	}
	for _, u := range block {
		if !isSearchEngineURL(u) {
			t.Errorf("应判定为搜索引擎搜索 URL（拦截）: %s", u)
		}
	}
	allow := []string{
		"https://example.com/article/123",
		"https://docs.google.com/document/d/abc", // google 域但非搜索
		"https://github.com/foo/bar",
		"https://mp.weixin.qq.com/s/xyz",
	}
	for _, u := range allow {
		if isSearchEngineURL(u) {
			t.Errorf("不应拦（非搜索页）: %s", u)
		}
	}
}

// TestLooksLikeAntiBot 证明反爬/挑战页被识别，且正常文本（含 "captcha" 单词的科普）不误伤。
func TestLooksLikeAntiBot(t *testing.T) {
	bot := []string{
		"Please verify you are human to continue",
		"Our systems have detected unusual traffic from your network",
		"需要完成人机验证",
		"enable javascript to continue",
	}
	for _, s := range bot {
		if !looksLikeAntiBot(s) {
			t.Errorf("应判定为反爬页: %q", s)
		}
	}
	normal := []string{
		"This article discusses how CAPTCHA systems work in general.", // 含 captcha 单词但不收，避免误伤
		"Welcome to the documentation. Here is the API reference.",
		"北山核心是一个 AI 智能体框架。",
	}
	for _, s := range normal {
		if looksLikeAntiBot(s) {
			t.Errorf("正常文本不应判定为反爬: %q", s)
		}
	}
}

// TestBrowserNavigateBlocksPrivateIP 证明 browser_navigate 现在有 SSRF 防护（此前漏了）。
// 私有 IP 在 isSafeURL 里走前缀短路、不做 DNS，故本测试 hermetic。
func TestBrowserNavigateBlocksPrivateIP(t *testing.T) {
	res := browserNavigateHandler(map[string]interface{}{"url": "http://10.1.2.3/admin"})
	if res.Success {
		t.Fatal("browser_navigate 应拦截私有 IP（SSRF），实际放行了")
	}
	if !strings.Contains(res.Output, "私有") && !strings.Contains(res.Output, "blocked") {
		t.Fatalf("应是 SSRF 拦截错误，实得: %s", res.Output)
	}
}

// TestWebSearchIntegration 端到端验证 web_search→Tavily 真能搜到（有 key 才跑）。
// 无 TAVILY_API_KEY 时 t.Skip——故 verify.sh（去密钥的 hermetic 门禁）会跳过，不联网。
func TestWebSearchIntegration(t *testing.T) {
	if os.Getenv("TAVILY_API_KEY") == "" {
		t.Skip("无 TAVILY_API_KEY，跳过 web_search 集成测试（hermetic 安全）")
	}
	res := webSearchHandler(map[string]interface{}{"query": "golang programming language", "limit": float64(3)})
	if !res.Success {
		t.Fatalf("web_search 应成功，实得 error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "\"url\"") {
		n := len(res.Output)
		if n > 200 {
			n = 200
		}
		t.Fatalf("结果应含 url 字段，实得: %s", res.Output[:n])
	}
}

