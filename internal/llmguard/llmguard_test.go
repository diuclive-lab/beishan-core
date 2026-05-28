package llmguard

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"beishan/internal/llm"
)

// withStubChatFunc 临时替换 defaultChatFunc 为桩函数，测试结束自动恢复。
// 所有 Chat 路径测试都用这个工具函数，避免污染全局状态。
func withStubChatFunc(t *testing.T, stub func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error)) {
	t.Helper()
	original := defaultChatFunc
	defaultChatFunc = stub
	t.Cleanup(func() { defaultChatFunc = original })
}

// ─── baseline 注入测试 ─────────────────────────────────────────

func TestBuildBaseline_Empty(t *testing.T) {
	if b := buildBaseline(Contract{}); b != "" {
		t.Errorf("零值 Contract 应返回空 baseline, 实际: %q", b)
	}
}

func TestBuildBaseline_AllFlags(t *testing.T) {
	b := buildBaseline(Contract{
		AntiLazy:        true,
		RequireEvidence: true,
		OutputFormat:    "json",
		RequiredFields:  "findings,risk_register",
	})
	// 检查关键关键词都在
	for _, kw := range []string{"防偷懒", "证据等级", "JSON 输出", "findings,risk_register"} {
		if !strings.Contains(b, kw) {
			t.Errorf("baseline 缺少关键词 %q\nbaseline=%s", kw, b)
		}
	}
}

func TestBuildBaseline_YAMLFormat(t *testing.T) {
	b := buildBaseline(Contract{
		OutputFormat:   "yaml",
		RequiredFields: "id,steps",
	})
	for _, kw := range []string{"YAML 输出", "id,steps"} {
		if !strings.Contains(b, kw) {
			t.Errorf("YAML baseline 缺少关键词 %q\nbaseline=%s", kw, b)
		}
	}
	// YAML baseline 不应出现 JSON 字样（避免混淆）
	if strings.Contains(b, "JSON 输出") {
		t.Errorf("YAML baseline 不应包含 JSON 输出规则")
	}
}

func TestInjectBaseline_AppendToExistingSystem(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "你好"},
	}
	out := injectBaseline(msgs, "BASELINE")
	if out[0].Role != "system" || !strings.Contains(out[0].Content, "你是助手") || !strings.Contains(out[0].Content, "BASELINE") {
		t.Errorf("应追加到现有 system message, 实际: %+v", out[0])
	}
	// 不修改入参
	if msgs[0].Content != "你是助手" {
		t.Errorf("入参被污染: %q", msgs[0].Content)
	}
}

func TestInjectBaseline_InsertWhenNoSystem(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: "user", Content: "你好"},
	}
	out := injectBaseline(msgs, "BASELINE")
	if len(out) != 2 || out[0].Role != "system" || out[0].Content != "BASELINE" {
		t.Errorf("应在开头插入 system message, 实际: %+v", out)
	}
}

func TestInjectBaseline_EmptyBaseline(t *testing.T) {
	msgs := []llm.ChatMessage{{Role: "user", Content: "你好"}}
	out := injectBaseline(msgs, "")
	if len(out) != 1 {
		t.Errorf("空 baseline 不应增加 message, 实际长度: %d", len(out))
	}
}

// ─── validateOutput 测试 ──────────────────────────────────────

func TestValidate_NoContract(t *testing.T) {
	if err := validateOutput("anything", Contract{}); err != nil {
		t.Errorf("零值 Contract 不应校验失败: %v", err)
	}
}

func TestValidate_JSONFormat(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{"plain json", `{"a":1}`, false},
		{"markdown wrapped", "```json\n{\"a\":1}\n```", false},
		{"not json", "this is text", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutput(tc.output, Contract{OutputFormat: "json"})
			if (err != nil) != tc.wantErr {
				t.Errorf("output=%q wantErr=%v gotErr=%v", tc.output, tc.wantErr, err)
			}
		})
	}
}

