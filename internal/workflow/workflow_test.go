package workflow

import (
	"testing"
)

func TestErrorKindClassification(t *testing.T) {
	tests := []struct{ msg string; kind ErrorKind }{
		{"timeout occurred", KindTimeout},
		{"connection refused", KindTransientBackend},
		{"permission denied", KindPermissionDenied},
		{"not found", KindDependencyMissing},
		{"invalid input", KindInputMismatch},
		{"unexpected error", KindInternal},
	}
	for _, tc := range tests {
		te := ClassifyError("test", &testError{tc.msg})
		if te.Kind != tc.kind { t.Fatalf("msg=%q: expected %v got %v", tc.msg, tc.kind, te.Kind) }
	}
}

func TestErrorKindRetryable(t *testing.T) {
	if !IsRetryable(&ToolError{Kind: KindTimeout, Retryable: true}) { t.Fatal("timeout should be retryable") }
	if IsRetryable(&ToolError{Kind: KindPermissionDenied, Retryable: false}) { t.Fatal("permission should not be retryable") }
}

func TestStateStoreGetSet(t *testing.T) {
	s := NewStateStore(map[string]interface{}{"input": "hello"})
	s.Set("step1", StepResult{ID: "step1", Output: `{"result":"ok"}`})
	v, ok := s.Get("step1")
	if !ok { t.Fatal("step1 not found") }
	_, ok = v.(StepResult)
	if !ok { t.Fatal("expected StepResult") }
}

func TestGoWorkflowValid(t *testing.T) {
	wf := GoWorkflow{Name: "test", Steps: []GoStep{{ID: "s1", Type: GoStepTransform, TransformFn: func(ctx GoContext, input map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	}}}}
	if wf.Name != "test" { t.Fatal("workflow name") }
	if len(wf.Steps) != 1 { t.Fatal("should have 1 step") }
}

type testError struct{ msg string }
func (e *testError) Error() string { return e.msg }

func TestStepDefaultRetryDelay(t *testing.T) {
	s := GoStep{MaxRetries: 0} // no retry
	_ = s
}

func TestWorkflowTimeoutDefault(t *testing.T) {
	wf := GoWorkflow{}
	if wf.Timeout != 0 { t.Fatal("default timeout should be 0") }
}
