package tools

// cdp.go — 兼容层。CDP-over-pipe 核心已迁至 internal/browser/chrome_cdp.go。
// 这里保留对外导出函数，供旧代码过渡调用。

import (
	"beishan/internal/browser"
)

// FindChrome 返回系统 Chrome 可执行文件路径。
// 设 BEISHAN_CHROME 环境变量可覆盖。
func FindChrome() string {
	return browser.FindChromePath()
}

// DefaultChromeProfile 返回默认的持久 Chrome profile 路径。
func DefaultChromeProfile() string {
	return browser.DefaultProfileDir()
}

// NewChromeEngine 创建并启动一个 Chrome 浏览器引擎。
// 是 browser.NewChrome 的简写，供旧代码使用。
func NewChromeEngine(userDataDir string, headless bool) (browser.Engine, error) {
	return browser.NewChrome(userDataDir, headless)
}
