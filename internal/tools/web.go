// Package tools — web search and content extraction.
// Port of tools/web_tools.py (2102 lines) from Python Hermes Agent.
package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// checkWebAvailable returns true if any web search backend is configured.
func checkWebAvailable() bool {
	return os.Getenv("TAVILY_API_KEY") != "" ||
		os.Getenv("FIRECRAWL_API_KEY") != "" ||
		os.Getenv("BRAVE_API_KEY") != ""
}

func registerWebTools() {
	RegisterWithCheck("web_search", "Search the web. Returns structured results: {success, data: {web: [{title, url, description, position}]}}.",
		map[string]interface{}{
			"type": "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"query": stringParam("Search query"),
				"limit": intParam("Number of results (default: 5, max: 10)"),
			},
			"required": []string{"query"},
		},
		webSearchHandler,
		checkWebAvailable,
	)

	RegisterWithCheck("web_fetch", "Fetch and extract content from URLs. Supports multiple URLs.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":  stringParam("URL to fetch (or comma-separated list)"),
				"urls": map[string]interface{}{
					"type":        "array",
					"description": "List of URLs to fetch (alternative to url)",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{},
		},
		webFetchHandler,
		checkWebAvailable,
	)

	RegisterWithCheck("web_extract", "Fetch and extract content from a web page URL (alias for web_fetch).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": stringParam("The URL to extract content from"),
			},
			"required": []string{"url"},
		},
		webFetchHandler,
		checkWebAvailable,
	)
}

// ─── Unified output format matching Python ─────────────────────────────────

// WebSearchResult matches Python's {success, data: {web: [{title, url, description, position}]}}
type WebSearchOutput struct {
	Success bool              `json:"success"`
	Data    *WebSearchData    `json:"data,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type WebSearchData struct {
	Web []WebResult `json:"web"`
}

type WebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

// WebFetchOutput matches Python's per-URL extract output
type WebFetchOutput struct {
	Success bool          `json:"success"`
	Data    []FetchResult `json:"data,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type FetchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

func webSearchHandler(args map[string]interface{}) *ToolResult {
	query, _ := args["query"].(string)
	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
		if limit > 10 {
			limit = 10
		}
	}
	if query == "" {
		return errorResult("query is required")
	}

	// 多引擎并行搜索
	results := searchMultiEngine(query, limit)

	out := WebSearchOutput{
		Success: true,
		Data:    &WebSearchData{Web: results},
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return successResult(string(b))
}

// searchMultiEngine 并行调用多个搜索引擎，合并去重后返回。
func searchMultiEngine(query string, limit int) []WebResult {
	type engineResult struct {
		results []WebResult
		err     error
	}

	ch := make(chan engineResult, 2)
	// DuckDuckGo
	go func() {
		r := performDuckDuckGoSearch(query, limit)
		ch <- engineResult{results: r}
	}()
	// Bing
	go func() {
		r := searchBing(query, limit)
		ch <- engineResult{results: r}
	}()

	var all []WebResult
	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		res := <-ch
		for _, r := range res.results {
			key := strings.ToLower(strings.TrimSpace(r.URL))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, r)
		}
	}

	// 按 query 关键词重叠度排序（核心排序：最相关的排最前）
	qWords := strings.Fields(strings.ToLower(query))
	sort.SliceStable(all, func(i, j int) bool {
		scoreI := countQueryMatch(qWords, all[i].Title+" "+all[i].Description)
		scoreJ := countQueryMatch(qWords, all[j].Title+" "+all[j].Description)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		return all[i].Position < all[j].Position
	})

	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// countQueryMatch 计算文本中包含多少个查询关键词。
func countQueryMatch(words []string, text string) int {
	lower := strings.ToLower(text)
	count := 0
	for _, w := range words {
		if len(w) > 1 && strings.Contains(lower, w) {
			count++
		}
	}
	return count
}

func webFetchHandler(args map[string]interface{}) *ToolResult {
	urls := collectURLs(args)
	if len(urls) == 0 {
		return errorResult("url or urls is required")
	}
	if len(urls) > 5 {
		urls = urls[:5]
	}

	var results []FetchResult
	for _, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}

		// Security: block API key exfiltration
		if containsSecret(rawURL) {
			results = append(results, FetchResult{URL: rawURL, Error: "blocked: URL contains potential API key or token"})
			continue
		}

		// Security: SSRF protection
		if !isSafeURL(rawURL) {
			results = append(results, FetchResult{URL: rawURL, Error: "blocked: URL resolves to private/internal network"})
			continue
		}

		content, title, err := fetchAndExtract(rawURL)
		if err != nil {
			results = append(results, FetchResult{URL: rawURL, Error: err.Error()})
		} else {
			results = append(results, FetchResult{URL: rawURL, Title: title, Content: content})
		}
	}

	out := WebFetchOutput{Success: true, Data: results}
	b, _ := json.MarshalIndent(out, "", "  ")
	return successResult(string(b))
}

