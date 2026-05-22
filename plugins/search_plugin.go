package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"beishan/internal/llm"
	"beishan/internal/tools"
	"beishan/kernel"
)

type SearchPlugin struct {
	Kernel *kernel.Kernel
}

// extractQueryFromPayload 从搜索 payload 中提取 query 字段。
func extractQueryFromPayload(payload []byte) string {
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return ""
	}
	q, _ := obj["query"].(string)
	return q
}

// tokenize 将文本拆分为小写关键词（中文按字符，英文按空格）。
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var buf strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

// checkSearchRelevance 检查搜索结果是否与查询相关。
// 用 query 关键词与结果标题/描述做重叠检测，返回相关性分数 0.0~1.0。
func checkSearchRelevance(query, resultJSON string) float64 {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return 1.0 // 无法判断时默认相关
	}

	// 去掉太短的 token（单字母英文、单个数字）
	var filtered []string
	for _, t := range queryTokens {
		if len([]rune(t)) >= 2 || (len([]rune(t)) == 1 && unicode.Is(unicode.Han, []rune(t)[0])) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return 1.0
	}

	var output tools.WebSearchOutput
	if err := json.Unmarshal([]byte(resultJSON), &output); err != nil || output.Data == nil {
		return 1.0 // 解析失败时不阻断
	}

	totalHits := 0
	for _, r := range output.Data.Web {
		titleTokens := tokenize(r.Title + " " + r.Description)
		titleSet := make(map[string]bool, len(titleTokens))
		for _, t := range titleTokens {
			titleSet[t] = true
		}
		for _, qt := range filtered {
			if titleSet[qt] {
				totalHits++
				break // 每条结果最多计一次命中
			}
		}
	}

	// 分数 = 命中结果数 / 总结果数
	if len(output.Data.Web) == 0 {
		return 0.0
	}
	return float64(totalHits) / float64(len(output.Data.Web))
}

// ─── Web 搜索 Query 改写 ────────────────────────────

// webVaguePatterns 需要改写的口语化搜索模式
var webVaguePatterns = []string{
	"看看", "帮我搜", "搜一下", "查一下", "找一下",
	"最近一个月", "最近一周", "最近几天", "怎么样",
	"相关资讯", "相关信息", "相关报道",
}

func needsWebQueryRewrite(query string) bool {
	for _, p := range webVaguePatterns {
		if strings.Contains(query, p) {
			return true
		}
	}
	// 查询太长（>30字符）也可能需要精简
	if len([]rune(query)) > 30 {
		return true
	}
	return false
}

func rewriteWebQuery(query string) string {
	prompt := fmt.Sprintf(`将以下用户搜索意图改写为简洁的搜索引擎关键词。
要求：
- 去掉口语化表达（"帮我搜""看看""查一下"）
- 保留核心实体（人名、公司名、产品名、数字）
- 保留时间范围（如有）
- 输出 5-15 个字的搜索关键词
- 不要解释，只输出关键词

用户搜索：%s`, query)

	reply, _, err := llm.ChatCompletionWithUsage([]llm.ChatMessage{
		{Role: "system", Content: "你是搜索关键词优化器。只输出精炼的搜索关键词，不要解释。"},
		{Role: "user", Content: prompt},
	}, 10*time.Second)
	llm.RecordUsage("web_query_rewrite", nil)

	if err != nil || strings.TrimSpace(reply) == "" {
		return query
	}

	rewritten := strings.TrimSpace(reply)
	rewritten = strings.TrimPrefix(rewritten, "```")
	rewritten = strings.TrimSuffix(rewritten, "```")
	rewritten = strings.TrimSpace(rewritten)

	fmt.Printf("[web_query_rewrite] %q → %q\n", query, rewritten)
	return rewritten
}

func (p *SearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "web_search":
		// Query 改写：口语化搜索 → 精确搜索词
		searchPayload := msg.Payload
		originalQuery := extractQueryFromPayload(msg.Payload)
		if originalQuery != "" && needsWebQueryRewrite(originalQuery) {
			if rewritten := rewriteWebQuery(originalQuery); rewritten != originalQuery {
				searchPayload, _ = json.Marshal(map[string]interface{}{"query": rewritten})
			}
		}

		result := tools.ValidateAndExecute("web_search", searchPayload)
		fmt.Printf("[搜索] %s\n", result.Output)

		// 输出质量门禁：检查搜索结果与查询的相关性
		query := extractQueryFromPayload(searchPayload)
		relevance := checkSearchRelevance(query, result.Output)
		if relevance < 0.3 {
			fmt.Printf("[搜索] 相关性低 (%.2f)，query=%s\n", relevance, query)
		}

		// 结果回传 think_plugin 做自然语言总结
		if p.Kernel != nil && result.Success && result.Output != "" {
			prompt := fmt.Sprintf("请用中文总结以下搜索结果：%s", result.Output)
			if relevance < 0.3 {
				prompt = fmt.Sprintf("⚠️ 以下搜索结果与用户查询「%s」的相关性很低（匹配度 %.0f%%），请在总结时明确告知用户结果可能不相关，建议换关键词重搜。\n\n搜索结果：%s",
					query, relevance*100, result.Output)
			}
			summaryPayload, _ := json.Marshal(map[string]string{
				"message": prompt,
				"mode":    "no_retrieval",
			})
			summary, err := p.Kernel.Call(kernel.Message{
				Recipient: "think_plugin",
				Type:      "chat",
				Payload:   summaryPayload,
			}, 30*time.Second)
			if err == nil {
				return summary, nil
			}
			fmt.Printf("[搜索] 总结失败，退回原始结果: %v\n", err)
		}

		// 降级：直接返回原始结果
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: respPayload,
		}, nil

	case "web_fetch", "web_extract", "web_render":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[抓取] %s\n", result.Output)
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: respPayload,
		}, nil

	default:
		return kernel.Message{}, fmt.Errorf("search_plugin: 未知消息类型 %s", msg.Type)
	}
}
