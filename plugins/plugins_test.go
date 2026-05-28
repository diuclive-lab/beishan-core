package plugins

import (
	"encoding/json"
	"testing"

	"beishan/internal/tools"
	"beishan/kernel"
)

func TestThinkPluginRejectsNonChat(t *testing.T) {
	p := &ThinkPlugin{Kernel: nil}
	_, err := p.OnMessage(kernel.Message{Type: "non_chat", Payload: json.RawMessage(`"test"`)})
	if err == nil {
		t.Fatal("expected error for non-chat type")
	}
}

func TestSearchPluginImplementsPlugin(t *testing.T) {
	var p interface{} = &SearchPlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok {
		t.Fatal("SearchPlugin should implement kernel.Plugin")
	}
}

func TestWritePluginImplementsPlugin(t *testing.T) {
	var p interface{} = &WritePlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok {
		t.Fatal("WritePlugin should implement kernel.Plugin")
	}
}

func TestMemoryPluginImplementsPlugin(t *testing.T) {
	var p interface{} = &MemoryPlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok {
		t.Fatal("MemoryPlugin should implement kernel.Plugin")
	}
}

func TestLegalWritePluginImplementsPlugin(t *testing.T) {
	var p interface{} = &LegalWritePlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok {
		t.Fatal("LegalWritePlugin should implement kernel.Plugin")
	}
}

// ─── 静默失败专项测试 ───────────────────────────────────
// 验证所有插件在失败路径返回具体错误而非空 Message。
// 历史问题：browser_plugin/image_gen_plugin/tts_plugin/write_plugin
// 在工具执行失败时返回 kernel.Message{}, nil（空 Message 无 Payload）。

