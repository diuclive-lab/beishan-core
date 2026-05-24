package retrieval

import "testing"

func TestFormatForPromptEmpty(t *testing.T) {
	r := FormatForPromptFull(nil)
	if r != "" { t.Fatalf("expected empty, got %q", r) }
}

func TestFormatForPromptFiltersLowScore(t *testing.T) {
	results := []RetrievalResult{
		{Title: "high", Score: 80},
		{Title: "low", Score: 5},
		{Title: "medium", Score: 30},
	}
	r := FormatForPromptFull(results)
	if len(r) == 0 { t.Fatal("expected non-empty with high/medium") }
}

func TestFormatForPromptNonEmpty(t *testing.T) {
	results := []RetrievalResult{{Title: "t1", Score: 80}}
	r := FormatForPromptFull(results)
	if r == "" { t.Fatal("expected non-empty") }
}
