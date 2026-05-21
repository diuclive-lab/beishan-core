package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

// ContainsVerifiableClaim 快速判断文本是否包含可核查事实。
func ContainsVerifiableClaim(text string) bool {
	return len(DetectVerifiableClaims(text)) > 0
}
