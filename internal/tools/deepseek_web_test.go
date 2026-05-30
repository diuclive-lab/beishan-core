package tools

import (
	"os"
	"strings"
	"testing"
)

// TestDeepseekWebSearchLive 端到端实跑：用 beishan 自有浏览器在登录态 DeepSeek 网页版搜索，
// 应返回真实结果（+ 已思考）。未登录则 Skip（提示先跑 deepseek-login）。
// 仅 BEISHAN_DEEPSEEK_TEST=1 时跑（起 Chrome + 联网，默认不在 hermetic 门禁里）。
func TestDeepseekWebSearchLive(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（起 Chrome + 联网）")
	}
	if findChrome() == "" {
		t.Skip("未找到 Chrome")
	}
	out := deepseekWebSearch("2026年 AI agent 框架 趋势", 4)
	t.Logf("success=%v results=%d thinkingBytes=%d error=%q",
		out.Success, len(out.Results), len(out.Thinking), out.Error)
	if strings.Contains(out.Error, "登录") {
		t.Skip("需先 go run ./cmd/deepseek-login 登录一次")
	}
	if !out.Success || len(out.Results) == 0 {
		t.Fatalf("登录态下应搜到结果，实得 error=%q", out.Error)
	}
}

// TestExtractMarkedJSON 纯函数单测（hermetic）：标记 JSON 抽取。
func TestExtractMarkedJSON(t *testing.T) {
	text := "一些思考过程……\n<BEISHAN_JSON>{\"query\":\"x\",\"results\":[{\"title\":\"T\",\"url\":\"https://a.com\",\"snippet\":\"s\"}]}</BEISHAN_JSON>尾巴"
	parsed, ok := extractMarkedJSON(text)
	if !ok {
		t.Fatal("应抽到 JSON")
	}
	hits := normalizeDeepseekResults(parsed, 5)
	if len(hits) != 1 || hits[0].URL != "https://a.com" {
		t.Fatalf("结果解析不对: %+v", hits)
	}
	// 无标记 → 抽不到
	if _, ok := extractMarkedJSON("no markers here"); ok {
		t.Fatal("无标记不应抽到")
	}
	// 非公开链接（deepseek 自身）被过滤
	bad := "<BEISHAN_JSON>{\"results\":[{\"title\":\"T\",\"url\":\"relative/path\",\"snippet\":\"s\"}]}</BEISHAN_JSON>"
	p2, _ := extractMarkedJSON(bad)
	if len(normalizeDeepseekResults(p2, 5)) != 0 {
		t.Fatal("非 http(s) 链接应被过滤")
	}
}

// TestDeepseekLoginState 纯函数单测：登录态判定。
func TestDeepseekLoginState(t *testing.T) {
	if deepseekLoginState("https://chat.deepseek.com/sign_in", "请扫码登录") != "sign_in_required" {
		t.Fatal("应判为需登录")
	}
	if deepseekLoginState("https://chat.deepseek.com/", "联网搜索 深度思考") != "ready_or_logged_in" {
		t.Fatal("应判为已登录/就绪")
	}
}
