package tools

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestCDPPipeTransport 端到端验证 CDP-over-pipe 传输：beishan 启动并拥有一个 headless
// Chrome，attach 一个页面，跑 JS 取回结果。无 Chrome 时 Skip（故 verify.sh 会跳过、不联网/不起进程）。
func TestCDPPipeTransport(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome，非 hermetic，默认不在门禁里跑）")
	}
	chrome := findChrome()
	if chrome == "" {
		t.Skip("未找到 Chrome，跳过 CDP 传输测试")
	}
	dir, err := os.MkdirTemp("", "beishan-cdp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	conn, err := newCDPConn(chrome, dir, true)
	if err != nil {
		t.Fatalf("启动 CDP 连接失败: %v", err)
	}
	defer conn.Close()

	sess, err := conn.attachPage("about:blank", 15*time.Second)
	if err != nil {
		t.Fatalf("attachPage 失败: %v", err)
	}
	if sess == "" {
		t.Fatal("attachPage 返回空 sessionID")
	}

	// 跑一段 JS（字符串结果）
	ua, err := conn.evalString(sess, "navigator.userAgent", 10*time.Second)
	if err != nil {
		t.Fatalf("evalString 失败: %v", err)
	}
	if !strings.Contains(ua, "Chrome") && !strings.Contains(ua, "HeadlessChrome") {
		t.Fatalf("userAgent 不像 Chrome: %q", ua)
	}
	t.Logf("CDP-over-pipe 通了，UA=%s", ua)
}
