package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"beishan/internal/llm"
	"beishan/internal/tools"
	"beishan/kernel"
)

/* ─── Suggest-to-Remember ──────────────────────── */

// PendingRemember 待确认的记忆建议
type PendingRemember struct {
	ID        string
	Title     string
	Summary   string
	Tags      []string
	ExpiresAt int64
}

var (
	// session-scoped pending remember
	pendingRemembers   = make(map[string]*PendingRemember)
	pendingRemembersMu sync.Mutex
)

// rememberTriggers 保守的触发关键词
var rememberTriggers = []string{
	"已放弃", "最终方案", "确认结论", "架构决定",
	"经验教训", "踩坑记录", "最终决定",
}

// shouldSuggestRemember 检测是否值得记住
func shouldSuggestRemember(userText, reply string) bool {
	combined := userText + " " + reply
	for _, trigger := range rememberTriggers {
		if strings.Contains(combined, trigger) {
			return true
		}
	}
	return false
}

// extractRememberCandidate 从对话中提取记忆候选
func extractRememberCandidate(userText, reply string) (title, summary string) {
	// 简单提取：用触发词所在句子作为摘要
	combined := userText + " " + reply
	for _, trigger := range rememberTriggers {
		if idx := strings.Index(combined, trigger); idx >= 0 {
			// 提取触发词前后各 50 字符
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + len(trigger) + 50
			if end > len(combined) {
				end = len(combined)
			}
			summary = combined[start:end]
			title = trigger
			return
		}
	}
	return
}

// createPendingRemember 创建待确认记忆（session-scoped，single-active）
func createPendingRemember(sessionID, title, summary string) *PendingRemember {
	id := "pr_" + newID()
	pr := &PendingRemember{
		ID:        id,
		Title:     title,
		Summary:   summary,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	}
	pendingRemembersMu.Lock()
	pendingRemembers[sessionID] = pr // replace previous
	pendingRemembersMu.Unlock()
	return pr
}

// confirmPendingRemember 确认待确认记忆
func confirmPendingRemember(sessionID string) *PendingRemember {
	pendingRemembersMu.Lock()
	defer pendingRemembersMu.Unlock()
	pr, ok := pendingRemembers[sessionID]
	if !ok {
		return nil
	}
	if time.Now().Unix() > pr.ExpiresAt {
		delete(pendingRemembers, sessionID)
		return nil
	}
	delete(pendingRemembers, sessionID)
	return pr
}

// isConfirmReply 检测是否是确认回复
func isConfirmReply(sessionID, text string) bool {
	t := strings.TrimSpace(text)
	if t == "确认" || t == "是" || t == "yes" {
		pendingRemembersMu.Lock()
		defer pendingRemembersMu.Unlock()
		pr, ok := pendingRemembers[sessionID]
		return ok && time.Now().Unix() <= pr.ExpiresAt
	}
	return false
}

// cleanupExpiredPending 清理过期的 pending remember
func cleanupExpiredPending() {
	pendingRemembersMu.Lock()
	defer pendingRemembersMu.Unlock()
	now := time.Now().Unix()
	for id, pr := range pendingRemembers {
		if now > pr.ExpiresAt {
			delete(pendingRemembers, id)
		}
	}
}

// newID 生成随机 ID
func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

/* ThinkPlugin （思考插件）

   处理一般对话、问答、闲聊、创意写作。
   收到 chat 消息后调 DeepSeek 生成回答。

   当用户直接说"你好"、"帮我写一首诗"等通用请求时，
   DeepSeek 路由到本插件，由本插件再调 DeepSeek 生成回答。
*/
type ThinkPlugin struct{}

func (p *ThinkPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "chat" {
		return kernel.Message{}, fmt.Errorf("think_plugin: 未知类型 %s", msg.Type)
	}

	userText := extractPrompt(msg.Payload)

	// 清理过期的 pending remember
	cleanupExpiredPending()

	// 检测是否是确认回复
	sessionID := msg.SessionID
	if isConfirmReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			// 调用 knowledge_remember 写入
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.Tags, 0)
			reply := fmt.Sprintf("已记录：%s\n%s", pr.Title, result.Output)
			replyJSON, _ := json.Marshal(reply)
			return kernel.Message{
				Type:    "chat.response",
				Payload: replyJSON,
			}, nil
		}
	}

	// 自动检索相关知识（固定步骤，不是 LLM 决策）
	background := ""
	trace := tools.NewRetrievalTrace(userText)
	matches := tools.SearchMemory(userText, 3, trace)
	if len(matches) > 0 {
		background = tools.FormatForPrompt(matches)
		fmt.Printf("[思考] 检索到 %d 条相关记忆\n", len(matches))
	}
	trace.Log()

	reply, err := callDeepSeek(userText, background)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	// Suggest-to-Remember：检测是否值得记住
	if shouldSuggestRemember(userText, reply) {
		title, summary := extractRememberCandidate(userText, reply)
		if title != "" {
			pr := createPendingRemember(sessionID, title, summary)
			reply += fmt.Sprintf("\n\n---\n是否将此结论加入知识库？回复「确认」即可。")
			fmt.Printf("[思考] 检测到值得记住的结论: %s (session=%s, pending=%s)\n", title, sessionID, pr.ID)
		}
	}

	replyJSON, _ := json.Marshal(reply)
	fmt.Printf("[思考] %s\n", truncate(reply, 120))
	return kernel.Message{
		Type:    "chat.response",
		Payload: replyJSON,
	}, nil
}

var systemPrompt = `你是 beishan-core 智能助手。你所在的系统提供以下能力：

- 搜索网络（search_plugin）
- 读写文件（write_plugin）
- 执行终端命令（terminal_plugin）
- 浏览器导航与网页提取（browser_plugin）
- 记忆存储与检索（memory_plugin）
- 待办管理（todo_plugin）
- 文本转语音（tts_plugin）
- 图片生成（image_gen_plugin）
- 法律审查工作流（workflow_plugin）

当用户需要上述能力时，请明确告知用户可以发送指定请求。
例如："你可以说'帮我搜索XX'来完成搜索"。

对于你能直接回答的问题（闲聊、知识、创作等），直接回答。
不需要说明自己是AI助手。`

// callDeepSeek 调 LLM API 生成回答。
// 使用共享 llm.ChatCompletion（不经过 think_plugin 自身路径，避免递归）。
// background 为检索到的知识上下文，为空时不注入。
func callDeepSeek(prompt string, background string) (string, error) {
	userMsg := prompt
	if background != "" {
		userMsg = "请参考以下背景知识回答：\n" + background + "\n\n用户问题：\n" + prompt
	}
	return llm.ChatCompletion(systemPrompt, userMsg, 60*time.Second)
}

// extractPrompt 从 Payload 中提取用户提示文本。
// 支持两种格式：
//   - JSON 对象：提取 message 字段（workflow 场景）
//   - JSON 字符串：直接返回（普通场景）
func extractPrompt(payload []byte) string {
	s := string(payload)
	// 尝试解析为 JSON 对象，提取 message 字段
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) == nil {
		if msg, ok := obj["message"].(string); ok {
			return msg
		}
	}
	// 回退：去掉外层引号
	return strings.TrimFunc(s, func(r rune) bool { return r == '"' })
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
