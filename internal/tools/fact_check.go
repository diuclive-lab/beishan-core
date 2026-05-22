package tools

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/* ─── FactCheck 可核查事实的检测和验证 ──────────

   写入记忆前，检测 claim 中是否包含可核查的系统事实，
   用已有工具实际计算来验证，避免"用户说什么就记什么"。

   使用场景：
   - source_type=fact 时自动触发
   - confirmPendingRemember 用户确认后触发
*/

type FactCheckResult struct {
	Status string `json:"status"` // verified / unverified / contradicted
	Reason string `json:"reason"` // 核查说明
	Actual string `json:"actual,omitempty"` // 实际值（核查通过时）
}

// DetectVerifiableClaims 检测文本中是否包含可核查的系统事实。
// 返回检测到的 claim 描述列表。
func DetectVerifiableClaims(text string) []string {
	var claims []string
	lower := strings.ToLower(text)

	// 知识库条数："N条"、"N条数据"、"N个条目"
	if match := extractNumber(lower, "条"); match > 0 {
		claims = append(claims, fmt.Sprintf("知识库条目数: %d", match))
	} else if match := extractNumber(lower, "个条目"); match > 0 {
		claims = append(claims, fmt.Sprintf("知识库条目数: %d", match))
	}

	// 健康度分数："健康度N分"、"健康度N"
	if match := extractNumberAfter(lower, "健康度"); match > 0 {
		claims = append(claims, fmt.Sprintf("健康度分数: %d", match))
	}

	return claims
}

// FactCheck 核查一条 claim 是否与系统实际状态一致。
// 返回 verified（一致）/ contradicted（矛盾）/ unverified（不可核查）。
func FactCheck(claim string) FactCheckResult {
	lower := strings.ToLower(claim)

	// 知识库条目数核查
	if n := extractNumberAfter(lower, "条目数"); n > 0 || extractNumberAfter(lower, "条") > 0 {
		actual := countKnowledgeEntries()
		claimed := n
		if claimed <= 0 {
			claimed = extractNumberAfter(lower, "条")
		}
		if claimed > 0 && actual != claimed {
			return FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("知识库实际有 %d 条条目，不是 %d 条", actual, claimed),
				Actual: fmt.Sprintf("%d", actual),
			}
		}
		return FactCheckResult{
			Status: "verified",
			Reason: fmt.Sprintf("知识库条目数确认: %d 条", actual),
			Actual: fmt.Sprintf("%d", actual),
		}
	}

	// 健康度分数核查
	if match := extractNumberAfter(lower, "健康度"); match > 0 {
		audit := KBaudit()
		actual := audit.HealthScore
		claimed := match
		if actual != claimed {
			return FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("知识库健康度实际为 %d 分，不是 %d 分", actual, claimed),
				Actual: fmt.Sprintf("%d", actual),
			}
		}
		return FactCheckResult{
			Status: "verified",
			Reason: fmt.Sprintf("健康度确认: %d 分", actual),
			Actual: fmt.Sprintf("%d", actual),
		}
	}

	return FactCheckResult{Status: "unverified", Reason: "无法通过系统工具核查"}
}

// countKnowledgeEntries 统计当前知识库实际条目数。
func countKnowledgeEntries() int {
	return len(loadAllKnowledge())
}

// extractNumber 从文本中提取关键词前或后的数字。
func extractNumber(text, keyword string) int {
	// 尝试 "N条" 格式
	re := regexp.MustCompile(`(\d+)\s*` + regexp.QuoteMeta(keyword))
	if m := re.FindStringSubmatch(text); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// extractNumberAfter 提取关键词后面的数字（如"健康度96"→96）。
func extractNumberAfter(text, keyword string) int {
	idx := strings.Index(text, keyword)
	if idx < 0 {
		return 0
	}
	after := text[idx+len(keyword):]
	re := regexp.MustCompile(`^(\d+)`)
	if m := re.FindStringSubmatch(after); len(m) > 1 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// DateVerify 检测文本中的日期格式并验证合理性。
func DateVerify(text string) []FactCheckResult {
	var results []FactCheckResult
	// 匹配 YYYY-MM-DD 和 YYYY年MM月DD日
	re := regexp.MustCompile(`(\d{4})[-/年](\d{1,2})[-/月](\d{1,2})日?`)
	matches := re.FindAllStringSubmatch(text, -1)
	now := time.Now()

	for _, m := range matches {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])

		if month < 1 || month > 12 {
			results = append(results, FactCheckResult{Status: "contradicted", Reason: fmt.Sprintf("月份 %d 不存在", month)})
			continue
		}
		if day < 1 || day > 31 {
			results = append(results, FactCheckResult{Status: "contradicted", Reason: fmt.Sprintf("日期 %d 不存在", day)})
			continue
		}
		// 检查日期是否在合理范围内（当前年份 ±5 年）
		if year < now.Year()-5 || year > now.Year()+1 {
			results = append(results, FactCheckResult{Status: "contradicted", Reason: fmt.Sprintf("年份 %d 超出合理范围", year), Actual: fmt.Sprintf("%d", now.Year())})
		}
	}
	return results
}

