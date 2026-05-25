package retrieval

import (
	"testing"
)

func TestParseQuery_Empty(t *testing.T) {
	q := ParseQuery("")
	if !q.IsEmpty() {
		t.Fatal("expected empty query for empty input")
	}
	if len(q.Keywords) != 0 {
		t.Fatal("expected no keywords for empty input")
	}
}

func TestParseQuery_RawKeyword(t *testing.T) {
	q := ParseQuery("go 架构 路由方案")
	if q.Raw != "go 架构 路由方案" {
		t.Fatalf("expected raw 'go 架构 路由方案', got %q", q.Raw)
	}
	if len(q.Keywords) != 1 || q.Keywords[0] != "go 架构 路由方案" {
		t.Fatalf("expected keyword 'go 架构 路由方案', got %v", q.Keywords)
	}
}

func TestParseQuery_HasFieldFilters_NotImplemented(t *testing.T) {
	// 字段解析器未实现时，所有 field: 前缀被当作关键词的一部分
	q := ParseQuery("tag:go type:decision")
	if q.HasFieldFilters() {
		t.Fatal("expected false — field parser not yet implemented")
	}
}

func TestParseQuery_HasFieldFilters_True(t *testing.T) {
	// 显式设置字段值时 HasFieldFilters 应返回 true
	q := ParseQuery("test")
	q.Tags = []string{"go"}
	if !q.HasFieldFilters() {
		t.Fatal("expected true when Tags is set explicitly")
	}
}

func TestParseQuery_Trimmed(t *testing.T) {
	q := ParseQuery("  hello world  ")
	if q.Raw != "  hello world  " {
		t.Fatalf("expected raw to preserve original whitespace")
	}
	if q.Keywords[0] != "hello world" {
		t.Fatalf("expected trimmed keyword")
	}
}
