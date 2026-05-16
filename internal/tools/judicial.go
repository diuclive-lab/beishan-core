package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

/* JudicialSearchResult 司法大数据检索返回结果。

   与 LegalSearchPlugin 的 LegalReference 结构对齐，
   确保 legal_search_plugin 可以直接解析。
*/
type JudicialSearchResult struct {
	Title    string `json:"title"`              // 标题
	Source   string `json:"source"`             // 数据源：data_court / wenshu / model_knowledge
	URL      string `json:"url,omitempty"`      // 原文链接
	Summary  string `json:"summary,omitempty"`  // 摘要
	CaseType string `json:"case_type,omitempty"` // 案由（裁判文书）
	Court    string `json:"court,omitempty"`     // 法院（裁判文书）
	Year     string `json:"year,omitempty"`      // 年份
	Error    string `json:"error,omitempty"`     // 检索异常信息
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func registerJudicialTools() {
	Register("judicial_search", "检索中国司法大数据服务网(data.court.gov.cn)和中国裁判文书网(wenshu.court.gov.cn)的公开数据。返回法律法规、司法案例和研究报告。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":      stringParam("搜索关键词"),
				"case_type":  stringParam("案由，如\"买卖合同纠纷\"\"劳动争议\""),
				"court_level": stringParam("法院层级：基层/中级/高级/最高"),
				"source":     stringParam("数据源：data_court/wenshu/auto，默认auto自动选择"),
			},
			"required": []string{"query"},
		},
		judicialSearchHandler,
	)
}

func judicialSearchHandler(args map[string]interface{}) *ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("judicial_search: query 不能为空")
	}

	caseType, _ := args["case_type"].(string)
	courtLevel, _ := args["court_level"].(string)
	source, _ := args["source"].(string)

	results := []JudicialSearchResult{}

	// 按数据源优先级检索
	switch source {
	case "data_court":
		results = searchDataCourt(query, caseType)
	case "wenshu":
		results = searchWenshu(query, caseType, courtLevel)
	default:
		// auto: 先查司法大数据服务网，再查裁判文书网
		results = searchDataCourt(query, caseType)
		if len(results) == 0 {
			results = searchWenshu(query, caseType, courtLevel)
		}
	}

	// 如果两个数据源都无结果，返回空（由调用方决定是否回退 web_search）
	if len(results) == 0 {
		return successResult("[]")
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("judicial_search: 结果序列化失败: %v", err))
	}

	return successResult(string(data))
}

/* searchDataCourt 查询中国司法大数据服务网公开数据。

   免费接口限制（非注册用户）：
   - 仅支持部分案由（民间借贷、离婚、买卖、交通、信用卡、劳动争议）
   - 仅支持 2016-2019 年数据
   - 返回研究报���和统计分析数据

   参考 claude-for-legal CONNECTORS.md 的 MCP 连接器模式：
   所有结果携带来源标识，允许审查者验证。
*/
func searchDataCourt(query, caseType string) []JudicialSearchResult {
	// 构建公开搜索 URL
	params := url.Values{}
	params.Set("keyword", query)
	if caseType != "" {
		params.Set("caseType", caseType)
	}
	searchURL := "https://data.court.gov.cn/search?" + params.Encode()

	// 尝试获取公开页面
	body, err := httpGet(searchURL)
	if err != nil {
		return []JudicialSearchResult{{
			Title:   "司法大数据服务网暂不可用",
			Source:  "data_court",
			Summary: fmt.Sprintf("data.court.gov.cn 暂时无法访问 (%v)。建议通过浏览器访问 data.court.gov.cn 手动查询。", err),
			Error:   err.Error(),
		}}
	}

	// 解析公开页面（简化：提取标题和摘要）
	results := parseDataCourtHTML(string(body), query)

	// 如果页面解析无结果，返回说明信息
	if len(results) == 0 {
		results = append(results, JudicialSearchResult{
			Title:   query + " — 司法大数据研究报告",
			Source:  "data_court",
			URL:     searchURL,
			Summary: "请通过 data.court.gov.cn 搜索框查询相关研究报告。当前免费接口仅支持部分案由（民间借贷、离婚、买卖合同、交通事故、信用卡、劳动争议）的统计查询。",
		})
	}

	return results
}

