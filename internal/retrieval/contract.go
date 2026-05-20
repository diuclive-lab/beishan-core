package retrieval

import (
	"fmt"
	"strings"
)

/* ─── RetrievalKind 检索结果来源 ──────────────── */

type RetrievalKind string

const (
	KindDirect   RetrievalKind = "direct"   // L0 直接匹配
	KindLinked   RetrievalKind = "linked"   // L0.5 图扩展
	KindSemantic RetrievalKind = "semantic" // L2 语义回退
	KindCode     RetrievalKind = "code"     // 代码检索
)

/* ─── ContradictionAnnotation 矛盾标注 ──────────── */

type ContradictionAnnotation struct {
	TargetTitle string `json:"target_title"`
	Reason      string `json:"reason"`
}

/* ─── RetrievalResult 统一检索结果 ──────────────── */

type RetrievalResult struct {
	Source         RetrievalKind            `json:"source"`
	EntryID        string                   `json:"entry_id"`
	Title          string                   `json:"title"`
	Summary        string                   `json:"summary"`
	Tags           []string                 `json:"tags"`
	SourceType     string                   `json:"source_type"` // "knowledge" | "code" | "session"
	Score          int                      `json:"score"`
	LinkType       string                   `json:"link_type,omitempty"` // "related" | "contradicts" | ...
	LinkFrom       string                   `json:"link_from,omitempty"`
	Contradictions []ContradictionAnnotation `json:"contradictions,omitempty"`

	// Code-specific fields
	FilePath   string `json:"file_path,omitempty"`   // 代码文件路径
	LineNumber int    `json:"line_number,omitempty"` // 行号
	Language   string `json:"language,omitempty"`     // 语言
}

/* ─── FormatForPromptFull 统一渲染 ──────────────── */

// FormatForPromptFull 将 RetrievalResult 格式化为带来源标注的 <background> 文本。
// 标注：⚠️ 矛盾 / 📎 演进 / 🔗 关联 / 🔍 语义 / 📁 代码
func FormatForPromptFull(results []RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString("<background>\n")
	for i, r := range results {
		tags := strings.Join(r.Tags, ", ")
		if tags == "" {
			tags = r.SourceType
		}
		summary := r.Summary
		runes := []rune(summary)
		if len(runes) > 100 {
			summary = string(runes[:100]) + "..."
		}
		// 来源标注
		prefix := ""
		switch r.Source {
		case KindLinked:
			switch r.LinkType {
			case "contradicts":
				prefix = "⚠️ "
			case "supersedes":
				prefix = "📎 "
			default:
				prefix = "🔗 "
			}
		case KindSemantic:
			prefix = "🔍 "
		case KindCode:
			prefix = "📁 "
		}
		// 代码结果用文件路径作为标题
		if r.Source == KindCode && r.FilePath != "" {
			title := fmt.Sprintf("%s:%d", r.FilePath, r.LineNumber)
			sb.WriteString(fmt.Sprintf("%d. %s%s\n", i+1, prefix, title))
			sb.WriteString(fmt.Sprintf("   %s\n", summary))
		} else {
			sb.WriteString(fmt.Sprintf("%d. %s%s（%s）\n", i+1, prefix, r.Title, tags))
			sb.WriteString(fmt.Sprintf("   %s\n", summary))
		}
		// 矛盾标注
		for _, c := range r.Contradictions {
			reason := c.Reason
			if reason == "" {
				reason = "存在矛盾观点"
			}
			sb.WriteString(fmt.Sprintf("   ⚠️ 矛盾: \"%s\" — %s\n", c.TargetTitle, reason))
		}
	}
	sb.WriteString("</background>")
	return sb.String()
}