func TestValidate_JSONSchema(t *testing.T) {
	c := Contract{OutputFormat: "json", RequiredFields: "findings,risk_register"}

	if err := validateOutput(`{"findings":[],"risk_register":[]}`, c); err != nil {
		t.Errorf("含全部字段应通过: %v", err)
	}
	if err := validateOutput(`{"findings":[]}`, c); err == nil {
		t.Errorf("缺 risk_register 应失败")
	}
}

func TestValidate_YAMLFormat(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{"valid yaml", "id: test\nsteps:\n  - id: s1", false},
		{"markdown wrapped", "```yaml\nid: test\nsteps: []\n```", false},
		{"invalid yaml", "id: [\nbad: yaml: here", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutput(tc.output, Contract{OutputFormat: "yaml"})
			if (err != nil) != tc.wantErr {
				t.Errorf("output=%q wantErr=%v gotErr=%v", tc.output, tc.wantErr, err)
			}
		})
	}
}

func TestValidate_YAMLRequiredFields(t *testing.T) {
	c := Contract{OutputFormat: "yaml", RequiredFields: "id,steps"}

	if err := validateOutput("id: wf\nsteps:\n  - id: s1", c); err != nil {
		t.Errorf("含全部字段应通过: %v", err)
	}
	if err := validateOutput("id: wf\nname: test", c); err == nil {
		t.Errorf("缺 steps 应失败")
	}
	// 错误消息应说 YAML 而不是 JSON
	err := validateOutput("id: wf", c)
	if err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Errorf("错误消息应包含 YAML, 实际: %v", err)
	}
}

func TestValidate_Evidence(t *testing.T) {
	c := Contract{RequireEvidence: true}

	if err := validateOutput("发现问题 X (E1: 见 file.go:42)", c); err != nil {
		t.Errorf("含 E1 应通过: %v", err)
	}
	if err := validateOutput("根据证据，可以判断...", c); err != nil {
		t.Errorf("含\"证据\"字样应通过: %v", err)
	}
	if err := validateOutput("我觉得这有问题", c); err == nil {
		t.Errorf("无证据标注应失败")
	}
}

// ─── Chat 主流程测试 ──────────────────────────────────────

func TestChat_NoContract_PassThrough(t *testing.T) {
	called := 0
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		called++
		return "OK", &llm.Usage{TotalTokens: 10}, nil
	})

	out, usage, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "hi"}},
		Contract{},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "OK" || usage.TotalTokens != 10 || called != 1 {
		t.Errorf("got out=%q usage=%+v called=%d", out, usage, called)
	}
}

func TestChat_JSONRetry_RecoverOnSecondAttempt(t *testing.T) {
	calls := 0
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		calls++
		if calls == 1 {
			return "not json", &llm.Usage{TotalTokens: 5}, nil
		}
		return `{"ok":true}`, &llm.Usage{TotalTokens: 7}, nil
	})

	out, usage, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "give me json"}},
		Contract{OutputFormat: "json", MaxRetries: 1},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("应在第二次重试成功: %v", err)
	}
	if calls != 2 {
		t.Errorf("应调用 2 次, 实际 %d", calls)
	}
	if !strings.Contains(out, `"ok"`) {
		t.Errorf("第二次输出未返回, got: %q", out)
	}
	if usage.TotalTokens != 12 {
		t.Errorf("usage 应累加为 12, 实际 %d", usage.TotalTokens)
	}
}

func TestChat_RetryExhausted_ReturnsLastOutputWithError(t *testing.T) {
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		return "still not json", &llm.Usage{TotalTokens: 3}, nil
	})

	out, _, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "x"}},
		Contract{OutputFormat: "json", MaxRetries: 2},
		5*time.Second,
	)
	if err == nil {
		t.Fatal("重试用尽应返回 error")
	}
	if !strings.Contains(out, "still not json") {
		t.Errorf("应返回最后一次输出供降级使用, got: %q", out)
	}
}

