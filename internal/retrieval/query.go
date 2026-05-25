// Package retrieval — 检索层：查询 DSL + 结果合同 + 搜索策略。
//
// 查询 DSL 是余量设计（margin-of-safety）。
// ParseQuery 当前仅提取原始关键词，不做结构化字段解析。
// 字段筛选功能（tag:/type:/date:> 等）待知识库增长到 2000+ 条后激活。
package retrieval

import "strings"

/* ─── Query DSL 查询结构 ─────────────────────────

   当前阶段：ParseQuery 只提取原始关键词，所有结构化字段忽略。
   设计为余量接口，后续激活不需修改调用方。

   查询语法（规划，未实现）：
     "tag:go type:decision date:>2026-04"    → Tags=["go"], Types=["decision"], DateAfter="2026-04"
     "namespace:hermes 路由方案"              → Namespace="hermes", Keywords=["路由方案"]
     "status:archived"                        → Status="archived"
     date:>2026-05-01
     date:<2026-05-25                         → DateBefore 纯文本字符串，不解析为 time.Time
*/

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
// 当前实现：将所有输入作为原始关键词，不提取字段值。
// 这是余量接口——调用方不感知解析器是否激活。
//
// 待实现：字段解析器按顺序扫描输入，提取 field:value 对，
// 剩余部分作为 Keywords。格式：field:value 或 field:>value。
func ParseQuery(input string) *Query {
	q := &Query{Raw: input}

	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return q
	}

	// TODO: 解析 tag:/type:/namespace:/date:>/date:</status: 字段前缀
	// 当前仅提取原始关键词，不做结构化解析
	q.Keywords = []string{trimmed}

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
