package tools

// deepseek_web.go — 把 DeepSeek 网页版当成「会联网搜索的 LLM」用：beishan 用自己拥有的
// headless Chrome（CDP-over-pipe，见 cdp.go）打开 chat.deepseek.com（复用一次性登录的 profile），
// 往输入框敲一段提示词让它联网搜索并只回结构化 JSON，轮询正文抽取结果，并顺带捕获「已思考」。
//
// 移植自 FangLab scripts/mcp_runtime/browser_mcp_server.py 的 search_via_deepseek，
// 但用原生 Go + CDP-over-pipe（不依赖 playwright 库），且新增「已思考」捕获。
//
// 定位：Tavily 仍是主搜索后端；这是一个免费的备选/补充（且深度思考推理时有惊喜）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	deepseekStartMarker = "<BEISHAN_JSON>"
	deepseekEndMarker   = "</BEISHAN_JSON>"
)

// deepseekMu 串行化搜索：同一 Chrome profile 同时只能被一个 Chrome 进程持有（SingletonLock）。
var deepseekMu sync.Mutex

// FindChrome 暴露给登录辅助命令用。
func FindChrome() string { return findChrome() }

// DeepseekProfileDir 是持久化登录态的 Chrome profile 目录（BEISHAN_DEEPSEEK_PROFILE 覆盖）。
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
	Thinking string        `json:"thinking,omitempty"` // DeepSeek「已思考」深度推理（bonus）
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

// extractMarkedJSON 从正文里抽出最后一对 <BEISHAN_JSON>...</BEISHAN_JSON> 并解析。
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
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return nil, false
	}
	return parsed, true
}

// deepseekLoginState 判断当前页面是否需要登录。
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

// focusInputJS 找到第一个可见的输入框（textarea / contenteditable）并聚焦。
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

// extractThinkingJS 尽力读取 DeepSeek「深度思考」推理块（启发式选择器；找不到则空）。
const extractThinkingJS = `(() => {
  const sels = ["[class*='think']","[class*='Think']","[class*='reason']","[class*='Reason']"];
  let best = "";
  for (const s of sels) {
    for (const el of document.querySelectorAll(s)) {
      const t = (el.innerText || "").trim();
      if (t.length > best.length) best = t;
    }
  }
  return best.slice(0, 4000);
})()`

// deepseekWebSearch 是核心流程。
func deepseekWebSearch(query string, maxResults int) deepseekSearchOutput {
	deepseekMu.Lock()
	defer deepseekMu.Unlock()

	out := deepseekSearchOutput{Query: query, Backend: "deepseek_web"}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = 5
	}
	chrome := findChrome()
	if chrome == "" {
		out.Error = "未找到 Chrome（设 BEISHAN_CHROME 或安装 Google Chrome）"
		return out
	}
	profile := DeepseekProfileDir()
	os.MkdirAll(profile, 0755)

	conn, err := newCDPConn(chrome, profile, true)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer conn.Close()

	sess, err := conn.attachPage(deepseekURL(), 60*time.Second)
	if err != nil {
		out.Error = "打开 DeepSeek 失败: " + err.Error()
		return out
	}

	// 等 SPA 渲染
	for i := 0; i < 20; i++ {
		rs, _ := conn.evalString(sess, "document.readyState", 5*time.Second)
		if rs == "complete" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)

	bodyText, _ := conn.evalString(sess, "document.body ? document.body.innerText : ''", 10*time.Second)
	curURL, _ := conn.evalString(sess, "location.href", 5*time.Second)
	if deepseekLoginState(curURL, bodyText) == "sign_in_required" {
		out.Error = "DeepSeek 网页版需要一次性登录：运行  go run ./cmd/deepseek-login  登录一次（profile 持久化后自动复用）"
		return out
	}

	focused, _ := conn.evalString(sess, focusInputJS, 5*time.Second)
	if !strings.Contains(focused, "true") {
		out.Error = "未找到 DeepSeek 输入框（可能未登录或页面结构变化）"
		return out
	}

	if err := conn.insertText(sess, buildDeepseekPrompt(query, maxResults), 15*time.Second); err != nil {
		out.Error = "输入提示词失败: " + err.Error()
		return out
	}
	if err := conn.pressEnter(sess, 5*time.Second); err != nil {
		out.Error = "提交失败: " + err.Error()
		return out
	}

	// 轮询正文抓 marked JSON（深度思考较慢，给 90s）
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1200 * time.Millisecond)
		text, err := conn.evalString(sess, "document.body ? document.body.innerText : ''", 10*time.Second)
		if err != nil {
			continue
		}
		parsed, ok := extractMarkedJSON(text)
		if !ok {
			continue
		}
		out.Results = normalizeDeepseekResults(parsed, maxResults)
		if think, terr := conn.evalString(sess, extractThinkingJS, 5*time.Second); terr == nil {
			out.Thinking = think
		}
		out.Success = len(out.Results) > 0
		if !out.Success {
			out.Error = "DeepSeek 返回了 JSON 但无有效结果"
		}
		return out
	}
	out.Error = "等待 DeepSeek 结构化结果超时（90s）"
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
		"用 DeepSeek 网页版的免费联网搜索做搜索（beishan 自有 headless 浏览器驱动，需一次性登录）。返回 {success, results:[{title,url,snippet}], thinking}。Tavily 不可用或想要 DeepSeek 深度思考时用。",
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
