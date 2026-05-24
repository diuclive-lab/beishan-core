package bench

import (
	"context"
	"testing"
)

func TestClarifySuite(t *testing.T) {
	s := ClarifySuite()
	if s.Name != "clarify_behavior" { t.Fatalf("name=%q", s.Name) }
	if len(s.Cases) == 0 { t.Fatal("expected cases") }
}

func TestFilesystemSuite(t *testing.T) {
	s := FilesystemSuite()
	if len(s.Cases) == 0 { t.Fatal("expected cases") }
}

func TestSearchSuite(t *testing.T) {
	s := SearchSuite()
	if len(s.Cases) == 0 { t.Fatal("expected cases") }
}

func TestRunSuite(t *testing.T) {
	handler := func(ctx context.Context, prompt string) (string, error) { return "ok", nil }
	r := RunSuite(context.Background(), ClarifySuite(), handler)
	if len(r.Results) == 0 { t.Fatal("expected results") }
	if r.SuiteName == "" { t.Fatal("expected report name") }
}

func TestRunAll(t *testing.T) {
	handler := func(ctx context.Context, prompt string) (string, error) { return "ok", nil }
	report := RunAll(context.Background(), handler)
	if len(report) == 0 { t.Fatal("expected report") }
}
