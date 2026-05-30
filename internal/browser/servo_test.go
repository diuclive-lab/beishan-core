package browser

import (
	"os"
	"strings"
	"testing"
)

func TestServoNavigateAndEval(t *testing.T) {
	if os.Getenv("BEISHAN_SERVO_TEST") == "" {
		t.Skip("设 BEISHAN_SERVO_TEST=1 才跑（要起 Servo 进程，非 hermetic）")
	}
	eng, err := NewServo()
	if err != nil {
		t.Fatalf("NewServo 失败: %v", err)
	}
	defer eng.Close()

	page, err := eng.NewPage("https://example.com")
	if err != nil {
		t.Fatalf("NewPage 失败: %v", err)
	}

	title, err := page.Eval("return document.title")
	if err != nil {
		t.Fatalf("Eval 失败: %v", err)
	}
	if !strings.Contains(title, "Example Domain") {
		t.Errorf("期望 title 含 'Example Domain'，实际: %q", title)
	}
	t.Logf("Servo 端到端: title=%q", title)

	inner, err := page.InnerText()
	if err != nil {
		t.Fatalf("InnerText 失败: %v", err)
	}
	if !strings.Contains(inner, "example") {
		t.Errorf("innerText 应含 'example'，实际: %q", inner[:min(60, len(inner))])
	}
	t.Logf("innerText: %s...", inner[:min(60, len(inner))])
}