func TestChat_LLMCallFailure_NoRetry(t *testing.T) {
	calls := 0
	stubErr := errors.New("network down")
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		calls++
		return "", nil, stubErr
	})

	_, _, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "x"}},
		Contract{MaxRetries: 3}, // 即使指定了 3 次也不应重试网络错误
		5*time.Second,
	)
	if err == nil {
		t.Fatal("网络错误应直接返回")
	}
	if calls != 1 {
		t.Errorf("网络错误不应重试, 调用次数 %d", calls)
	}
}

func TestChat_BaselineInjected(t *testing.T) {
	var capturedMessages []llm.ChatMessage
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		capturedMessages = messages
		return "OK", &llm.Usage{}, nil
	})

	_, _, _ = Chat(
		[]llm.ChatMessage{{Role: "user", Content: "hi"}},
		Contract{AntiLazy: true},
		5*time.Second,
	)
	if len(capturedMessages) < 2 || capturedMessages[0].Role != "system" {
		t.Fatalf("应在前面插入 system baseline, 实际: %+v", capturedMessages)
	}
	if !strings.Contains(capturedMessages[0].Content, "防偷懒") {
		t.Errorf("baseline 未注入: %q", capturedMessages[0].Content)
	}
}

// ─── critique-revise 测试 ─────────────────────────────────────

func TestChat_Critique_NoIssues_ReturnsOriginal(t *testing.T) {
	calls := 0
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		calls++
		switch calls {
		case 1: // 主调用
			return `{"finding":"x","evidence":"E1"}`, &llm.Usage{TotalTokens: 10}, nil
		case 2: // critique
			return `{"has_issues": false, "issues": []}`, &llm.Usage{TotalTokens: 5}, nil
		}
		t.Fatalf("不应调用第 %d 次", calls)
		return "", nil, nil
	})

	out, usage, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "x"}},
		Contract{OutputFormat: "json", RequireEvidence: true, Critique: true},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.Contains(out, "E1") {
		t.Errorf("应返回原输出: %q", out)
	}
	if usage.TotalTokens != 15 {
		t.Errorf("usage 应累加主+critique = 15, 实际 %d", usage.TotalTokens)
	}
}

// ─── 维度化 preset API 测试 ─────────────────────────────────

func TestForStructure(t *testing.T) {
	c := ForStructure("json", "findings,risk_register", 1)
	if c.OutputFormat != "json" {
		t.Errorf("OutputFormat want json, got %q", c.OutputFormat)
	}
	if c.RequiredFields != "findings,risk_register" {
		t.Errorf("RequiredFields mismatch: %q", c.RequiredFields)
	}
	if c.MaxRetries != 1 {
		t.Errorf("MaxRetries want 1, got %d", c.MaxRetries)
	}
	// 结构维度不应启用 AntiLazy / Critique
	if c.AntiLazy || c.Critique || c.RequireEvidence {
		t.Errorf("纯 ForStructure 不应启用其他维度: %+v", c)
	}
}

func TestForStructure_YAML(t *testing.T) {
	c := ForStructure("yaml", "id,steps", 1)
	if c.OutputFormat != "yaml" {
		t.Errorf("OutputFormat want yaml, got %q", c.OutputFormat)
	}
	if c.RequiredFields != "id,steps" {
		t.Errorf("RequiredFields mismatch: %q", c.RequiredFields)
	}
}

func TestForContent(t *testing.T) {
	c := ForContent()
	if !c.AntiLazy {
		t.Errorf("ForContent 应启用 AntiLazy")
	}
	// 内容维度不应启用结构/事实
	if c.OutputFormat != "" || c.Critique || c.RequireEvidence {
		t.Errorf("纯 ForContent 不应启用其他维度: %+v", c)
	}
}

