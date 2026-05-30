package workflow

import (
	"strings"
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

// TestGoExecutorRun_StepPanicRecovered 证明 R1：同步步骤里的 panic 被兜底成失败
// WorkflowResult 返回，而非把整个请求带栈中断（此测试若兜底失效会直接 panic 挂掉）。
func TestGoExecutorRun_StepPanicRecovered(t *testing.T) {
	ex := NewGoExecutor(nil, nil)
	wf := GoWorkflow{Name: "panic_wf", Steps: []GoStep{{
		ID:   "boom",
		Type: GoStepTransform,
		TransformFn: func(ctx GoContext, input map[string]interface{}) (map[string]interface{}, error) {
			panic("boom in transform")
		},
	}}}
	res := ex.Run(wf, map[string]interface{}{"input": "x"}) // 能正常返回即证明兜底生效
	if res == nil {
		t.Fatal("Run 在 panic 后返回 nil；应返回失败 WorkflowResult")
	}
	if res.Success {
		t.Fatal("步骤 panic 后 Success 应为 false")
	}
	if !strings.Contains(res.Error, "panic") {
		t.Fatalf("错误信息应含 panic，实得 %q", res.Error)
	}
}

// TestGoExecutorRun_ParallelSubStepPanicRecorded 证明 R1 并行侧：子步骤 panic 被记成
// 一条失败 StepResult（不静默消失），整体不 crash。
func TestGoExecutorRun_ParallelSubStepPanicRecorded(t *testing.T) {
	ex := NewGoExecutor(nil, nil)
	wf := GoWorkflow{Name: "par_wf", Steps: []GoStep{{
		ID:   "par",
		Type: GoStepParallel,
		SubSteps: []GoStep{
			{ID: "good", Type: GoStepTransform, TransformFn: func(ctx GoContext, in map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			}},
			{ID: "boom", Type: GoStepTransform, TransformFn: func(ctx GoContext, in map[string]interface{}) (map[string]interface{}, error) {
				panic("boom in parallel")
			}},
		},
	}}}
	res := ex.Run(wf, nil)
	if res == nil {
		t.Fatal("Run 返回 nil")
	}
	var par *StepResult
	for i := range res.Steps {
		if res.Steps[i].ID == "par" {
			par = &res.Steps[i]
		}
	}
	if par == nil {
		t.Fatal("缺少并行步骤结果")
	}
	if len(par.SubResults) != 2 {
		t.Fatalf("应有 2 个子步骤结果(good + 被兜底的 boom)，实得 %d", len(par.SubResults))
	}
	panicCount := 0
	for _, sr := range par.SubResults {
		if strings.Contains(sr.Error, "panic") {
			panicCount++
		}
	}
	if panicCount != 1 {
		t.Fatalf("应恰有 1 个子步骤记为 panic 错误，实得 %d", panicCount)
	}
}

// TestNewGoWorkflowPlugin_BadToolStepReturnsError 证明：GoStepTool 引用未注册工具时，
// 构造返回 error（而非 panic 掀翻进程）——调用方据此降级跳过，daemon 照常启动。
func TestNewGoWorkflowPlugin_BadToolStepReturnsError(t *testing.T) {
	router := map[string]GoWorkflow{
		"bad": {Name: "bad", Steps: []GoStep{{ID: "s", Type: GoStepTool, Tool: "ghost_tool"}}},
	}
	p, err := NewGoWorkflowPlugin(nil, map[string]string{}, func(string) bool { return false }, router)
	if err == nil {
		t.Fatal("引用未注册工具应返回 error，而非 panic")
	}
	if p != nil {
		t.Fatal("出错时 plugin 应为 nil")
	}
	if !strings.Contains(err.Error(), "ghost_tool") {
		t.Fatalf("error 应点名缺失工具，实得 %q", err.Error())
	}
}

// TestNewGoWorkflowPlugin_PluginStepsOK 证明：全 GoStepPlugin 步骤（如 legal_review）
// 不触发工具校验，正常返回 (plugin, nil)。
func TestNewGoWorkflowPlugin_PluginStepsOK(t *testing.T) {
	router := map[string]GoWorkflow{
		"ok": {Name: "ok", Steps: []GoStep{{ID: "s", Type: GoStepPlugin, Recipient: "x", MsgType: "y"}}},
	}
	p, err := NewGoWorkflowPlugin(nil, map[string]string{}, func(string) bool { return true }, router)
	if err != nil {
		t.Fatalf("合法插件步骤不应出错: %v", err)
	}
	if p == nil {
		t.Fatal("成功时 plugin 不应为 nil")
	}
}

// TestNewGoToolPlugin_UnregisteredToolReturnsError 证明：NewGoToolPlugin 对未注册工具
// 返回 (nil, error) 而非 panic。
func TestNewGoToolPlugin_UnregisteredToolReturnsError(t *testing.T) {
	p, err := NewGoToolPlugin(nil, map[string]string{}, func(string) bool { return false }, map[string]string{"t": "ghost"})
	if err == nil || p != nil {
		t.Fatalf("未注册工具应返回 (nil, error)，实得 p=%v err=%v", p, err)
	}
}
