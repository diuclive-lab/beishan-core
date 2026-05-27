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

func TestParseQuery_FieldTag(t *testing.T) {
	q := ParseQuery("tag:go type:decision 路由方案")
	if len(q.Tags) != 1 || q.Tags[0] != "go" {
		t.Fatalf("expected Tags=[go], got %v", q.Tags)
	}
	if len(q.Types) != 1 || q.Types[0] != "decision" {
		t.Fatalf("expected Types=[decision], got %v", q.Types)
	}
	if len(q.Keywords) != 1 || q.Keywords[0] != "路由方案" {
		t.Fatalf("expected Keywords=[路由方案], got %v", q.Keywords)
	}
	if !q.HasFieldFilters() {
		t.Fatal("expected HasFieldFilters=true when tags/types are set")
	}
}

func TestParseQuery_SourceAlias(t *testing.T) {
	q := ParseQuery("source:note tag:go tag:rust")
	if len(q.Types) != 1 || q.Types[0] != "note" {
		t.Fatalf("expected Types=[note], got %v", q.Types)
	}
	if len(q.Tags) != 2 || q.Tags[0] != "go" || q.Tags[1] != "rust" {
		t.Fatalf("expected Tags=[go rust], got %v", q.Tags)
	}
}

func TestParseQuery_DateRange(t *testing.T) {
	q := ParseQuery("date:>2026-04 date:<2026-05-25 架构决策")
	if q.DateAfter != "2026-04" {
		t.Fatalf("expected DateAfter=2026-04, got %q", q.DateAfter)
	}
	if q.DateBefore != "2026-05-25" {
		t.Fatalf("expected DateBefore=2026-05-25, got %q", q.DateBefore)
	}
	if len(q.Keywords) != 1 || q.Keywords[0] != "架构决策" {
		t.Fatalf("expected Keywords=[架构决策], got %v", q.Keywords)
	}
}

func TestParseQuery_Namespace(t *testing.T) {
	q := ParseQuery("namespace:hermes 路由方案")
	if q.Namespace != "hermes" {
		t.Fatalf("expected Namespace=hermes, got %q", q.Namespace)
	}
	if len(q.Keywords) != 1 || q.Keywords[0] != "路由方案" {
		t.Fatalf("expected Keywords=[路由方案], got %v", q.Keywords)
	}
}

func TestParseQuery_Status(t *testing.T) {
	q := ParseQuery("status:archived")
	if q.Status != "archived" {
		t.Fatalf("expected Status=archived, got %q", q.Status)
	}
	if len(q.Keywords) != 0 {
		t.Fatal("expected no keywords for pure-field query")
	}
}

func TestParseQuery_AllFieldsMixed(t *testing.T) {
	q := ParseQuery("date:>2026-01 tag:go namespace:beishan type:decision 并发模型 type:lesson")
	if q.DateAfter != "2026-01" {
		t.Fatalf("DateAfter=%q", q.DateAfter)
	}
	if len(q.Tags) != 1 || q.Tags[0] != "go" {
		t.Fatalf("Tags=%v", q.Tags)
	}
	if q.Namespace != "beishan" {
		t.Fatalf("Namespace=%q", q.Namespace)
	}
	if len(q.Types) != 2 || q.Types[0] != "decision" || q.Types[1] != "lesson" {
		t.Fatalf("Types=%v", q.Types)
	}
	if len(q.Keywords) != 1 || q.Keywords[0] != "并发模型" {
		t.Fatalf("Keywords=%v", q.Keywords)
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
