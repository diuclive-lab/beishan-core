package clarify

import "testing"

func TestBuildQuestion(t *testing.T) {
	q := BuildQuestion("test", []string{"opt1", "opt2"}, nil)
	if q == "" { t.Fatal("expected question") }
}

func TestNewResponse(t *testing.T) {
	r := NewResponse("input", "selected", []string{"a", "b"})
	if r.Input != "input" { t.Fatalf("input=%q", r.Input) }
	if r.Selected != "selected" { t.Fatalf("selected=%q", r.Selected) }
	if len(r.Candidates) != 2 { t.Fatalf("candidates=%d", len(r.Candidates)) }
}
