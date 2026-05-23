package bench

import (
	"context"
	"fmt"
	"strings"
)

// ClarifySuite tracks clarify rate and threshold evolution.
func ClarifySuite() Suite {
	return Suite{
		Name: "clarify_behavior",
		Cases: []Case{
			{Name: "unambiguous-case", Prompt: "search for go 1.22 release notes", Expectations: []string{"no error"}},
			{Name: "ambiguous-case", Prompt: "看看苹果怎么样", Expectations: []string{"contains:澄清", "contains:苹果"}},
			{Name: "ambiguous-topic", Prompt: "open a file", Expectations: []string{"no error"}},
		},
	}
}

// FilesystemSuite tests filesystem skill capabilities.
func FilesystemSuite() Suite {
	return Suite{
		Name: "filesystem",
		Cases: []Case{
			{Name: "list-files", Prompt: "列出当前目录的文件", Expectations: []string{"no error"}},
			{Name: "read-file", Prompt: "打开 main.go", Expectations: []string{"no error"}},
			{Name: "find-largest", Prompt: "最大的文件是哪个", Expectations: []string{"no error"}},
			{Name: "missing-path", Prompt: "打开不存在的文件 xyz.txt", Expectations: []string{"no error"}},
		},
	}
}

// SearchSuite tests web search with ambiguity detection.
func SearchSuite() Suite {
	return Suite{
		Name: "search",
		Cases: []Case{
			{Name: "direct-cn", Prompt: "搜索 Go语言 教程", Expectations: []string{"no error"}},
			{Name: "direct-en", Prompt: "search for climate data", Expectations: []string{"no error"}},
			{Name: "empty-query", Prompt: "", Expectations: []string{"no error"}},
			{Name: "ambiguous-brand-cn", Prompt: "苹果公司的股票", Expectations: []string{"no error"}},
			{Name: "ambiguous-brand-en", Prompt: "apple stocks", Expectations: []string{"no error"}},
			{Name: "topic-search", Prompt: "latest AI research papers", Expectations: []string{"no error"}},
			{Name: "local-info", Prompt: "北京天气", Expectations: []string{"no error"}},
		},
	}
}

// RunSuite is a convenience wrapper that runs a suite against a handler.
func RunSuite(ctx context.Context, suite Suite, handler func(ctx context.Context, prompt string) (string, error)) Report {
	runner := NewRunner(handler)
	return runner.RunSuite(ctx, suite)
}

// RunAll runs all built-in suites and returns a combined markdown report.
func RunAll(ctx context.Context, handler func(ctx context.Context, prompt string) (string, error)) string {
	suites := []Suite{ClarifySuite(), FilesystemSuite(), SearchSuite()}
	var reports []string
	for _, s := range suites {
		r := RunSuite(ctx, s, handler)
		reports = append(reports, r.Markdown())
	}
	return fmt.Sprintf("# Bench Report\n\n%s", strings.Join(reports, "\n\n---\n\n"))
}
