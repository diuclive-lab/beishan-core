package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

/* ─── kb_audit 知识库质量审计 ────────────────────

   扫描全库，生成结构化质量报告。
   确定性代码，不调 LLM。
*/

// AuditResult 审计报告
type AuditResult struct {
	Total           int            `json:"total"`
	Issues          []AuditIssue   `json:"issues"`
	Stats           AuditStats     `json:"stats"`
	DuplicateGroups []DupGroup     `json:"duplicate_groups,omitempty"`
	HealthScore     int            `json:"health_score"` // 0-100
}

// AuditStats 统计数据
type AuditStats struct {
	NoSourceType int            `json:"no_source_type"`
	NoTags       int            `json:"no_tags"`
	NoTypedLinks int            `json:"no_typed_links"`
	NoEmbedding  int            `json:"no_embedding"`
	EmptySummary int            `json:"empty_summary"`
	ShortSummary int            `json:"short_summary"` // < 10 字
	SourceTypes  map[string]int `json:"source_types"`
}

// AuditIssue 单条问题
type AuditIssue struct {
	EntryID  string   `json:"entry_id"`
	Title    string   `json:"title"`
	Problems []string `json:"problems"`
	Severity string   `json:"severity"` // "critical" | "warning" | "info"
}

// DupGroup 重复组
type DupGroup struct {
	Titles   []string `json:"titles"`
	EntryIDs []string `json:"entry_ids"`
	Reason   string   `json:"reason"`
}

// KBaudit 扫描全库，生成质量报告
func KBaudit() *AuditResult {
	entries := loadAllKnowledge()
	result := &AuditResult{
		Total:           len(entries),
		Issues:          []AuditIssue{},
		Stats:           AuditStats{SourceTypes: make(map[string]int)},
		DuplicateGroups: []DupGroup{},
	}

	if len(entries) == 0 {
		result.HealthScore = 100
		return result
	}

	titleIndex := make(map[string][]string) // normalized title → []entryID

	for _, e := range entries {
		var problems []string
		severity := "info"

		// 字段完整性检查
		if e.SourceType == "" {
			problems = append(problems, "缺少 source_type")
			result.Stats.NoSourceType++
			severity = "critical"
		}
		if len(e.Tags) == 0 {
			problems = append(problems, "缺少 tags")
			result.Stats.NoTags++
			if severity != "critical" {
				severity = "warning"
			}
		}
		if len(e.TypedLinks) == 0 {
			result.Stats.NoTypedLinks++
		}
		if len(e.Embedding) == 0 {
			result.Stats.NoEmbedding++
		}
		if e.Summary == "" {
			problems = append(problems, "summary 为空")
			result.Stats.EmptySummary++
			severity = "critical"
		} else if len([]rune(e.Summary)) < 10 {
			problems = append(problems, "summary 过短（<10字）")
			result.Stats.ShortSummary++
			if severity == "info" {
				severity = "warning"
			}
		}

		// 模板变量垃圾检测
		if strings.Contains(e.Title, "${") || strings.Contains(e.Summary, "${") {
			problems = append(problems, "包含未解析的模板变量")
			severity = "critical"
		}

		// source_type 统计
		st := e.SourceType
		if st == "" {
			st = "(empty)"
		}
		result.Stats.SourceTypes[st]++

		// 重复检测（精确标题匹配）
		normalizedTitle := strings.ToLower(strings.TrimSpace(e.Title))
		if normalizedTitle != "" {
			titleIndex[normalizedTitle] = append(titleIndex[normalizedTitle], e.ID)
		}

		if len(problems) > 0 {
			result.Issues = append(result.Issues, AuditIssue{
				EntryID:  e.ID,
				Title:    e.Title,
				Problems: problems,
				Severity: severity,
			})
		}
	}

	// 生成重复组
	for title, ids := range titleIndex {
		if len(ids) > 1 {
			result.DuplicateGroups = append(result.DuplicateGroups, DupGroup{
				Titles:   []string{title},
				EntryIDs: ids,
				Reason:   "exact_title",
			})
		}
	}

	// 健康度评分：100 分起步，按问题扣分
	score := 100
	total := float64(result.Total)
	if total > 0 {
		score -= int(float64(result.Stats.NoSourceType) / total * 30) // 最多扣 30
		score -= int(float64(result.Stats.NoTags) / total * 20)       // 最多扣 20
		score -= int(float64(result.Stats.EmptySummary) / total * 25) // 最多扣 25
		score -= int(float64(result.Stats.NoTypedLinks) / total * 15) // 最多扣 15
		score -= len(result.DuplicateGroups) * 2                      // 每组重复扣 2
	}
	if score < 0 {
		score = 0
	}
	result.HealthScore = score

	return result
}

