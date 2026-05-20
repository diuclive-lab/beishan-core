package plugins

import (
	"fmt"
	"path/filepath"
	"strings"

	"beishan/internal/retrieval"
	"beishan/internal/tools"
)

/* ─── RetrievalPipe 检索管道（L4-pipe 角色）────────── */

// RunKnowledgeRetrieval 执行知识检索管道（multi-hop）。
// 确定性编排，全程零 LLM。
func RunKnowledgeRetrieval(query string, limit int) ([]retrieval.RetrievalResult, *tools.RetrievalTrace) {
	trace := tools.NewRetrievalTrace(query)

	// Round 1: 确定性检索
	results := tools.SearchMemoryFull(query, limit, trace)

	// Multi-hop decision（纯代码，零 LLM）
	if shouldHop, reason := tools.NeedsSecondHop(results); shouldHop {
		secondQuery := tools.DeriveSecondQuery(results, reason)
		if secondQuery != "" {
			more := tools.SearchMemoryFull(secondQuery, 2, trace)
			results = tools.MergeResults(results, more)
			trace.Add(tools.RetrievalStage{
				Stage:  "multi_hop",
				Method: "code_decision",
				Input:  reason,
				Output: map[string]any{"query": secondQuery, "merged": len(results)},
				Reason: fmt.Sprintf("trigger=%s", reason),
			})
			fmt.Printf("[retrieval] 多跳检索: reason=%s query=%q → 合并后 %d 条\n", reason, secondQuery, len(results))
		}
	}

	return results, trace
}

// RunFullRetrieval 执行完整检索（知识 + 代码）。
func RunFullRetrieval(query string, projectPath string) ([]retrieval.RetrievalResult, *tools.RetrievalTrace) {
	results, trace := RunKnowledgeRetrieval(query, 3)

	// 代码检索：当检测到代码问题且有项目路径时
	if looksLikeCodeQuestion(query) && projectPath != "" {
		codeResults := RunCodeRetrieval(query, projectPath, 2, trace)
		results = append(results, codeResults...)
		// 总上限 5 条
		if len(results) > 5 {
			results = results[:5]
		}
	}

	return results, trace
}

// ── 代码检索管道 ─────────────────────────────────

// codeQuestionKeywords 代码问题检测关键词
var codeQuestionKeywords = []string{
	"代码", "函数", "实现", "源码", "调用", "定义在哪",
	"怎么实现", "code", "func", "implementation", "where",
}

// looksLikeCodeQuestion 确定性检测是否是代码问题
func looksLikeCodeQuestion(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range codeQuestionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// extractCodeTerms 从查询中提取代码相关关键词（英文标识符）
func extractCodeTerms(query string) []string {
	var terms []string
	words := strings.Fields(query)
	for _, w := range words {
		// 提取英文单词（至少 3 个字符）
		cleaned := strings.Trim(w, ".,;:!?()[]{}\"'")
		if len(cleaned) >= 3 && isASCII(cleaned) {
			terms = append(terms, cleaned)
		}
	}
	// 去重
	seen := make(map[string]bool)
	var result []string
	for _, t := range terms {
		lower := strings.ToLower(t)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, t)
		}
	}
	return result
}

// isASCII 检查字符串是否只包含 ASCII 字符
func isASCII(s string) bool {
	for _, c := range s {
		if c > 127 {
			return false
		}
	}
	return true
}

// RunCodeRetrieval 代码检索管道
func RunCodeRetrieval(query, projectPath string, limit int, trace *tools.RetrievalTrace) []retrieval.RetrievalResult {
	if projectPath == "" {
		return nil
	}

	// 提取代码相关关键词（从查询中提取英文标识符）
	searchTerms := extractCodeTerms(query)
	if len(searchTerms) == 0 {
		return nil
	}

	// L0: grep（用提取的关键词搜索）
	var allMatches []tools.GrepMatch
	for _, term := range searchTerms {
		matches, err := tools.CodeGrep(term, projectPath, []string{"*.go"}, limit*2)
		if err == nil {
			allMatches = append(allMatches, matches...)
		}
	}

	if len(allMatches) == 0 {
		return nil
	}

	var results []retrieval.RetrievalResult
	seen := make(map[string]bool) // 按文件去重

	for _, m := range allMatches {
		if seen[m.File] {
			continue
		}
		seen[m.File] = true

		// 提取语言
		lang := strings.TrimPrefix(filepath.Ext(m.File), ".")

		results = append(results, retrieval.RetrievalResult{
			Source:     retrieval.KindCode,
			Title:      fmt.Sprintf("%s:%d", filepath.Base(m.File), m.Line),
			Summary:    m.Content,
			SourceType: "code",
			Score:      3, // grep 命中权重
			FilePath:   m.File,
			LineNumber: m.Line,
			Language:   lang,
		})

		if len(results) >= limit {
			break
		}
	}

	if trace != nil {
		trace.Add(tools.RetrievalStage{
			Stage:  "L0_code_grep",
			Method: "ripgrep",
			Input:  query,
			Output: map[string]any{"matches": len(allMatches), "files": len(results)},
		})
	}

	return results
}
