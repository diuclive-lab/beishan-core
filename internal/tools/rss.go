package tools

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// stripHTML 移除 HTML/XML 标签，保留纯文本内容。
func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, "")
	// 常见 HTML 实体解码
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return s
}

/* ─── rss_fetch L3 工具 ─────────────────────────

   抓取并解析 RSS 2.0 / Atom 订阅源。
   纯 Go 标准库，零外部依赖（只用 encoding/xml）。
*/

// rssFeed RSS 2.0 订阅源结构
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title       string    `xml:"title"`
		Description string    `xml:"description"`
		Link        string    `xml:"link"`
		Items       []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
}

// atomFeed Atom 订阅源结构
type atomFeed struct {
	XMLName xml.Name   `xml:"feed"`
	Title   string     `xml:"title"`
	Entries []atomItem `xml:"entry"`
}

type atomItem struct {
	Title   string `xml:"title"`
	Link    struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
	Summary string `xml:"summary"`
	Updated string `xml:"updated"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

// FeedItem 统一条目
type FeedItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Published   string `json:"published"`
	Source      string `json:"source"`
}

// RssFetchResult 抓取结果
type RssFetchResult struct {
	FeedTitle string     `json:"feed_title"`
	Items     []FeedItem `json:"items"`
	Count     int        `json:"count"`
	Error     string     `json:"error,omitempty"`
}

// ─── RSS 熔断器 ─────────────────────────────────────

const (
	rssMaxFailures = 5               // 连续失败次数阈值
	rssCooldown    = 24 * time.Hour  // 熔断冷却时间
)

type rssCircuitState struct {
	failures int
	lastFail time.Time
	offline  bool
}

var rssCircuitBreaker = make(map[string]*rssCircuitState)
var rssCircuitMu sync.Mutex

func rssCheckCircuit(url string) bool {
	rssCircuitMu.Lock()
	defer rssCircuitMu.Unlock()
	state, ok := rssCircuitBreaker[url]
	if !ok {
		return false
	}
	if state.offline && time.Since(state.lastFail) < rssCooldown {
		return true // 熔断中
	}
	if state.offline && time.Since(state.lastFail) >= rssCooldown {
		// 冷却结束，重试
		state.offline = false
		state.failures = 0
	}
	return false
}

func rssRecordFailure(url string) {
	rssCircuitMu.Lock()
	defer rssCircuitMu.Unlock()
	state, ok := rssCircuitBreaker[url]
	if !ok {
		state = &rssCircuitState{}
		rssCircuitBreaker[url] = state
	}
	state.failures++
	state.lastFail = time.Now()
	if state.failures >= rssMaxFailures {
		state.offline = true
		fmt.Printf("[rss] 源 %s 连续失败 %d 次，标记为 offline（%s 后重试）\n", url, state.failures, rssCooldown)
	}
}

func rssRecordSuccess(url string) {
	rssCircuitMu.Lock()
	defer rssCircuitMu.Unlock()
	if state, ok := rssCircuitBreaker[url]; ok {
		state.failures = 0
		state.offline = false
	}
}

func fetchRSS(url string, limit int) *RssFetchResult {
	// 熔断检查
	if rssCheckCircuit(url) {
		return &RssFetchResult{Error: "源已熔断（连续失败过多，24小时后自动重试）"}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		rssRecordFailure(url)
		return &RssFetchResult{Error: fmt.Sprintf("HTTP 请求失败: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	raw := strings.TrimSpace(string(body))

	// 尝试解析 RSS 2.0
	var rss rssFeed
	if err := xml.Unmarshal([]byte(raw), &rss); err == nil && len(rss.Channel.Items) > 0 {
		result := &RssFetchResult{
			FeedTitle: rss.Channel.Title,
		}
		for _, item := range rss.Channel.Items {
			desc := stripHTML(item.Description)
			if len([]rune(desc)) > 300 {
				desc = string([]rune(desc)[:300]) + "..."
			}
			result.Items = append(result.Items, FeedItem{
				Title:       item.Title,
				Link:        item.Link,
				Description: desc,
				Published:   item.PubDate,
				Source:      rss.Channel.Title,
			})
			if len(result.Items) >= limit {
				break
			}
		}
		result.Count = len(result.Items)
		rssRecordSuccess(url)
		return result
	}

	// 尝试解析 Atom
	var atom atomFeed
	if err := xml.Unmarshal([]byte(raw), &atom); err == nil && len(atom.Entries) > 0 {
		result := &RssFetchResult{
			FeedTitle: atom.Title,
		}
		for _, entry := range atom.Entries {
			desc := stripHTML(entry.Summary)
			if len([]rune(desc)) > 300 {
				desc = string([]rune(desc)[:300]) + "..."
			}
			result.Items = append(result.Items, FeedItem{
				Title:       entry.Title,
				Link:        entry.Link.Href,
				Description: desc,
				Published:   entry.Updated,
				Source:      atom.Title,
			})
			if len(result.Items) >= limit {
				break
			}
		}
		result.Count = len(result.Items)
		rssRecordSuccess(url)
		return result
	}

	rssRecordFailure(url)
	return &RssFetchResult{Error: "无法解析为 RSS 2.0 或 Atom 格式"}
}

func RssFetchHandler(args map[string]interface{}) *ToolResult {
	url, _ := args["url"].(string)
	if url == "" {
		return errorResult("url（RSS 订阅源地址）不能为空")
	}
	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 50 {
			limit = 50
		}
	}

	result := fetchRSS(url, limit)
	if result.Error != "" {
		return errorResult(result.Error)
	}

	b, _ := json.Marshal(result)
	return successResult(string(b))
}

func RssDefaultHandler(args map[string]interface{}) *ToolResult {
	// 默认监控源列表
	sources := []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Lang string `json:"lang"`
	}{
		{Name: "Hacker News", URL: "https://hnrss.org/frontpage?count=10", Lang: "en"},
		{Name: "Go Blog", URL: "https://go.dev/blog/feed.atom", Lang: "en"},
		{Name: "GitHub Trending Go", URL: "https://github.com/trending/go.rss", Lang: "en"},
		{Name: "掘金前端", URL: "https://feed.juejin.im/", Lang: "zh"},
		{Name: "InfoQ 中文", URL: "https://feed.infoq.cn/", Lang: "zh"},
	}

	var allItems []FeedItem
	var errors []string
	for _, src := range sources {
		result := fetchRSS(src.URL, 3)
		if result.Error != "" {
			errors = append(errors, fmt.Sprintf("%s: %s", src.Name, result.Error))
			continue
		}
		for i := range result.Items {
			result.Items[i].Source = src.Name
		}
		allItems = append(allItems, result.Items...)
	}

	output := map[string]interface{}{
		"items":  allItems,
		"count":  len(allItems),
		"errors": errors,
	}
	b, _ := json.Marshal(output)
	return successResult(string(b))
}

func registerRSSTools() {
	Register("rss_fetch", "抓取并解析 RSS 2.0 / Atom 订阅源，返回条目列表。零外部依赖。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"url"},
			"properties": map[string]interface{}{
				"url":   stringParam("RSS 订阅源 URL"),
				"limit": intParam("最大返回条目数（默认 10，最大 50）"),
			},
		},
		RssFetchHandler,
	)

	Register("rss_default", "抓取默认技术资讯源列表（HN/Go Blog/GitHub Trending/掘金/InfoQ），每源取 top 3。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		RssDefaultHandler,
	)
}
