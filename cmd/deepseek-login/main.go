// Command deepseek-login 打开一个「有头」Chrome（用 beishan 的 DeepSeek profile），
// 让你在 DeepSeek 网页版登录一次。登录后直接关闭窗口即可——profile 持久化，
// 之后 deepseek_web_search 工具会用 headless 自动复用这份登录态。
//
// 用法：go run ./cmd/deepseek-login
package main

import (
	"fmt"
	"os"
	"os/exec"

	"beishan/internal/tools"
)

func main() {
	chrome := tools.FindChrome()
	if chrome == "" {
		fmt.Fprintln(os.Stderr, "未找到 Chrome。设 BEISHAN_CHROME 或安装 Google Chrome 后重试。")
		os.Exit(1)
	}
	profile := tools.DeepseekProfileDir()
	if err := os.MkdirAll(profile, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建 profile 目录失败:", err)
		os.Exit(1)
	}

	url := os.Getenv("BEISHAN_DEEPSEEK_URL")
	if url == "" {
		url = "https://chat.deepseek.com/"
	}

	fmt.Println("即将打开 DeepSeek 网页版，请登录（扫码 / 手机号）。")
	fmt.Println("登录成功后，直接关闭浏览器窗口即可。")
	fmt.Println("profile:", profile)

	// 有头启动（不加 --headless），阻塞直到用户关闭窗口。
	cmd := exec.Command(chrome, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// 用户关闭窗口时 Chrome 可能返回非 0，这通常不是错误
		fmt.Println("（浏览器已关闭）")
	}
	fmt.Println("✅ 完成。profile 已持久化，之后 deepseek_web_search 会自动复用这份登录态。")
}
