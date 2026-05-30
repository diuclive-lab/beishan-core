package browser

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAntibotBaseline(t *testing.T) {
	if os.Getenv("BEISHAN_DEEPSEEK_TEST") == "" {
		t.Skip("设 BEISHAN_DEEPSEEK_TEST=1 才跑（要起 Chrome）")
	}

	targets := []struct {
		name string
		url  string
	}{
		{"sannysoft", "https://bot.sannysoft.com"},
		{"creepjs", "https://creepjs.site"},
		{"fingerprint", "https://fingerprint.com/products/bot-detection/demo"},
	}

	for _, tt := range targets {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := NewChromeWithConfig(ChromeConfig{Headless: true})
			if err != nil {
				t.Fatalf("启动 Chrome 失败: %v", err)
			}
			defer eng.Close()

			page, err := eng.NewPage(tt.url)
			if err != nil {
				t.Fatalf("NewPage(%s) 失败: %v", tt.url, err)
			}
			defer page.Close()
			time.Sleep(3 * time.Second)

			text, err := page.InnerText()
			if err != nil {
				t.Logf("InnerText 失败: %v", err)
			}
			blocked := checkBlocked(text)
			savePath := fmt.Sprintf("/tmp/baseline_%s.txt", tt.name)
			os.WriteFile(savePath, []byte(text), 0644)

			url, _ := page.URL()
			t.Logf("\n=== %s ===\nFINAL_URL=%s\nBLOCKED=%v\nSAVED=%s\n%s", tt.name, url, blocked, savePath, truncate(text, 300))
		})
	}
}

func checkBlocked(text string) bool {
	signals := []string{
		"challenge", "cf-browser-", "attention required", "verify you are human",
		"enable javascript", "please turn javascript", "blocked", "access denied",
		"检测到异常", "请完成安全验证", "人机验证", "your request has been blocked",
	}
	lower := strings.ToLower(text)
	for _, s := range signals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
