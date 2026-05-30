package tools

import (
	"os"
	"testing"
)

// TestDeepseekLoginDetection 实跑：headless 打开 chat.deepseek.com（profile 未登录），
// 应快速返回「需要一次性登录」而非挂起/崩溃。仅在 BEISHAN_DEEPSEEK_TEST=1 时跑
// （要起 Chrome + 联网，默认不在门禁里跑）。
func TestDeepseekLoginDetection(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（起 Chrome + 联网）")
	}
	if findChrome() == "" {
		t.Skip("未找到 Chrome")
	}
	out := deepseekWebSearch("beishan core test query", 3)
	t.Logf("success=%v backend=%s error=%q results=%d thinkinglen=%d",
		out.Success, out.Backend, out.Error, len(out.Results), len(out.Thinking))
	// 未登录时不应 Success；且必须有明确 Error（不能静默/挂起）
	if out.Success {
		t.Log("注意：竟然 success 了（profile 可能已登录）")
	}
	if !out.Success && out.Error == "" {
		t.Fatal("未成功却无 Error——不可接受（静默失败）")
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
