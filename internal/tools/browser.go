package tools

import (
	"fmt"
	"net/url"
	"strings"
	beishan_browser "beishan/internal/browser"
	"encoding/json"
	"sync"
)

var (
	browserMu    sync.Mutex
	browserURL   string
	browserTitle string
	browserHTML  string
)

// GlobalEngine 是 CDP 浏览器引擎全局实例（由 main.go 初始化）。
var GlobalEngine beishan_browser.Engine

func registerBrowserTools() {
	Register("browser_navigate", "Load a URL and extract its text content.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"url"},
			"properties": map[string]interface{}{
				"url": stringParam("URL to navigate to"),
			},
		},
		browserNavigateHandler,
	)

	Register("browser_snapshot", "Get the current page's text content.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{
				"full": boolParam("Return full content (default: truncated to 3000 chars)"),
			},
		},
		browserSnapshotHandler,
	)

	Register("browser_click", "Click a link identified by text or ref.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"ref"},
			"properties": map[string]interface{}{
				"ref": stringParam("Link text or element ref to click"),
			},
		},
		browserClickHandler,
	)

	Register("browser_scroll", "Scroll the page up or down (refetches content).",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"direction"},
			"properties": map[string]interface{}{
				"direction": stringParam("'up' or 'down'"),
			},
		},
		browserScrollHandler,
	)

	Register("browser_back", "Go back to the previous page.",
		emptyObjParams(),
		browserBackHandler,
	)

	Register("browser_eval", "Execute JavaScript on the current CDP-powered page and return result.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"script"},
			"properties": map[string]interface{}{
				"script": stringParam("JavaScript expression to evaluate"),
			},
		},
		browserEvalHandler,
	)

	Register("browser_screenshot", "Take a screenshot of the current CDP-powered page.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{
				"format": stringParam("Image format: png or jpeg (default png)"),
			},
		},
		browserScreenshotHandler,
	)

	Register("browser_print_pdf", "Export the current CDP-powered page as PDF.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		browserPrintPDFHandler,
	)

	Register("browser_performance", "Get performance metrics from the CDP-powered page.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		browserPerformanceHandler,
	)

	Register("browser_security", "Check the security state of the CDP-powered page.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		browserSecurityHandler,
	)

	Register("browser_network_capture", "Start network capture. Returns captured responses after stopping.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		browserNetworkCaptureHandler,
	)

	Register("browser_configure", "Configure browser fingerprint for anti-bot. Set env BEISHAN_BROWSER_FP=1 for default stealth config.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ua":          stringParam("User-Agent string (empty = default stealth)"),
				"webgl_vendor": stringParam("WebGL vendor (e.g. Google Inc.)"),
				"webgl_renderer": stringParam("WebGL renderer (e.g. ANGLE (Apple, Apple M4 Pro, OpenGL 4.1))"),
			},
		},
		browserConfigureHandler,
	)
}

// isSearchEngineURL 判断是否在「用浏览器抓搜索引擎结果页」——这条路会被反爬拦成
// 302 同意页 / 202 挑战页，应改用 web_search。只拦搜索路径（/search、?q=、/html），
// 不拦这些域名的其它页面（如某篇 google 文档）。
func isSearchEngineURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	engines := []string{"google.", "bing.com", "duckduckgo.com", "baidu.com",
		"yandex.", "search.brave.com", "ecosia.org", "startpage.com", "sogou.com", "so.com"}
	isEngine := false
	for _, se := range engines {
		if strings.Contains(host, se) {
			isEngine = true
			break
		}
	}
	if !isEngine {
		return false
	}
	low := strings.ToLower(rawURL)
	return strings.Contains(low, "/search") || strings.Contains(low, "q=") ||
		strings.Contains(strings.ToLower(u.Path), "/html")
}

func browserNavigateHandler(args map[string]interface{}) *ToolResult {
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return errorResult("url is required")
	}

	// 安全：与 web_fetch 对齐——SSRF + 密钥外泄防护（browser_navigate 此前漏了这两个）。
	if containsSecret(urlStr) {
		return errorResult("blocked: URL 含疑似 API key/token")
	}
	if !isSafeURL(urlStr) {
		return errorResult("blocked: URL 解析到私有/内网地址")
	}
	// 搜索请走 web_search：裸 HTTP 抓搜索引擎会被反爬（302/202 挑战页），别用浏览器搜。
	if isSearchEngineURL(urlStr) {
		return errorResult("browser_navigate 不适合抓搜索引擎结果（会被反爬拦成 302/挑战页，拿不到结果）。请改用 web_search 工具搜索。")
	}

	content, title, err := fetchAndExtract(urlStr)
	if err != nil {
		return errorResult(fmt.Sprintf("navigate: %v", err))
	}

	browserMu.Lock()
	browserURL = urlStr
	browserTitle = title
	browserHTML = content
	browserMu.Unlock()

	return successResult(fmt.Sprintf("Navigated to: %s\nTitle: %s\nSize: %d bytes", urlStr, title, len(content)))
}