func collectURLs(args map[string]interface{}) []string {
	if urlsRaw, ok := args["urls"].([]interface{}); ok {
		urls := make([]string, 0, len(urlsRaw))
		for _, u := range urlsRaw {
			if s, ok := u.(string); ok && strings.TrimSpace(s) != "" {
				urls = append(urls, strings.TrimSpace(s))
			}
		}
		return urls
	}
	if u, ok := args["url"].(string); ok && strings.TrimSpace(u) != "" {
		return strings.Split(u, ",")
	}
	return nil
}

// ─── DuckDuckGo Search ─────────────────────────────────────────────────────

var ddgLinkRe = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*class="result-link"[^>]*>([^<]+)</a>`)
var ddgSnippetRe = regexp.MustCompile(`<td class="result-snippet"[^>]*>(.*?)</td>`)
var ddgTitleRe = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>([^<]+)</a>`)

func performDuckDuckGoSearch(query string, limit int) []WebResult {
	// Try DuckDuckGo first
	results := searchDDG(query, limit)
	if len(results) > 0 {
		return results
	}
	// Fallback: Bing (no API key needed)
	return searchBing(query, limit)
}

func searchDDG(query string, limit int) []WebResult {
	// Try DuckDuckGo HTML (non-lite) first - better results
	if results := searchDDGHTML(query, limit); len(results) > 0 {
		return results
	}
	// Fallback to lite version
	encoded := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", encoded)
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HermesAgent/1.0)")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return parseSearchResults(string(body), limit)
}

func searchDDGHTML(query string, limit int) []WebResult {
	encoded := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", encoded)
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	results := parseDDGHTMLResults(string(body), limit)
	return results
}

