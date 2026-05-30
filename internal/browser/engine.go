// Package browser 提供引擎无关的浏览器接口。
//
// Engine 抽象了一个浏览器引擎（Chrome/Servo），Page 抽象了一个页面。
// 上层工具（deepseek_web_search 等）通过接口调用，不依赖具体引擎。
//
// 当前实现：
//   - chromeCDP：CDP-over-pipe（走系统 Chrome，今天可用）
//   - servoEngine：WebDriver / 自定义 IPC（开发中）
package browser

import "time"

// pageTimeout 是 CDP 命令的默认超时。
const pageTimeout = 30 * time.Second

// Engine 浏览器引擎。抽象 Chrome/Servo 的共性。
type Engine interface {
	// NewPage 创建新页面，返回 Page 实例。
	NewPage(url string) (Page, error)
	// Close 关闭引擎（释放浏览器进程等）。
	Close()
}

// Page 单个页面控制句柄。
type Page interface {
	// Eval 执行 JS，返回字符串结果。
	Eval(js string) (string, error)
	// InnerText 读取 document.body.innerText。
	InnerText() (string, error)
	// InsertText 模拟真实输入（React 受控组件友好）。
	InsertText(text string) error
	// PressKey 模拟按键。
	PressKey(key string) error
	// Navigate 导航到 URL。
	Navigate(url string) error
	// URL 返回当前页面 URL。
	URL() (string, error)
	// Screenshot 截取页面截图（可选，返回 nil 表示不支持）。
	Screenshot() ([]byte, error)
	// Close 关闭页面。
	Close()
}

// PageExt 可选页面能力扩展。通过类型断言检测是否支持：
//
//	if ext, ok := page.(PageExt); ok { ext.PrintToPDF() }
type PageExt interface {
	// PrintToPDF 将当前页面导出为 PDF（返回 bytes）。
	PrintToPDF() ([]byte, error)
	// PerformanceMetrics 返回页面性能指标。
	PerformanceMetrics() (map[string]float64, error)
	// SecurityState 返回页面安全状态（secure / warning / insecure）。
	SecurityState() (string, error)
}

// NetworkPage 网络捕获扩展（Chrome CDP 特有）。
type NetworkPage interface {
	// StartNetworkCapture 开始捕获网络响应。同一页面重复调用是 no-op。
	StartNetworkCapture() error
	// StopNetworkCapture 停止捕获并返回已捕获的响应列表。
	StopNetworkCapture() []NetworkResponse
}

// NetworkResponse 捕获的网络响应。
type NetworkResponse struct {
	URL        string `json:"url"`
	Status     int    `json:"status"`
	StatusCode int    `json:"status_code"`
	Type       string `json:"type"` // Document/Script/Stylesheet/XHR/Fetch/Media
	MimeType   string `json:"mime_type"`
	Body       string `json:"body,omitempty"`
}