func browserSnapshotHandler(args map[string]interface{}) *ToolResult {
	full, _ := args["full"].(bool)

	browserMu.Lock()
	url := browserURL
	title := browserTitle
	content := browserHTML
	browserMu.Unlock()

	if content == "" {
		return errorResult("No page loaded. Use browser_navigate first.")
	}

	text := collapseWhitespace(stripTags(content))
	out := fmt.Sprintf("URL: %s\nTitle: %s\n\n", url, title)
	if full || len(text) <= 3000 {
		out += text
	} else {
		out += text[:3000] + fmt.Sprintf("\n... [%d total chars]", len(text))
	}
	return successResult(out)
}

func browserClickHandler(args map[string]interface{}) *ToolResult {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return errorResult("ref is required")
	}

	browserMu.Lock()
	content := browserHTML
	browserMu.Unlock()

	if content == "" {
		return errorResult("No page loaded.")
	}

	refClean := strings.TrimPrefix(ref, "@")
	lines := strings.Split(content, "\n")
	var foundURL string
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(refClean)) {
			if hrefIdx := strings.Index(strings.ToLower(line), "href="); hrefIdx >= 0 {
				rest := line[hrefIdx+5:]
				if len(rest) > 0 {
					quote := rest[0]
					if endIdx := strings.IndexByte(rest[1:], quote); endIdx >= 0 {
						foundURL = rest[1 : endIdx+1]
					}
				}
			}
			break
		}
	}

	if foundURL == "" {
		return successResult(fmt.Sprintf("Element '%s' found but no link to follow.", ref))
	}

	// Navigate to found URL
	return browserNavigateHandler(map[string]interface{}{"url": foundURL})
}

func browserScrollHandler(args map[string]interface{}) *ToolResult {
	browserMu.Lock()
	curURL := browserURL
	browserMu.Unlock()
	if curURL == "" {
		return errorResult("No page loaded.")
	}
	// 文本模式无分页/滚动：页面已一次性全量抓取。诚实告知，别让智能体以为滚动生效。
	return successResult("browser_scroll 在文本模式浏览器里无效：页面已一次性全量抓取、无分页。用 browser_snapshot {\"full\":true} 看全文。")
}

func browserBackHandler(args map[string]interface{}) *ToolResult {
	// 文本模式无历史栈：诚实告知，别让智能体以为回退生效。
	return successResult("browser_back 在文本模式浏览器里不支持（无历史栈）。请用 browser_navigate 重新指定 URL。")
}


// agentSourceAllowed 检查调用来源是否有权限执行高风险浏览器操作。
// source="agent" 时不允许 eval（避免 Agent 获得浏览器特权）。
// 参考 OWL 的 Direct-to-Renderer 原则：Agent 输入不能绕过 DOM security boundary。
func agentSourceAllowed(args map[string]interface{}, highRisk bool) (string, bool) {
	src, _ := args["agent_source"].(string)
	if src == "agent" && highRisk {
		return "agent 不允许执行浏览器特权操作（eval/screenshot）。请使用 user 来源或降低权限", false
	}
	return "", true
}

func browserEvalHandler(args map[string]interface{}) *ToolResult {
	script, _ := args["script"].(string)
	if script == "" {
		return errorResult("script is required")
	}
	if msg, ok := agentSourceAllowed(args, true); !ok {
		return errorResult(msg)
	}
	if GlobalEngine == nil {
		return errorResult("CDP 浏览器引擎未初始化（main.go 未调用 InitBrowserEngine）")
	}
	page, err := GlobalEngine.NewPage("about:blank")
	if err != nil {
		return errorResult("创建页面失败: " + err.Error())
	}
	defer page.Close()
	result, err := page.Eval(script)
	if err != nil {
		return errorResult("JS 执行失败: " + err.Error())
	}
	return successResult(result)
}

