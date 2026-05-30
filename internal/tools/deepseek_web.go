package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"beishan/internal/browser"
)

const (
	deepseekStartMarker = "<BEISHAN_JSON>"
	deepseekEndMarker   = "</BEISHAN_JSON>"
)

var deepseekMu sync.Mutex

func DeepseekProfileDir() string {
	if p := os.Getenv("BEISHAN_DEEPSEEK_PROFILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "deepseek_web_profile")
}

func deepseekURL() string {
	if u := os.Getenv("BEISHAN_DEEPSEEK_URL"); u != "" {
		return u
	}
	return "https://chat.deepseek.com/"
}

type deepseekHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type deepseekSearchOutput struct {
	Success  bool          `json:"success"`
	Query    string        `json:"query"`
	Results  []deepseekHit `json:"results"`
	Thinking string        `json:"thinking,omitempty"`
	Backend  string        `json:"backend"`
	Error    string        `json:"error,omitempty"`
}

func buildDeepseekPrompt(query string, maxResults int) string {
	return "请把下面这个查询当成联网搜索任务处理。" +
		"只输出一个 JSON 对象，不要使用 Markdown 代码块，不要输出解释。" +
		"最终回答必须严格包在 " + deepseekStartMarker + " 和 " + deepseekEndMarker + " 之间。" +
		`JSON 结构为 {"query":"","results":[{"title":"","url":"","snippet":""}]}。` +
		fmt.Sprintf("results 最多 %d 条，url 必须是公开来源链接，不能是 deepseek 自身链接。", maxResults) +
		"查询：" + query
}

func extractMarkedJSON(text string) (map[string]interface{}, bool) {
	start := strings.LastIndex(text, deepseekStartMarker)
	end := strings.LastIndex(text, deepseekEndMarker)
	if start == -1 || end == -1 || end <= start {
		return nil, false
	}
	raw := strings.TrimSpace(text[start+len(deepseekStartMarker) : end])
	if raw == "" {
		return nil, false
	}
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(raw), &parsed) == nil {
		return parsed, true
	}
	repaired := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(raw)
	if json.Unmarshal([]byte(repaired), &parsed) == nil {
		return parsed, true
	}
	return nil, false
}

func deepseekLoginState(curURL, bodyText string) string {
	u := strings.ToLower(curURL)
	if strings.Contains(u, "sign_in") || strings.Contains(bodyText, "登录") ||
		strings.Contains(bodyText, "扫码登录") || strings.Contains(bodyText, "请输入手机号") {
		return "sign_in_required"
	}
	if strings.Contains(u, "deepseek") || strings.Contains(bodyText, "联网搜索") ||
		strings.Contains(bodyText, "深度思考") {
		return "ready_or_logged_in"
	}
	return "unknown"
}

func normalizeDeepseekResults(parsed map[string]interface{}, maxResults int) []deepseekHit {
	var hits []deepseekHit
	raw, _ := parsed["results"].([]interface{})
	for _, it := range raw {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		url, _ := m["url"].(string)
		url = strings.TrimSpace(url)
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			continue
		}
		title, _ := m["title"].(string)
		title = strings.TrimSpace(title)
		if title == "" {
			title = url
		}
		snippet, _ := m["snippet"].(string)
		if len(title) > 160 {
			title = title[:160]
		}
		if len(snippet) > 280 {
			snippet = snippet[:280]
		}
		hits = append(hits, deepseekHit{Title: title, URL: url, Snippet: strings.TrimSpace(snippet)})
		if len(hits) >= maxResults {
			break
		}
	}
	return hits
}

const focusInputJS = `(() => {
  const sels = ["textarea", "div[contenteditable='true']", "input[type='text']"];
  for (const s of sels) {
    for (const el of document.querySelectorAll(s)) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) { el.focus(); el.scrollIntoView(); return "true"; }
    }
  }
  return "false";
})()`

