package tools

import (
	"os"
	"strings"
	"testing"

	"beishan/internal/browser"
)

// TestCDPPipeTransport 端到端验证 CDP-over-pipe 传输：beishan 启动并拥有一个 headless
// Chrome，通过 Engine/Page 接口跑 JS 取回结果。无 Chrome 时 Skip。
func TestCDPPipeTransport(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome，非 hermetic，默认不在门禁里跑）")
	}
	chrome := browser.FindChromePath()
	if chrome == "" {
		t.Skip("未找到 Chrome，跳过 CDP 传输测试")
	}
	dir, err := os.MkdirTemp("", "beishan-cdp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	eng, err := browser.NewChrome(dir, true)
	if err != nil {
		t.Fatalf("启动 Chrome 引擎失败: %v", err)
	}
	defer eng.Close()

	page, err := eng.NewPage("about:blank")
	if err != nil {
		t.Fatalf("NewPage 失败: %v", err)
	}
	defer page.Close()

	ua, err := page.Eval("navigator.userAgent")
	if err != nil {
		t.Fatalf("Eval 失败: %v", err)
	}
	if !strings.Contains(ua, "Chrome") && !strings.Contains(ua, "HeadlessChrome") {
		t.Fatalf("userAgent 不像 Chrome: %q", ua)
	}
	t.Logf("CDP-over-pipe 通了，UA=%s", ua)
}