func browserScreenshotHandler(args map[string]interface{}) *ToolResult {
	if msg, ok := agentSourceAllowed(args, true); !ok {
		return errorResult(msg)
	}
	if GlobalEngine == nil {
		return errorResult("CDP 浏览器引擎未初始化")
	}
	page, err := GlobalEngine.NewPage("about:blank")
	if err != nil {
		return errorResult("创建页面失败: " + err.Error())
	}
	defer page.Close()
	data, err := page.Screenshot()
	if err != nil {
		return errorResult("截图失败: " + err.Error())
	}
	return successResult(fmt.Sprintf("data:image/png;base64,%s", string(data)))
}


func withPage(fn func(beishan_browser.Page) *ToolResult) *ToolResult {
	if GlobalEngine == nil {
		return errorResult("CDP 浏览器引擎未初始化")
	}
	page, err := GlobalEngine.NewPage("about:blank")
	if err != nil {
		return errorResult("创建页面失败: " + err.Error())
	}
	defer page.Close()
	return fn(page)
}

func withPageExt(fn func(beishan_browser.PageExt) *ToolResult) *ToolResult {
	return withPage(func(page beishan_browser.Page) *ToolResult {
		ext, ok := page.(beishan_browser.PageExt)
		if !ok {
			return errorResult("当前浏览器引擎不支持此 CDP 扩展能力")
		}
		return fn(ext)
	})
}

func browserPrintPDFHandler(_ map[string]interface{}) *ToolResult {
	return withPageExt(func(ext beishan_browser.PageExt) *ToolResult {
		data, err := ext.PrintToPDF()
		if err != nil {
			return errorResult("PDF 导出失败: " + err.Error())
		}
		return successResult(fmt.Sprintf("data:application/pdf;base64,%s", string(data)))
	})
}

func browserPerformanceHandler(_ map[string]interface{}) *ToolResult {
	return withPageExt(func(ext beishan_browser.PageExt) *ToolResult {
		metrics, err := ext.PerformanceMetrics()
		if err != nil {
			return errorResult("性能指标获取失败: " + err.Error())
		}
		b, _ := json.MarshalIndent(metrics, "", "  ")
		return successResult(string(b))
	})
}

func browserSecurityHandler(_ map[string]interface{}) *ToolResult {
	return withPageExt(func(ext beishan_browser.PageExt) *ToolResult {
		state, err := ext.SecurityState()
		if err != nil {
			return errorResult("安全状态检测失败: " + err.Error())
		}
		return successResult("{\"security_state\":\"" + state + "\"}")
	})
}

func browserNetworkCaptureHandler(_ map[string]interface{}) *ToolResult {
	return withPage(func(page beishan_browser.Page) *ToolResult {
		netPage, ok := page.(beishan_browser.NetworkPage)
		if !ok {
			return errorResult("当前浏览器引擎不支持网络捕获")
		}
		if err := netPage.StartNetworkCapture(); err != nil {
			return errorResult("网络捕获启动失败: " + err.Error())
		}
		resp := netPage.StopNetworkCapture()
		b, _ := json.MarshalIndent(resp, "", "  ")
		return successResult(string(b))
	})
}


func browserConfigureHandler(args map[string]interface{}) *ToolResult {
	if GlobalEngine == nil {
		return errorResult("CDP 浏览器引擎未初始化")
	}
	fpEng, ok := GlobalEngine.(beishan_browser.FingerprintEngine)
	if !ok {
		return errorResult("当前引擎不支持指纹配置（仅 Chrome）")
	}
	fp := beishan_browser.GetDefaultFingerprint()
	if ua, ok := args["ua"].(string); ok && ua != "" {
		fp.UserAgent = ua
	}
	if v, ok := args["webgl_vendor"].(string); ok && v != "" {
		fp.WebGLVendor = v
	}
	if r, ok := args["webgl_renderer"].(string); ok && r != "" {
		fp.WebGLRenderer = r
	}
	page, err := GlobalEngine.NewPage("about:blank")
	if err != nil {
		return errorResult("创建页面失败: " + err.Error())
	}
	defer page.Close()
	if err := fpEng.ApplyFingerprint(page, fp); err != nil {
		return errorResult("指纹配置失败: " + err.Error())
	}
	return successResult("{\"status\":\"configured\",\"ua\":\"" + fp.UserAgent + "\"}")
}