func TestForFacts(t *testing.T) {
	c := ForFacts()
	if !c.RequireEvidence || !c.AntiLazy || !c.Critique {
		t.Errorf("ForFacts 应启用 RequireEvidence + AntiLazy + Critique: %+v", c)
	}
}

func TestFluentComposition(t *testing.T) {
	// 三维度全开（V25 全合规场景）
	c := ForStructure("json", "findings", 1).WithContent().WithFacts()
	if c.OutputFormat != "json" || c.RequiredFields != "findings" || c.MaxRetries < 1 {
		t.Errorf("结构维度丢失: %+v", c)
	}
	if !c.AntiLazy || !c.RequireEvidence || !c.Critique {
		t.Errorf("内容+事实维度丢失: %+v", c)
	}
}

func TestWithStructure_PreservesRetries(t *testing.T) {
	// 先设置 3 次重试，再叠加 WithStructure(1)，应保留 3（取大）
	c := Contract{MaxRetries: 3}.WithStructure("json", "x", 1)
	if c.MaxRetries != 3 {
		t.Errorf("WithStructure 不应降低 MaxRetries, 实际 %d", c.MaxRetries)
	}
}

// ─── buildRetryFeedback 测试 ─────────────────────────────────

func TestBuildRetryFeedback_JSONMissingField_ShowsPresent(t *testing.T) {
	// 有现有字段时，反馈应包含"已有字段"信息
	output := `{"findings":[],"summary":"ok"}`
	violation := fmt.Errorf("输出 JSON 缺少必需字段：risk_register")
	c := Contract{OutputFormat: "json", RequiredFields: "findings,risk_register"}

	fb := buildRetryFeedback(output, violation, c)
	if !strings.Contains(fb, "已有字段") {
		t.Errorf("应包含已有字段信息, got: %q", fb)
	}
	if !strings.Contains(fb, "findings") || !strings.Contains(fb, "summary") {
		t.Errorf("应列出现有字段 findings/summary, got: %q", fb)
	}
	if !strings.Contains(fb, "risk_register") {
		t.Errorf("应提示缺失字段 risk_register, got: %q", fb)
	}
}

func TestBuildRetryFeedback_JSONMarkdownWrapped_SpecificHint(t *testing.T) {
	// 有 ``` 时，应给出"去掉代码块"的具体指示
	output := "```json\n{\"a\":1}\n```"
	violation := fmt.Errorf(`输出不是合法 JSON（前 100 字符: "` + "```" + `json..."）`)
	c := Contract{OutputFormat: "json"}

	fb := buildRetryFeedback(output, violation, c)
	if !strings.Contains(fb, "```") && !strings.Contains(fb, "代码块") {
		t.Errorf("应提示去掉 markdown 包裹, got: %q", fb)
	}
}

func TestBuildRetryFeedback_YAMLMissingField_ShowsPresent(t *testing.T) {
	output := "id: wf\nname: test"
	violation := fmt.Errorf("输出 YAML 缺少必需字段：steps")
	c := Contract{OutputFormat: "yaml", RequiredFields: "id,steps"}

	fb := buildRetryFeedback(output, violation, c)
	if !strings.Contains(fb, "已有字段") {
		t.Errorf("应包含已有字段信息, got: %q", fb)
	}
	if !strings.Contains(fb, "id") {
		t.Errorf("应列出现有字段 id, got: %q", fb)
	}
	if !strings.Contains(fb, "steps") {
		t.Errorf("应提示缺失字段 steps, got: %q", fb)
	}
}

func TestBuildRetryFeedback_CheckAllMissingFields(t *testing.T) {
	// checkRequiredFields 应一次收集所有缺失字段，不是首次即返
	c := Contract{OutputFormat: "json", RequiredFields: "a,b,c"}
	err := validateOutput(`{"x":1}`, c)
	if err == nil {
		t.Fatal("应失败")
	}
	// 错误消息应包含全部 3 个缺失字段
	msg := err.Error()
	for _, field := range []string{"a", "b", "c"} {
		if !strings.Contains(msg, field) {
			t.Errorf("缺失字段 %q 应在错误消息中, got: %q", field, msg)
		}
	}
}

