package observatory

import "testing"

func TestClassifyEmpty(t *testing.T) {
	ft := Classify(nil)
	if ft.Severity != SeverityNone {
		t.Fatalf("severity=%q", ft.Severity)
	}
}

func TestClassifySidecarError(t *testing.T) {
	ft := Classify(map[string]float64{"sidecar_error_count": 1})
	if !ft.HasFlag(FlagSidecarError) {
		t.Fatal("expected sidecar_error flag")
	}
	if ft.Severity != SeverityError {
		t.Fatalf("severity=%q", ft.Severity)
	}
}

func TestClassifyCitationWarning(t *testing.T) {
	ft := Classify(map[string]float64{"citation_url_count": 5, "citation_unmatched_count": 2})
	if !ft.HasFlag(FlagCitationUnmatched) {
		t.Fatal("expected citation_unmatched flag")
	}
	if ft.Severity != SeverityWarning {
		t.Fatalf("severity=%q", ft.Severity)
	}
}

func TestClassifyDoomLoop(t *testing.T) {
	ft := Classify(map[string]float64{"loop_detected": 1})
	if !ft.HasFlag(FlagDoomLoopDetected) {
		t.Fatal("expected doom_loop flag")
	}
}

func TestClassifyMultipleWarningsBecomesError(t *testing.T) {
	ft := Classify(map[string]float64{"sidecar_error_count": 1, "loop_detected": 1})
	if ft.Severity != SeverityError {
		t.Fatalf("error+warning should produce error severity, got %q", ft.Severity)
	}
	if len(ft.Flags) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(ft.Flags))
	}
}

func TestClassifyRightFlowerError(t *testing.T) {
	ft := Classify(map[string]float64{"rightflower_error_count": 1})
	if !ft.HasFlag(FlagRightFlowerFailed) {
		t.Fatal("expected rightflower_failed flag")
	}
}

func TestTaxonomyMarkdown(t *testing.T) {
	ft := Classify(map[string]float64{"loop_detected": 1})
	md := ft.Markdown()
	if len(md) == 0 {
		t.Fatal("expected non-empty markdown")
	}
}
