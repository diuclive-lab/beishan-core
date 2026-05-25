package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

/* ─── YAML 解析测试 ─────────────────────────────── */

func TestLoadYAMLWorkflow(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
id: test_workflow
steps:
  - id: step1
    plugin: think_plugin
    type: chat
    inputs:
      message: "hello"
    next: step2
  - id: step2
    plugin: write_plugin
    type: write_file
    inputs:
      path: "out.txt"
`
	if err := os.WriteFile(filepath.Join(dir, "test_workflow.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	e := &Engine{Dir: dir}
	def, err := e.load("test_workflow")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if def.ID != "test_workflow" {
		t.Fatalf("expected test_workflow, got %s", def.ID)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(def.Steps))
	}
	if def.Steps[0].Plugin != "think_plugin" {
		t.Fatalf("expected think_plugin, got %s", def.Steps[0].Plugin)
	}
}

func TestLoadYAMLWorkflow_MissingID(t *testing.T) {
	dir := t.TempDir()
	content := `steps:
  - id: s1
    plugin: think_plugin
    type: chat`
	os.WriteFile(filepath.Join(dir, "no_id.yaml"), []byte(content), 0644)
	e := &Engine{Dir: dir}
	_, err := e.load("no_id")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestLoadYAMLWorkflow_NoSteps(t *testing.T) {
	dir := t.TempDir()
	content := `id: empty`
	os.WriteFile(filepath.Join(dir, "empty.yaml"), []byte(content), 0644)
	e := &Engine{Dir: dir}
	_, err := e.load("empty")
	if err == nil {
		t.Fatal("expected error for no steps")
	}
}

func TestLoadYAMLWorkflow_NotFound(t *testing.T) {
	e := &Engine{Dir: "/nonexistent"}
	_, err := e.load("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

/* ─── 模板解析测试 ──────────────────────────────── */

func TestBuildPayload_SingleRef(t *testing.T) {
	inputs := map[string]interface{}{
		"command": "${input}",
	}
	ctx := map[string]interface{}{"input": "hello world"}
	payload := buildPayload(inputs, ctx)

		// buildPayload wraps single ref in the parent object
		expected := `{"command":"hello world"}`
		if string(payload) != expected {
			t.Fatalf("expected %s, got %s", expected, string(payload))
	}
}

func TestBuildPayload_Interpolation(t *testing.T) {
	inputs := map[string]interface{}{
		"command": "echo ${steps.a.output}",
	}
	ctx := map[string]interface{}{"steps.a.output": "test"}
	payload := buildPayload(inputs, ctx)

	expected := `{"command":"echo test"}`
	if string(payload) != expected {
		t.Fatalf("expected %s, got %s", expected, string(payload))
	}
}

func TestBuildPayload_InterpolationMissing(t *testing.T) {
	// 模板变量不存在时应原样保留
	inputs := map[string]interface{}{
		"msg": "prefix ${nonexistent.key} suffix",
	}
	ctx := map[string]interface{}{}
	payload := buildPayload(inputs, ctx)

	if string(payload) != `{"msg":"prefix ${nonexistent.key} suffix"}` {
		t.Fatalf("expected unchanged template, got %s", string(payload))
	}
}

func TestBuildPayload_MultipleKeys(t *testing.T) {
	inputs := map[string]interface{}{
		"query": "${input}",
		"mode":  "no_retrieval",
	}
	ctx := map[string]interface{}{"input": "test query"}
	payload := buildPayload(inputs, ctx)

	expected := `{"mode":"no_retrieval","query":"test query"}`
	if string(payload) != expected {
		t.Fatalf("expected %s, got %s", expected, string(payload))
	}
}

func TestBuildPayload_EmptyInputs(t *testing.T) {
	inputs := map[string]interface{}{}
	ctx := map[string]interface{}{"input": "fallback"}
	payload := buildPayload(inputs, ctx)
	if string(payload) != `"fallback"` {
		t.Fatalf("expected fallback, got %s", string(payload))
	}
}

func TestBuildPayload_ParallelSubStepRef(t *testing.T) {
	inputs := map[string]interface{}{
		"message": "result: ${steps.parallel.output.sub_a}",
	}
	ctx := map[string]interface{}{"steps.parallel.output.sub_a": `{"ok":true}`}
	payload := buildPayload(inputs, ctx)

	expected := `{"message":"result: {\"ok\":true}"}`
	if string(payload) != expected {
		t.Fatalf("expected %s, got %s", expected, string(payload))
	}
}

/* ─── 条件分支测试 ──────────────────────────────── */

func TestResolveNext_Single(t *testing.T) {
	step := &StepDef{Next: NextList{{Goto: "next_step"}}}
	result := resolveNext(step, map[string]interface{}{})
	if result != "next_step" {
		t.Fatalf("expected next_step, got %s", result)
	}
}

func TestResolveNext_IfMatch(t *testing.T) {
	step := &StepDef{Next: NextList{
		{If: "steps.r1.output.status == 'clean'", Goto: "report"},
		{Default: true, Goto: "r2"},
	}}
	ctx := map[string]interface{}{"steps.r1.output.status": "clean"}
	result := resolveNext(step, ctx)
	if result != "report" {
		t.Fatalf("expected report, got %s", result)
	}
}

func TestResolveNext_Default(t *testing.T) {
	step := &StepDef{Next: NextList{
		{If: "steps.r1.output.status == 'clean'", Goto: "report"},
		{Default: true, Goto: "r2"},
	}}
	ctx := map[string]interface{}{"steps.r1.output.status": "found"}
	result := resolveNext(step, ctx)
	if result != "r2" {
		t.Fatalf("expected r2, got %s", result)
	}
}

func TestResolveNext_MultipleIf(t *testing.T) {
	step := &StepDef{Next: NextList{
		{If: "steps.r2.output.status == 'clean'", Goto: "report"},
		{If: "steps.r2.output.status == 'found'", Goto: "r3"},
		{Default: true, Goto: "report"},
	}}
	tests := []struct {
		status string
		want   string
	}{
		{"clean", "report"},
		{"found", "r3"},
		{"error", "report"},
	}
	for _, tc := range tests {
		ctx := map[string]interface{}{"steps.r2.output.status": tc.status}
		got := resolveNext(step, ctx)
		if got != tc.want {
			t.Fatalf("status=%s: expected %s, got %s", tc.status, tc.want, got)
		}
	}
}

func TestResolveNext_NoNext(t *testing.T) {
	step := &StepDef{Next: NextList{}}
	result := resolveNext(step, map[string]interface{}{})
	if result != "done" {
		t.Fatalf("expected done, got %s", result)
	}
}

/* ─── 条件表达式测试 ────────────────────────────── */

func TestEvaluateCondition_Equals(t *testing.T) {
	ctx := map[string]interface{}{"val": "ok"}
	if !evaluateCondition("val == 'ok'", ctx) {
		t.Fatal("expected true for val == 'ok'")
	}
	if evaluateCondition("val == 'bad'", ctx) {
		t.Fatal("expected false for val == 'bad'")
	}
}

func TestEvaluateCondition_NotEquals(t *testing.T) {
	ctx := map[string]interface{}{"val": "ok"}
	if !evaluateCondition("val != 'bad'", ctx) {
		t.Fatal("expected true for val != 'bad'")
	}
	if evaluateCondition("val != 'ok'", ctx) {
		t.Fatal("expected false for val != 'ok'")
	}
}

func TestEvaluateCondition_LongPath(t *testing.T) {
	ctx := map[string]interface{}{"steps.r1.output.status": "clean"}
	if !evaluateCondition("steps.r1.output.status == 'clean'", ctx) {
		t.Fatal("expected true for long path")
	}
	if evaluateCondition("steps.r1.output.status == 'found'", ctx) {
		t.Fatal("expected false for long path")
	}
}

func TestEvaluateCondition_JSONField(t *testing.T) {
	// 测试通过 extractJSONField 解析 JSON 路径
	ctx := map[string]interface{}{
		"steps.r1.output": `{"status":"clean","findings":[]}`,
	}
	if !evaluateCondition("steps.r1.output.status == 'clean'", ctx) {
		t.Fatal("expected true for JSON field extraction")
	}
}

/* ─── 真实工作流加载测试 ────────────────────────── */

func TestLoadCodeReview9x(t *testing.T) {
	// 从项目 workflows/ 目录加载真实的 code_review_9x.yaml
	e := &Engine{Dir: "../../workflows"}
	def, err := e.load("code_review_9x")
	if err != nil {
		t.Fatalf("加载 code_review_9x 失败: %v", err)
	}
	if def.ID != "code_review_9x" {
		t.Fatalf("expected code_review_9x, got %s", def.ID)
	}
	if len(def.Steps) < 5 {
		t.Fatalf("expected >=5 steps, got %d", len(def.Steps))
	}

	// 验证步骤类型分布（含并行子步骤）
	var parallelCount, thinkCount, terminalCount, writeCount int
	for _, s := range def.Steps {
		switch s.Plugin {
		case "think_plugin":
			thinkCount++
		case "terminal_plugin":
			terminalCount++
		case "write_plugin":
			writeCount++
		}
		if s.Type == "parallel" {
			parallelCount++
			if len(s.ParallelSteps) != 9 {
				t.Fatalf("expected 9 parallel sub-steps, got %d", len(s.ParallelSteps))
			}
			// 并行子步骤中的 think_plugin 也计入
			for _, sub := range s.ParallelSteps {
				if sub.Plugin == "think_plugin" {
					thinkCount++
				}
			}
		}
		// 验证所有非终端步骤都有 timeout（report 是最后一步，不强制）
		if s.Timeout <= 0 && s.Type != "parallel" && s.ID != "report" {
			t.Fatalf("step %s has no timeout", s.ID)
		}
	}
	if parallelCount != 1 {
		t.Fatalf("expected 1 parallel step, got %d", parallelCount)
	}
	if thinkCount < 8 {
		t.Fatalf("expected >=8 think_plugin steps, got %d", thinkCount)
	}

	// 验证逆向审计的 next 条件逻辑
	r1 := findStep(def, "reverse_audit_r1")
	if r1 == nil {
		t.Fatal("reverse_audit_r1 not found")
	}
	if len(r1.Next) < 2 {
		t.Fatal("r1 should have >=2 next conditions")
	}

	r3 := findStep(def, "reverse_audit_r3")
	if r3 == nil {
		t.Fatal("reverse_audit_r3 not found")
	}
	if len(r3.Next) == 0 {
		t.Fatal("r3 should have a next (report)")
	}
}