// ContainsVerifiableClaim 快速判断文本是否包含可核查事实。
func ContainsVerifiableClaim(text string) bool {
	return len(DetectVerifiableClaims(text)) > 0
}

// NumberRangeVerify 检测文本中的不合理数值。
// 百分比 >100% 或 <0%、负数金额、年龄 >150 等。
func NumberRangeVerify(text string) []FactCheckResult {
	var results []FactCheckResult

	// 百分比检测：匹配 "N%" 或 "N percent"
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		pct, _ := strconv.ParseFloat(m[1], 64)
		if pct > 100 {
			results = append(results, FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("百分比 %.1f%% 超过 100%%，可能有误", pct),
				Actual: "≤100%",
			})
		} else if pct < 0 {
			results = append(results, FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("百分比 %.1f%% 为负数，可能有误", pct),
				Actual: "≥0%",
			})
		}
	}

	// 年龄检测：匹配 "N岁"
	ageRe := regexp.MustCompile(`(\d+)\s*岁`)
	for _, m := range ageRe.FindAllStringSubmatch(text, -1) {
		age, _ := strconv.Atoi(m[1])
		if age > 150 {
			results = append(results, FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("年龄 %d 岁超过人类寿命极限", age),
				Actual: "≤150",
			})
		}
	}

	return results
}

// URLVerify 检测文本中的 URL 格式是否合法。
func URLVerify(text string) []FactCheckResult {
	var results []FactCheckResult
	// 匹配 http/https URL
	re := regexp.MustCompile(`https?://[^\s\)\]\"'<>]+`)
	matches := re.FindAllString(text, -1)
	for _, u := range matches {
		// 检查常见错误：缺少域名后缀、localhost 混入生产文本
		if strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") {
			results = append(results, FactCheckResult{
				Status: "contradicted",
				Reason: fmt.Sprintf("URL 包含 localhost，不应出现在正式文本中: %s", u),
			})
		}
		// 检查域名是否有后缀
		parsed, err := url.Parse(u)
		if err == nil {
			host := parsed.Hostname()
			if host != "" && !strings.Contains(host, ".") && host != "localhost" {
				results = append(results, FactCheckResult{
					Status: "contradicted",
					Reason: fmt.Sprintf("URL 域名缺少后缀: %s", host),
				})
			}
		}
	}
	return results
}

// StockCodeVerify 检测文本中的股票代码并用 stock_quote 验证。
// 匹配 6 位数字，调接口确认，只返回 contradicted 结果。
func StockCodeVerify(text string) []FactCheckResult {
	var results []FactCheckResult
	re := regexp.MustCompile(`(\d{6})`)
	matches := re.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)

	for _, m := range matches {
		code := m[1]
		if seen[code] {
			continue
		}
		seen[code] = true

		// 过滤明显不是股票代码的 6 位数字（如日期 202605）
		if !strings.HasPrefix(code, "6") && !strings.HasPrefix(code, "0") && !strings.HasPrefix(code, "3") {
			continue
		}

		quote, err := fetchStockQuote(code)
		if err != nil {
			continue
		}

		// 检查代码前后的文本是否包含与名称不匹配的公司名
		idx := strings.Index(text, code)
		if idx < 0 {
			continue
		}
		// 取代码前 20 个字符作为上下文
		start := idx - 20
		if start < 0 {
			start = 0
		}
		ctx := text[start : idx+6]
		// 如果上下文包含"移动"但名称不含"移动"，或反义
		claimedKeywords := extractStockKW(ctx, code)
		if len(claimedKeywords) > 0 {
			matched := false
			for _, kw := range claimedKeywords {
				if strings.Contains(quote.Name, kw) {
					matched = true
					break
				}
			}
			if !matched {
				results = append(results, FactCheckResult{
					Status: "contradicted",
					Reason: fmt.Sprintf("股票代码 %s 对应的是「%s」", code, quote.Name),
					Actual: fmt.Sprintf("%s（%s）", quote.Name, code),
				})
			}
		}
	}
	return results
}

// extractStockKW 从股票代码上下文中提取公司关键词。
func extractStockKW(ctx, code string) []string {
	ctx = strings.ReplaceAll(ctx, code, "")
	// 去掉标点和空格
	for _, r := range []string{"（", "）", "(", ")", " ", ",", "，", "、" } {
		ctx = strings.ReplaceAll(ctx, r, "")
	}
	if len([]rune(ctx)) < 2 {
		return nil
	}
	// 取最后 2-6 个字符作为关键词
	runes := []rune(ctx)
	start := len(runes) - 6
	if start < 0 {
		start = 0
	}
	kw := string(runes[start:])
	if len([]rune(kw)) >= 2 {
		return []string{kw}
	}
	return nil
}

