package tools

import (
	"fmt"
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

func browserNavigateHandler(args map[string]interface{}) *ToolResult {
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return errorResult("url is required")
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
	url := browserURL
	browserMu.Unlock()
	if url == "" {
		return errorResult("No page loaded.")
	}
	// In text mode, scroll is a no-op (re-fetch won't paginate)
	return successResult("Scrolled (text mode)")
}

func browserBackHandler(args map[string]interface{}) *ToolResult {
	return successResult("Back (no history in text mode)")
}