func extractThinking(body string) string {
	idx := strings.Index(body, "已思考（用时")
	if idx == -1 {
		idx = strings.Index(body, "已思考")
	}
	if idx == -1 {
		return ""
	}
	seg := body[idx:]
	if end := strings.Index(seg, deepseekStartMarker); end != -1 {
		seg = seg[:end]
	}
	seg = strings.TrimSpace(seg)
	if len(seg) > 6000 {
		seg = seg[:6000]
	}
	return seg
}

func ensureDeepseekToggleOn(page browser.Page, label string) {
	js := fmt.Sprintf(`(() => {
	  for (const el of document.querySelectorAll("div[role='button'].ds-toggle-button")) {
	    if ((el.innerText||"").trim() === %q) {
	      if (el.getAttribute("aria-pressed") !== "true") { el.click(); return "clicked"; }
	      return "on";
	    }
	  }
	  return "not-found";
	})()`, label)
	page.Eval(js)
}

func deepseekWebSearch(query string, maxResults int) deepseekSearchOutput {
	deepseekMu.Lock()
	defer deepseekMu.Unlock()

	out := deepseekSearchOutput{Query: query, Backend: "deepseek_web"}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}

	profile := DeepseekProfileDir()
	os.MkdirAll(profile, 0755)

	eng, err := browser.NewChrome(profile, true)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer eng.Close()

	page, err := eng.NewPage(deepseekURL())
	if err != nil {
		out.Error = "打开 DeepSeek 失败: " + err.Error()
		return out
	}
	defer page.Close()

	for i := 0; i < 20; i++ {
		rs, _ := page.Eval("document.readyState")
		if rs == "complete" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)

	bodyText, _ := page.Eval("document.body ? document.body.innerText : ''")
	curURL, _ := page.Eval("location.href")
	if deepseekLoginState(curURL, bodyText) == "sign_in_required" {
		out.Error = "DeepSeek 网页版需要一次性登录：运行  go run ./cmd/deepseek-login  登录一次（profile 持久化后自动复用）"
		return out
	}

	ensureDeepseekToggleOn(page, "智能搜索")
	ensureDeepseekToggleOn(page, "深度思考")
	time.Sleep(600 * time.Millisecond)

	focused, _ := page.Eval(focusInputJS)
	if !strings.Contains(focused, "true") {
		out.Error = "未找到 DeepSeek 输入框（可能未登录或页面结构变化）"
		return out
	}

	if err := page.InsertText(buildDeepseekPrompt(query, maxResults)); err != nil {
		out.Error = "输入提示词失败: " + err.Error()
		return out
	}
	time.Sleep(900 * time.Millisecond)
	if err := page.PressKey("Enter"); err != nil {
		out.Error = "提交失败: " + err.Error()
		return out
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1500 * time.Millisecond)
		text, err := page.Eval("document.body ? document.body.innerText : ''")
		if err != nil {
			continue
		}
		parsed, ok := extractMarkedJSON(text)
		if !ok {
			continue
		}
		out.Results = normalizeDeepseekResults(parsed, maxResults)
		out.Thinking = extractThinking(text)
		out.Success = len(out.Results) > 0
		if !out.Success {
			out.Error = "DeepSeek 返回了 JSON 但无有效结果"
		}
		return out
	}
	out.Error = "等待 DeepSeek 结构化结果超时（120s）"
	return out
}

func deepseekWebSearchHandler(args map[string]interface{}) *ToolResult {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return errorResult("query is required")
	}
	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	out := deepseekWebSearch(query, limit)
	b, _ := json.MarshalIndent(out, "", "  ")
	if !out.Success {
		return errorResult(string(b))
	}
	return successResult(string(b))
}

func registerDeepseekWebTools() {
	Register("deepseek_web_search",
		"用 DeepSeek 网页版的免费联网搜索做搜索（beishan 自有 headless Chrome 驱动，需一次性登录）。返回 {success, results:[{title,url,snippet}], thinking}。Tavily 不可用或想要 DeepSeek 深度思考时用。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": stringParam("搜索查询"),
				"limit": intParam("结果条数（默认 5，最大 10）"),
			},
			"required": []string{"query"},
		},
		deepseekWebSearchHandler,
	)
}
