package plugins

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

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

// RunFullRetrieval 执行完整检索（三柱分流：Episodic / Semantic / External）。
func RunFullRetrieval(query string, projectPath string) ([]retrieval.RetrievalResult, *tools.RetrievalTrace) {
	trace := tools.NewRetrievalTrace(query)
	intent := classifyIntent(query)

	var results []retrieval.RetrievalResult

	switch intent {
	case IntentEpisodic:
		// "我们之前讨论过..." → 情景优先，语义补充
		episodic := RunEpisodicRetrieval(query, 3, trace)
		results = append(results, episodic...)
		semantic, _ := RunKnowledgeRetrieval(query, 1)
		results = append(results, semantic...)

	case IntentSemantic:
		// "决策是什么" "结论" → 语义优先
		semantic, _ := RunKnowledgeRetrieval(query, 3)
		results = append(results, semantic...)

	case IntentCode:
		// "代码" "函数" → 代码优先，语义补充
		if projectPath != "" {
			code := RunCodeRetrieval(query, projectPath, 3, trace)
			results = append(results, code...)
		}
		semantic, _ := RunKnowledgeRetrieval(query, 1)
		results = append(results, semantic...)

	default: // IntentMixed
		// 无法判断 → 三路各取配额
		semantic, _ := RunKnowledgeRetrieval(query, 2)
		results = append(results, semantic...)
		episodic := RunEpisodicRetrieval(query, 1, trace)
		results = append(results, episodic...)
		if looksLikeCodeQuestion(query) && projectPath != "" {
			code := RunCodeRetrieval(query, projectPath, 1, trace)
			results = append(results, code...)
		}
	}

	// 总上限 5 条
	if len(results) > 5 {
		results = results[:5]
	}

	// 记录意图到 trace
	trace.Add(tools.RetrievalStage{
		Stage:  "intent",
		Method: "keyword_classify",
		Input:  query,
		Output: map[string]any{"intent": string(intent), "total": len(results)},
	})

	fmt.Printf("[retrieval] intent=%s results=%d\n", intent, len(results))
	return results, trace
}

// ── 代码检索管道 ─────────────────────────────────

// codeQuestionKeywords 代码问题检测关键词
var codeQuestionKeywords = []string{
	"代码", "函数", "实现", "源码", "调用", "定义在哪",
	"怎么实现", "code", "func", "implementation", "where",
	"怎么写的", "在哪", "什么方法",
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

/* ─── 意图分类（确定性，零 LLM）─────────────────────── */

// QueryIntent 查询意图
type QueryIntent string

const (
	IntentEpisodic QueryIntent = "episodic" // "之前讨论过" "上次说的"
	IntentSemantic QueryIntent = "semantic" // "决策" "结论" "方案"
	IntentCode     QueryIntent = "code"     // "代码" "函数" "实现"
	IntentMixed    QueryIntent = "mixed"    // 无法判断，混合检索
)

// episodicKeywords 情景记忆触发词
var episodicKeywords = []string{
	"之前", "上次", "讨论过", "聊过", "历史", "记得",
	"什么时候", "几月", "当时", "那次", "曾经", "过去",
	"还记得", "说过", "提到过", "刚才",
}

// semanticKeywords 语义知识触发词（decision/conclusion oriented）
var semanticKeywords = []string{
	"决策", "决定", "结论", "方案", "教训", "原则",
	"为什么放弃", "最终", "确定", "选择了", "机制",
	"架构", "流程", "设计", "放弃", "为什么", "标准", "规则",
}

// classifyIntent 确定性意图分类
func classifyIntent(text string) QueryIntent {
	lower := strings.ToLower(text)

	epiScore := 0
	semScore := 0
	codeScore := 0

	for _, kw := range episodicKeywords {
		if strings.Contains(lower, kw) {
			epiScore++
		}
	}
	for _, kw := range semanticKeywords {
		if strings.Contains(lower, kw) {
			semScore++
		}
	}
	for _, kw := range codeQuestionKeywords {
		if strings.Contains(lower, kw) {
			codeScore++
		}
	}

	maxScore := epiScore
	if semScore > maxScore {
		maxScore = semScore
	}
	if codeScore > maxScore {
		maxScore = codeScore
	}
	if maxScore == 0 {
		return IntentMixed
	}

	switch {
	case codeScore == maxScore && codeScore > 0:
		return IntentCode
	case epiScore > semScore:
		return IntentEpisodic
	case semScore > epiScore:
		return IntentSemantic
	default:
		return IntentMixed
	}
}

/* ─── Episodic Retrieval 管道 ──────────────────────── */

// RunEpisodicRetrieval 情景记忆检索管道
// 搜索会话历史，按时间倒序 + recency 加权
func RunEpisodicRetrieval(query string, limit int, trace *tools.RetrievalTrace) []retrieval.RetrievalResult {
	matches := tools.SessionSearchStructured(query, limit*2)

	if len(matches) == 0 {
		return nil
	}

	var results []retrieval.RetrievalResult
	seen := make(map[string]bool) // 按 session 去重（每 session 最多 1 条）

	now := time.Now().Unix()
	for _, m := range matches {
		if seen[m.SessionID] {
			continue
		}
		seen[m.SessionID] = true

		// Recency decay: 7天内满分，每多一天 -1 分
		ageDays := int((now - m.Timestamp) / 86400)
		recencyScore := 5 - ageDays
		if recencyScore < 1 {
			recencyScore = 1
		}

		ts := time.Unix(m.Timestamp, 0).Format("01-02 15:04")

		results = append(results, retrieval.RetrievalResult{
			Source:     retrieval.KindEpisodic,
			EntryID:    m.SessionID,
			Title:      fmt.Sprintf("%s 对话 — %s", ts, m.Role),
			Summary:    truncateStr(m.Payload, 120),
			SourceType: "session",
			Score:      recencyScore,
		})

		if len(results) >= limit {
			break
		}
	}

	if trace != nil {
		trace.Add(tools.RetrievalStage{
			Stage:  "L0_episodic",
			Method: "session_search",
			Input:  query,
			Output: map[string]any{"matches": len(matches), "sessions": len(results)},
		})
	}

	return results
}

func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
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
