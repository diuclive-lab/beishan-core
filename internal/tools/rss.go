package tools

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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

func fetchRSS(url string, limit int) *RssFetchResult {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
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
			desc := strings.TrimSpace(item.Description)
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
		return result
	}

	// 尝试解析 Atom
	var atom atomFeed
	if err := xml.Unmarshal([]byte(raw), &atom); err == nil && len(atom.Entries) > 0 {
		result := &RssFetchResult{
			FeedTitle: atom.Title,
		}
		for _, entry := range atom.Entries {
			desc := strings.TrimSpace(entry.Summary)
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
		return result
	}

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