// parseDDGHTMLResults parses the richer DuckDuckGo HTML results page
func parseDDGHTMLResults(html string, limit int) []WebResult {
	// Find result blocks: class="result" or class="web-result"
	var results []WebResult
	// Simple extraction: find all result titles and their links
	resultRe := regexp.MustCompile(`class="result__title"[^>]*>.*?<a[^>]*href="([^"]*)"[^>]*>([^<]+)</a>`)
	snippetRe := regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</(?:a|td|div)`)

	titleMatches := resultRe.FindAllStringSubmatch(html, limit)
	snippetMatches := snippetRe.FindAllStringSubmatch(html, limit)

	for i, m := range titleMatches {
		if len(m) < 3 {
			continue
		}
		url := strings.TrimSpace(m[1])
		title := strings.TrimSpace(stripTags(m[2]))
		if url == "" || title == "" || strings.Contains(url, "duckduckgo.com") {
			continue
		}
		snippet := ""
		if i < len(snippetMatches) && len(snippetMatches[i]) > 1 {
			snippet = strings.TrimSpace(stripTags(snippetMatches[i][1]))
		}
		results = append(results, WebResult{
			Title: title, URL: url, Description: snippet, Position: i + 1,
		})
	}
	return results
}

func searchBing(query string, limit int) []WebResult {
	encoded := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://www.bing.com/search?q=%s&count=%d", encoded, limit)
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return parseSearchResults(string(body), limit)
}

func parseSearchResults(html string, limit int) []WebResult {
	links := parseDDGLinks(html)
	var results []WebResult
	for i, link := range links {
		if i >= limit {
			break
		}
		// Extract snippet: text between this link and the next link
		snippet := extractSnippet(html, link.url, i, links)
		results = append(results, WebResult{
			Title:       link.title,
			URL:         link.url,
			Description: collapseWhitespace(stripTags(snippet)),
			Position:    i + 1,
		})
	}
	return results
}

// extractSnippet finds text near a URL in search results HTML
func extractSnippet(html, url string, idx int, allLinks []ddgLink) string {
	// Find the URL position
	pos := strings.Index(html, url)
	if pos < 0 {
		return ""
	}
	// Get text after the link, up to 300 chars or next link
	end := pos + len(url) + 300
	if end > len(html) {
		end = len(html)
	}
	text := html[pos+len(url) : end]
	// Stop at next link start
	if nextLink := strings.Index(text, "<a "); nextLink > 20 {
		text = text[:nextLink]
	}
	text = collapseWhitespace(stripTags(text))
	if len(text) > 200 {
		text = text[:197] + "..."
	}
	return text
}

type ddgLink struct {
	url   string
	title string
}

func parseDDGLinks(html string) []ddgLink {
	var links []ddgLink
	matches := ddgTitleRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		href := strings.TrimSpace(m[1])
		title := strings.TrimSpace(stripTags(collapseWhitespace(m[2])))

		// Skip internal DuckDuckGo links
		if strings.HasPrefix(href, "/") || strings.Contains(href, "duckduckgo.com") ||
			strings.Contains(href, "spreadprivacy.com") {
			continue
		}
		// Skip non-http links
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			href = "https://" + strings.TrimPrefix(href, "//")
		}
		if title == "" {
			continue
		}
		links = append(links, ddgLink{url: href, title: title})
	}
	return links
}

func parseDDGSnippets(html string) []string {
	return ddgSnippetRe.FindAllString(html, -1)
}

// ─── URL Fetch ────────────────────────────────────────────────────────────

func fetchAndExtract(rawURL string) (content, title string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", rawURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HermesAgent/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	raw := string(body)

	// HTML → text conversion for text/html content
	if strings.Contains(contentType, "html") {
		raw = stripTags(raw)
		raw = collapseWhitespace(raw)
		title = extractTitle(string(body))
	}

	if len(raw) > 50000 {
		raw = raw[:50000] + fmt.Sprintf("\n... [truncated, total %d chars]", len(raw))
	}

	return raw, title, nil
}

func extractTitle(html string) string {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<title>")
	if start < 0 {
		return ""
	}
	start += 7
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

// ─── Security: SSRF Protection ────────────────────────────────────────────

var privateRanges = []string{
	"10.", "172.16.", "172.17.", "172.18.", "172.19.",
	"172.20.", "172.21.", "172.22.", "172.23.", "172.24.",
	"172.25.", "172.26.", "172.27.", "172.28.", "172.29.",
	"172.30.", "172.31.", "192.168.", "127.", "0.",
}

func isSafeURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" || host == "localhost" {
		return false
	}

	// Block internal IP ranges
	for _, prefix := range privateRanges {
		if strings.HasPrefix(host, prefix) {
			return false
		}
	}

	// Block IPv6 loopback
	if host == "::1" || host == "[::1]" {
		return false
	}

	// DNS resolution check
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return false
		}
	}

	return true
}

// ─── Security: API Key / Secret Leak Detection ───────────────────────────

var secretKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|token|secret|password|credential)[=:]\s*['"]?\S+['"]?`),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`(?:AKIA|ASIA)[A-Z0-9]{16}`),
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
}

func containsSecret(u string) bool {
	decoded, err := url.QueryUnescape(u)
	if err != nil {
		decoded = u
	}
	for _, p := range secretKeyPatterns {
		if p.MatchString(decoded) || p.MatchString(u) {
			return true
		}
	}
	return false
}

// ─── HTML utilities ───────────────────────────────────────────────────────

// extractArticleText 从 HTML 中提取正文内容。
// 先移除 script/style/noscript 块，再剥离标签和空白。
func extractArticleText(html string) string {
	// 移除 <script>...</script> 块
	for {
		start := strings.Index(html, "<script")
		if start < 0 {
			break
		}
		end := strings.Index(html[start:], "</script>")
		if end < 0 {
			break
		}
		html = html[:start] + " " + html[start+end+9:]
	}
	// 移除 <style>...</style> 块
	for {
		start := strings.Index(html, "<style")
		if start < 0 {
			break
		}
		end := strings.Index(html[start:], "</style>")
		if end < 0 {
			break
		}
		html = html[:start] + " " + html[start+end+8:]
	}
	// 移除 <noscript>...</noscript> 块
	for {
		start := strings.Index(html, "<noscript")
		if start < 0 {
			break
		}
		end := strings.Index(html[start:], "</noscript>")
		if end < 0 {
			break
		}
		html = html[:start] + " " + html[start+end+11:]
	}
	// 剥离剩余标签 + 空白整理
	raw := stripTags(html)
	return collapseWhitespace(raw)
}

func stripTags(s string) string {
	var result strings.Builder
	inTag := false
	for i := 0; i < len(s); i++ {
		r := s[i]
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			result.WriteByte('\n')
		default:
			if !inTag {
				result.WriteByte(r)
			}
		}
	}
	return result.String()
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}