/* searchWenshu 查询中国裁判文书网公开数据。

   使用公开搜索接口，不做大规模爬取。
   注意：裁判文书网有反爬机制，单次查询不保证返回结果。
*/
func searchWenshu(query, caseType, courtLevel string) []JudicialSearchResult {
	// 构建裁判文书网搜索 URL
	params := url.Values{}
	params.Set("searchType", "1")
	params.Set("keyword", query)
	params.Set("pageNum", "1")
	params.Set("pageSize", "5")
	if caseType != "" {
		params.Set("caseType", caseType)
	}
	searchURL := "https://wenshu.court.gov.cn/search?" + params.Encode()

	body, err := httpGet(searchURL)
	if err != nil {
		return []JudicialSearchResult{{
			Title:   "裁判文书网暂不可用",
			Source:  "wenshu",
			Summary: fmt.Sprintf("wenshu.court.gov.cn 暂时无法访问 (%v)。建议通过浏览器访问 wenshu.court.gov.cn 手动检索相关裁判文书。", err),
			Error:   err.Error(),
		}}
	}

	results := parseWenshuHTML(string(body), query)
	if len(results) == 0 {
		results = append(results, JudicialSearchResult{
			Title:   query + " — 裁判文书检索",
			Source:  "wenshu",
			URL:     searchURL,
			Summary: "未从裁判文书网获取到结构化结果。建议通过浏览器访问 wenshu.court.gov.cn，使用关键词 \"" + query + "\" 手动检索。",
		})
	}

	return results
}

/* parseDataCourtHTML 解析司法大数据服务网 HTML。

   当前简化实现：提取页面标题和关键文本片段。
   后续可接入 LLM 做结构化提取。
*/
func parseDataCourtHTML(html, query string) []JudicialSearchResult {
	var results []JudicialSearchResult

	// 提取标题
	title := extractHTMLTitle(html)
	if title != "" {
		results = append(results, JudicialSearchResult{
			Title:   title,
			Source:  "data_court",
			Summary: extractSummary(html, query, 200),
		})
	}

	return results
}

/* parseWenshuHTML 解析裁判文书网 HTML。

   当前简化实现：提取文书标题和摘要。
   裁判文书网页面结构可能变化，此处做基础解析。
*/
func parseWenshuHTML(html, query string) []JudicialSearchResult {
	var results []JudicialSearchResult

	title := extractHTMLTitle(html)
	if title != "" {
		results = append(results, JudicialSearchResult{
			Title:   title,
			Source:  "wenshu",
			Summary: extractSummary(html, query, 200),
		})
	}

	return results
}

/* httpGet 执行 HTTP GET 请求。 */
func httpGet(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	// 设置浏览器 User-Agent 避免被拦截
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512KB 上限
	if err != nil {
		return nil, err
	}

	return body, nil
}

/* extractHTMLTitle 从 HTML 中提取 <title> 内容。 */
func extractHTMLTitle(html string) string {
	start := strings.Index(html, "<title>")
	if start == -1 {
		return ""
	}
	start += 7
	end := strings.Index(html[start:], "</title>")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

/* extractSummary 从 HTML 中提取包含关键词的文本摘要。

   去除 HTML 标签后，找到包含关键词的上下文片段。
*/
func extractSummary(html, query string, maxLen int) string {
	// 简单去标签
	text := stripHTMLTags(html)
	text = collapseSpaces(text)

	// 截取 500 字内的文本
	runes := []rune(text)
	if len(runes) > 500 {
		text = string(runes[:500])
	}

	// 尝试找到包含关键词的段落
	queryLower := strings.ToLower(query)
	if idx := strings.Index(strings.ToLower(text), queryLower); idx >= 0 {
		start := idx - 30
		if start < 0 {
			start = 0
		}
		end := idx + len(query) + 100
		if end > len(text) {
			end = len(text)
		}
		text = text[start:end]
	}

	runes = []rune(text)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return text
}

/* stripHTMLTags 移除 HTML 标签。 */
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

/* collapseSpaces 合并连续空白字符为一个空格。 */
func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}