func TestChat_YAMLRetry_RecoverOnSecondAttempt(t *testing.T) {
	calls := 0
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		calls++
		if calls == 1 {
			return "this is not yaml: [bad", &llm.Usage{TotalTokens: 4}, nil
		}
		return "id: wf\nsteps:\n  - id: s1", &llm.Usage{TotalTokens: 6}, nil
	})

	out, _, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "生成工作流"}},
		Contract{OutputFormat: "yaml", RequiredFields: "id,steps", MaxRetries: 1},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("应在第二次重试成功: %v", err)
	}
	if calls != 2 {
		t.Errorf("应调用 2 次, 实际 %d", calls)
	}
	if !strings.Contains(out, "id: wf") {
		t.Errorf("应返回第二次输出, got: %q", out)
	}
}

func TestWithEvidence_NoCritique(t *testing.T) {
	// WithEvidence 应只启用 evidence 标注，不启用 critique（避免成本翻倍）
	c := ForContent().WithEvidence()
	if !c.RequireEvidence {
		t.Errorf("WithEvidence 应启用 RequireEvidence")
	}
	if c.Critique {
		t.Errorf("WithEvidence 不应启用 Critique（区别于 WithFacts）")
	}
}

func TestWithRetries_Overrides(t *testing.T) {
	c := ForStructure("json", "x", 1).WithRetries(5)
	if c.MaxRetries != 5 {
		t.Errorf("WithRetries 应直接覆盖, 实际 %d", c.MaxRetries)
	}
}

func TestChat_WithProvider_ClosureRouted(t *testing.T) {
	// 验证 ChatWithProvider 不会走 defaultChatFunc，
	// 而是用闭包里的 llm.ChatCompletionWithProvider。
	// 这里我们不能拦截 llm 包内部函数，但可以测 defaultChatFunc 不被调用。
	defaultCalled := false
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		defaultCalled = true
		return "should not be used", nil, nil
	})

	// ChatWithProvider 会尝试调 llm.ChatCompletionWithProvider，
	// 无配置 provider 时会返回 API_KEY 未设置错误。
	// 我们关心的是：defaultChatFunc 不被调用。
	_, _, _ = ChatWithProvider("nonexistent_provider", []llm.ChatMessage{{Role: "user", Content: "x"}},
		Contract{}, 1*time.Second)

	if defaultCalled {
		t.Errorf("ChatWithProvider 不应触达 defaultChatFunc（应走闭包）")
	}
}

func TestChat_Critique_WithIssues_TriggersRevise(t *testing.T) {
	calls := 0
	withStubChatFunc(t, func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error) {
		calls++
		switch calls {
		case 1: // 主调用
			return `{"finding":"x","evidence":"E4"}`, &llm.Usage{TotalTokens: 10}, nil
		case 2: // critique
			return `{"has_issues": true, "issues": ["evidence E4 未注明推测前缀"]}`, &llm.Usage{TotalTokens: 5}, nil
		case 3: // revise
			return `{"finding":"x","evidence":"E4","note":"推测：..."}`, &llm.Usage{TotalTokens: 8}, nil
		}
		t.Fatalf("不应调用第 %d 次", calls)
		return "", nil, nil
	})

	out, usage, err := Chat(
		[]llm.ChatMessage{{Role: "user", Content: "x"}},
		Contract{OutputFormat: "json", RequireEvidence: true, Critique: true},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.Contains(out, "推测") {
		t.Errorf("应返回 revise 后的输出: %q", out)
	}
	if calls != 3 {
		t.Errorf("应调用 3 次（主+critique+revise）, 实际 %d", calls)
	}
	if usage.TotalTokens != 23 {
		t.Errorf("usage 应累加 10+5+8=23, 实际 %d", usage.TotalTokens)
	}
}