func registerKBAuditTools() {
	Register("kb_audit", "扫描知识库，生成数据质量报告（source_type/tags/links/embedding 完整性、重复检测、健康度评分）。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			result := KBaudit()
			b, _ := json.MarshalIndent(result, "", "  ")
			return successResult(string(b))
		},
	)

	Register("kb_repair", "修复知识库中的常见问题（补 source_type、tags、typed_links）。dry_run=true 时只报告不修改。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"dry_run": map[string]interface{}{
					"type":        "boolean",
					"description": "是否只报告不修改（默认 true）",
				},
			},
		},
		func(args map[string]interface{}) *ToolResult {
			dryRun := true
			if v, ok := args["dry_run"].(bool); ok {
				dryRun = v
			}
			result := KBrepair(dryRun)
			b, _ := json.MarshalIndent(result, "", "  ")
			return successResult(string(b))
		},
	)
}

/* ─── kb_repair 知识库修复 ──────────────────────── */

// RepairResult 修复结果
type RepairResult struct {
	Scanned  int      `json:"scanned"`
	Repaired int      `json:"repaired"`
	Skipped  int      `json:"skipped"`
	Details  []string `json:"details"`
}

// KBrepair 修复知识库中的常见问题
func KBrepair(dryRun bool) *RepairResult {
	entries := loadAllKnowledge()
	result := &RepairResult{Scanned: len(entries)}

	for _, e := range entries {
		modified := false

		// 1. 修复 source_type
		if e.SourceType == "" {
			inferred := inferSourceType(e)
			e.SourceType = inferred
			result.Details = append(result.Details,
				fmt.Sprintf("%s: source_type → %s", e.ID, inferred))
			modified = true
		}

		// 2. 修复 tags
		if len(e.Tags) == 0 {
			e.Tags = autoExtractTags(e.Title, e.Summary)
			if len(e.Tags) > 0 {
				result.Details = append(result.Details,
					fmt.Sprintf("%s: tags → %v", e.ID, e.Tags))
				modified = true
			}
		}

		// 3. 补 embedding（仅端点可用时自动填充）
		if len(e.Embedding) == 0 && embeddingEnabled() {
			text := e.Title + " " + e.Summary
			if emb, ok := tryEmbedding(text); ok {
				e.Embedding = emb
				result.Details = append(result.Details,
					fmt.Sprintf("%s: embedding +%d dims", e.ID, len(emb)))
				modified = true
			}
		}

		// 4. 补 typed_links
		if len(e.TypedLinks) == 0 && len(e.Tags) > 0 {
			autoLinkEntry(e.ID, e.Title, e.Summary, e.Tags, e.Topics)
			// 重新加载以获取链接结果
			updated := loadKnowledge(e.ID)
			if updated != nil && len(updated.TypedLinks) > 0 {
				e.TypedLinks = updated.TypedLinks
				result.Details = append(result.Details,
					fmt.Sprintf("%s: typed_links +%d", e.ID, len(e.TypedLinks)))
				modified = true
			}
		}

		if modified {
			if !dryRun {
				saveKnowledge(e)
			}
			result.Repaired++
		} else {
			result.Skipped++
		}
	}
	return result
}

