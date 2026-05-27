// Package retrieval — 检索层：查询 DSL + 结果合同 + 搜索策略。
//
// ParseQuery 支持结构化字段查询语法。调用方无需感知解析器是否激活，
// Query 结构的零值字段自动被 SearchWithQuery 跳过。
package retrieval

import "strings"

// Query 是结构化检索查询。
// 零值表示"无此筛选条件"，仅执行关键词搜索。
type Query struct {
	Raw      string   // 原始输入（保留完整查询字符串）
	Keywords []string // 关键词（去掉了 field: 前缀的剩余部分）

	Tags       []string // tag: 标签筛选
	Types      []string // type: / source: 来源类型筛选
	Namespace  string   // namespace: 命名空间筛选
	DateAfter  string   // date:> 起始日期（含）
	DateBefore string   // date:< 截止日期（含）
	Status     string   // status: 状态筛选
}

// ParseQuery 解析查询字符串为 Query 结构。
//
// 支持字段语法（可出现在输入任意位置）：
//
//	tag:go           → Tags=["go"]（可重复）
//	type:decision    → Types=["decision"]（type:/source: 别名，可重复）
//	source:note      → Types=["note"]
//	namespace:hermes → Namespace="hermes"
//	date:>2026-04    → DateAfter="2026-04"
//	date:<2026-05-25 → DateBefore="2026-05-25"
//	status:archived  → Status="archived"
//
// 不匹配任何字段前缀的文本段合并为 Keywords。
func ParseQuery(input string) *Query {
	q := &Query{Raw: input}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return q
	}

	tokens := strings.Fields(trimmed)
	var rest []string

	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "tag:"):
			if v := tok[4:]; v != "" {
				q.Tags = append(q.Tags, v)
			}
		case strings.HasPrefix(tok, "type:"):
			if v := tok[5:]; v != "" {
				q.Types = append(q.Types, v)
			}
		case strings.HasPrefix(tok, "source:"):
			if v := tok[7:]; v != "" {
				q.Types = append(q.Types, v)
			}
		case strings.HasPrefix(tok, "namespace:"):
			if v := tok[10:]; v != "" {
				q.Namespace = v
			}
		case strings.HasPrefix(tok, "date:>"):
			if v := tok[6:]; v != "" {
				q.DateAfter = v
			}
		case strings.HasPrefix(tok, "date:<"):
			if v := tok[6:]; v != "" {
				q.DateBefore = v
			}
		case strings.HasPrefix(tok, "status:"):
			if v := tok[7:]; v != "" {
				q.Status = v
			}
		default:
			rest = append(rest, tok)
		}
	}

	if len(rest) > 0 {
		q.Keywords = []string{strings.Join(rest, " ")}
	}
	return q
}

// HasFieldFilters 检查查询是否包含结构化筛选条件。
// 解析器未激活时始终返回 false。
func (q *Query) HasFieldFilters() bool {
	return len(q.Tags) > 0 || len(q.Types) > 0 ||
		q.Namespace != "" || q.DateAfter != "" ||
		q.DateBefore != "" || q.Status != ""
}

// IsEmpty 检查查询是否为空。
func (q *Query) IsEmpty() bool {
	return q.Raw == ""
}
