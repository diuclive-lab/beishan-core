package tools

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

var (
	browserMu    sync.Mutex
	browserURL   string
	browserTitle string
	browserHTML  string
)

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