// inferSourceType 从条目内容推断 source_type
func inferSourceType(e *KnowledgeEntry) string {
	lower := strings.ToLower(e.Title + " " + e.Summary)

	// 硬件/系统前缀 → code
	if strings.HasPrefix(e.Summary, "【") {
		return "code"
	}
	// 有 URL → web
	if strings.Contains(e.Summary, "http://") || strings.Contains(e.Summary, "https://") {
		return "web"
	}
	// 决策/方案 → codex
	decisionWords := []string{"决策", "决定", "方案", "放弃", "采用", "选择", "架构"}
	for _, w := range decisionWords {
		if strings.Contains(lower, w) {
			return "codex"
		}
	}
	// 代码相关
	codeWords := []string{"function", "struct", "interface", "package", "import", "func ", "def ", "class "}
	for _, w := range codeWords {
		if strings.Contains(lower, w) {
			return "code"
		}
	}
	return "note"
}

// splitTokens 按标点和空格分词（中文友好）
func splitTokens(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' ||
			r == '，' || r == '。' || r == '；' || r == '：' ||
			r == '！' || r == '？' || r == '（' || r == '）' ||
			r == '【' || r == '】' || r == '《' || r == '》' ||
			r == '“' || r == '”' || r == '‘' || r == '’' ||
			r == ',' || r == '.' || r == ';' || r == ':' ||
			r == '!' || r == '?' || r == '(' || r == ')' ||
			r == '[' || r == ']' || r == '{' || r == '}' ||
			r == '/' || r == '\\' || r == '|' || r == '@' ||
			r == '#' || r == '$' || r == '%' || r == '^' ||
			r == '&' || r == '*' || r == '+' || r == '=' ||
			r == '<' || r == '>' || r == '~' || r == '`' ||
			r == '—' || r == '–' || r == '-' || r == '_' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// autoExtractTags 从标题和摘要中提取关键词作为 tags
func autoExtractTags(title, summary string) []string {
	text := title + " " + summary
	var tags []string
	seen := make(map[string]bool)

	// 分词：先按标点/空格拆分，再逐个处理
	words := splitTokens(text)

	for _, word := range words {
		cleaned := strings.TrimSpace(word)
		if len(cleaned) < 2 {
			continue
		}
		lower := strings.ToLower(cleaned)
		if seen[lower] {
			continue
		}
		// 英文：>= 3 字符，排除停用词
		if isASCII(cleaned) && len(cleaned) >= 3 && !isStopWord(lower) {
			seen[lower] = true
			tags = append(tags, lower)
		}
		// 中文：2-8 字的连续中文
		if isChinese(cleaned) && len([]rune(cleaned)) >= 2 && len([]rune(cleaned)) <= 8 {
			seen[lower] = true
			tags = append(tags, cleaned)
		}
	}

	if len(tags) > 5 {
		tags = tags[:5]
	}
	return tags
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func isChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "from": true, "with": true,
	"that": true, "this": true, "are": true, "was": true, "not": true,
	"but": true, "has": true, "have": true, "had": true,
	"been": true, "will": true, "would": true, "could": true, "should": true,
	"can": true, "may": true, "might": true, "shall": true, "must": true,
	"its": true, "his": true, "her": true, "our": true, "your": true,
	"their": true, "they": true, "them": true, "then": true, "than": true,
	"when": true, "where": true, "what": true, "which": true, "who": true,
	"how": true, "all": true, "each": true, "every": true, "both": true,
	"few": true, "more": true, "most": true, "other": true, "some": true,
	"such": true, "only": true, "own": true, "same": true, "also": true,
	"just": true, "about": true, "into": true, "through": true, "during": true,
	"before": true, "after": true, "above": true, "below": true, "between": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}