func TestBrowserPlugin_ReturnsProperPayload(t *testing.T) {
	p := &BrowserPlugin{}
	// browser_navigate 需要 URL 参数，不传应触发 schema 校验失败
	msg, err := p.OnMessage(kernel.Message{Type: "browser_navigate", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected schema validation error for empty payload")
	}
	// 验证：错误时返回 error，不是空 Message
	_ = msg
	if msg.Type != "" {
		t.Errorf("expected empty Type on error, got %q", msg.Type)
	}
}

func TestImageGenPlugin_ReturnsProperPayload(t *testing.T) {
	p := &ImageGenPlugin{}
	// image_generate 需要 prompt 参数，不传应触发 schema 校验失败
	msg, err := p.OnMessage(kernel.Message{Type: "image_generate", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected schema validation error for empty payload")
	}
	_ = msg
}

func TestTTSPlugin_ReturnsProperPayload(t *testing.T) {
	p := &TTSPlugin{}
	msg, err := p.OnMessage(kernel.Message{Type: "text_to_speech", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected schema validation error for empty payload")
	}
	_ = msg
}

func TestWritePlugin_ReturnsProperPayload(t *testing.T) {
	p := &WritePlugin{}
	// write_file 需要 path 和 content，不传应触发 schema 校验失败
	msg, err := p.OnMessage(kernel.Message{Type: "write_file", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected schema validation error for empty payload")
	}
	_ = msg

	// file_parse 也需要 path
	msg2, err2 := p.OnMessage(kernel.Message{Type: "file_parse", Payload: json.RawMessage(`{}`)})
	if err2 == nil {
		t.Fatal("expected schema validation error for empty payload")
	}
	_ = msg2

	// unknown type: default 返回 error
	_, err3 := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err3 == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestTodoPlugin_ErrorTypeOnFailure(t *testing.T) {
	p := &TodoPlugin{}
	// todo_add 无 task 参数 → 工具校验失败 → 返回 .error 类型
	msg, err := p.OnMessage(kernel.Message{Type: "todo_add", Payload: json.RawMessage(`{}`)})
	if err != nil {
		return // Go error 也接受（browser_plugin 等返回 Go error）
	}
	// todo_plugin 返回 .error 类型消息而非 Go error
	if msg.Type != "todo_add.error" {
		t.Errorf("expected type 'todo_add.error', got %q", msg.Type)
	}
	if msg.Payload == nil {
		t.Error("expected non-nil Payload even on error")
	}
}

func TestNotifyPlugin_ErrorTypeOnFailure(t *testing.T) {
	p := &NotifyPlugin{}
	// notify_send 无 channel/message 参数 → 工具校验失败
	msg, err := p.OnMessage(kernel.Message{Type: "notify_send", Payload: json.RawMessage(`{}`)})
	if err != nil {
		return
	}
	if msg.Type != "notify_send.error" {
		t.Errorf("expected type 'notify_send.error', got %q", msg.Type)
	}
	if msg.Payload == nil {
		t.Error("expected non-nil Payload even on error")
	}
}

func TestSkillFactoryPlugin_ErrorTypeOnFailure(t *testing.T) {
	p := &SkillFactoryPlugin{}
	// skill_evaluate 需要 YAML 参数
	msg, err := p.OnMessage(kernel.Message{Type: "skill_evaluate", Payload: json.RawMessage(`{}`)})
	if err != nil {
		return
	}
	if msg.Type != "skill_evaluate.error" {
		t.Errorf("expected type 'skill_evaluate.error', got %q", msg.Type)
	}
	if msg.Payload == nil {
		t.Error("expected non-nil Payload even on error")
	}
}

func TestBrowserPlugin_DefaultReturnsError(t *testing.T) {
	p := &BrowserPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestImageGenPlugin_DefaultReturnsError(t *testing.T) {
	p := &ImageGenPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestTTSPlugin_DefaultReturnsError(t *testing.T) {
	p := &TTSPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestNotifyPlugin_DefaultReturnsError(t *testing.T) {
	p := &NotifyPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestSeachPlugin_DefaultReturnsError(t *testing.T) {
	p := &SearchPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestTodoPlugin_DefaultReturnsError(t *testing.T) {
	p := &TodoPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestSkillFactoryPlugin_DefaultReturnsError(t *testing.T) {
	p := &SkillFactoryPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestClauseAnalyzerPlugin_DefaultReturnsError(t *testing.T) {
	p := &ClauseAnalyzerPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestLegalWritePlugin_DefaultReturnsError(t *testing.T) {
	p := &LegalWritePlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestLegalSearchPlugin_DefaultReturnsError(t *testing.T) {
	p := &LegalSearchPlugin{}
	_, err := p.OnMessage(kernel.Message{Type: "unknown_type", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

// ─── session_search_plugin 格式往返测试 ───────────────────────────────────
// 历史 bug：fmt.Sprintf("%q", output) 导致双重转义，接收方 json.Unmarshal 得到错误内容。
// 修复后验证：无论 session_list 返回 JSON 数组还是纯字符串，
// 接收方都必须能对 msg.Payload 做 json.Unmarshal，且结果不是双重转义字符串。

func TestSessionSearchPlugin_PayloadRoundTrip(t *testing.T) {
	tools.Init() // 必须：注册 session_list/session_search 工具
	p := &SessionSearchPlugin{}

	// session_list 不需要参数，空知识库下会返回 "No sessions found." 纯字符串
	msg, err := p.OnMessage(kernel.Message{
		Type:    "session_list",
		Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("session_list 执行失败: %v", err)
	}

	// 类型断言
	if msg.Type != "session_list.result" && msg.Type != "session_list.error" {
		t.Errorf("期望 Type 以 .result/.error 结尾，实际: %q", msg.Type)
	}

	// 核心：Payload 必须是合法 JSON（可被 Unmarshal）
	if msg.Payload == nil {
		t.Fatal("Payload 不能为 nil")
	}
	var decoded interface{}
	if err := json.Unmarshal(msg.Payload, &decoded); err != nil {
		t.Fatalf("Payload 不是合法 JSON（格式往返失败）: %v\npayload=%s", err, msg.Payload)
	}

	// 反双重转义验证：如果结果是字符串，不应再包含 \" 转义（说明未被 %q 双重编码）
	if s, ok := decoded.(string); ok {
		if len(s) > 0 && s[0] == '"' {
			t.Errorf("检测到双重转义：Payload 是 JSON 字符串，但内容又以引号开头（历史 %%q bug）: %q", s)
		}
	}
}

func TestSessionSearchPlugin_JSONArrayPayload(t *testing.T) {
	// 验证：当工具返回 JSON 数组时，Payload 直接透传（不被 Marshal 成字符串）
	// 这是 memory_plugin 模式一致性的检验点
	tools.Init()
	p := &SessionSearchPlugin{}
	msg, err := p.OnMessage(kernel.Message{
		Type:    "session_search",
		Payload: json.RawMessage(`{"query":"不存在的内容xyz123abc"}`),
	})
	if err != nil {
		t.Fatalf("session_search 执行失败: %v", err)
	}
	if msg.Payload == nil {
		t.Fatal("Payload 不能为 nil")
	}
	var decoded interface{}
	if err := json.Unmarshal(msg.Payload, &decoded); err != nil {
		t.Fatalf("session_search Payload 不是合法 JSON: %v\npayload=%s", err, msg.Payload)
	}
}
