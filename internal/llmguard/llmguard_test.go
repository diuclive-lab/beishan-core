package llmguard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"beishan/internal/llm"
)

// withStubChatFunc 临时替换 chatFunc 为桩函数，测试结束自动恢复。
// 所有 Chat 路径测试都用这个工具函数，避免污染全局状态。
func withStubChatFunc(t *testing.T, stub func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error)) {
	t.Helper()
	original := chatFunc
	chatFunc = stub
	t.Cleanup(func() { chatFunc = original })
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
		JSONSchema:      "findings,risk_register",
	})
	// 检查关键关键词都在
	for _, kw := range []string{"防偷懒", "证据等级", "JSON 输出", "findings,risk_register"} {
		if !strings.Contains(b, kw) {
			t.Errorf("baseline 缺少关键词 %q\nbaseline=%s", kw, b)
		}
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
	c := Contract{OutputFormat: "json", JSONSchema: "findings,risk_register"}

	if err := validateOutput(`{"findings":[],"risk_register":[]}`, c); err != nil {
		t.Errorf("含全部字段应通过: %v", err)
	}
	if err := validateOutput(`{"findings":[]}`, c); err == nil {
		t.Errorf("缺 risk_register 应失败")
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
