package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/retrieval"
	"beishan/internal/tools"
	"beishan/kernel"
)

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
type ThinkPlugin struct {
	Kernel *kernel.Kernel
}

func (p *ThinkPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "chat" {
		return kernel.Message{}, fmt.Errorf("think_plugin: 未知类型 %s", msg.Type)
	}

	userText := extractPrompt(msg.Payload)
	sessionID := extractSessionID(msg.Payload)
	mode := extractMode(msg.Payload)

	// Mode dispatch 优先（L4 语义，不污染 L1）
	// no_retrieval 模式跳过所有自然语言检测，直接调 LLM
	switch mode {
	case ModeReviewExtract:
		return p.handleReviewExtract(userText, sessionID)
	case ModeNoRetrieval:
		return p.handleChatNoRetrieval(userText)
	}

	// 清理过期的 pending remember（懒清理 review）
	cleanupExpiredPending()

	// 批量确认检测（L4 语义）
	if isBatchConfirm(userText) {
		reviewID, indices, err := parseBatchConfirmWithID(userText)
		if err != nil {
			reply := fmt.Sprintf("格式错误，请使用：确认 1,2 或 确认 reviewID 1,2")
			replyJSON, _ := json.Marshal(reply)
			return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
		}

		// 获取审查报告（优先从内存缓存，其次从文件）
		var candidates []KnowledgeCandidate
		if reviewID != "" {
			// 从文件加载
			rf, loadErr := loadReviewFromFile(reviewID)
			if loadErr != nil {
				reply := fmt.Sprintf("找不到审查报告 %s", reviewID)
				replyJSON, _ := json.Marshal(reply)
				return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
			}
			candidates = rf.Candidates
		} else {
			// 从内存缓存加载（兼容旧格式）
			pr := getPendingReview(sessionID)
			if pr == nil {
				reply := fmt.Sprintf("没有待确认的审查报告，请先运行知识审查工作流")
				replyJSON, _ := json.Marshal(reply)
				return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
			}
			candidates = pr.Candidates
			reviewID = sessionID
		}

		// 执行入库
		var recorded []string
		for _, idx := range indices {
			if idx > 0 && idx <= len(candidates) {
				candidate := candidates[idx-1]
				result := tools.KnowledgeRemember(candidate.Title, candidate.Summary, nil, 0)
				recorded = append(recorded, fmt.Sprintf("%d. %s: %s", idx, candidate.Title, result.Output))
			}
		}

		// 清理（内存缓存 + 文件）
		clearPendingReview(reviewID)
		deleteReviewFile(reviewID)

		reply := fmt.Sprintf("已记录 %d 条知识：\n%s", len(recorded), strings.Join(recorded, "\n"))
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	// 单条确认检测（Stage 2）
	if isConfirmReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.Tags, 0)
			reply := fmt.Sprintf("已记录：%s\n%s", pr.Title, result.Output)
			replyJSON, _ := json.Marshal(reply)
			return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
		}
	}

	// 自然语言入口：知识审查触发
	if isReviewTrigger(userText) {
		return p.triggerReviewWorkflow()
	}

	// 自然语言入口：查看审查报告
	if isListReviews(userText) {
		return p.handleListReviews()
	}

	// 自然语言入口：全部确认
	if isConfirmAll(userText) {
		return p.handleConfirmAll()
	}

	// 自然语言入口：跳过/清理
	if isSkipAll(userText) {
		return p.handleSkipAll()
	}

	return p.handleChat(userText, sessionID)
}

// handleChatNoRetrieval 处理 workflow 步骤：跳过检索，直接调 LLM。
// 用于需要精确 JSON 输出的步骤（如 classify/evaluate），避免检索上下文干扰。
func (p *ThinkPlugin) handleChatNoRetrieval(userText string) (kernel.Message, error) {
	reply, err := llm.ChatCompletion(systemPrompt, userText, 120*time.Second)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	reply = strings.TrimSpace(reply)
	var respPayload json.RawMessage
	if len(reply) > 0 && reply[0] == '{' && json.Valid([]byte(reply)) {
		respPayload = json.RawMessage(reply)
	} else {
		respPayload, _ = json.Marshal(reply)
	}
	fmt.Printf("[思考] %s\n", truncate(reply, 120))
	return kernel.Message{Type: "chat.response", Payload: respPayload}, nil
}

// handleChat 处理普通聊天
func (p *ThinkPlugin) handleChat(userText, sessionID string) (kernel.Message, error) {
	background := ""

	// 项目路径（从环境变量或固定配置）
	projectPath := os.Getenv("BEISHAN_PROJECT_PATH")
	if projectPath == "" {
		projectPath = "."
	}

	// 执行完整检索管道
	results, trace := RunFullRetrieval(userText, projectPath)

	if len(results) > 0 {
		background = retrieval.FormatForPromptFull(results)
		fmt.Printf("[思考] 检索到 %d 条相关记忆\n", len(results))
	}
	trace.Log()

	reply, err := callDeepSeek(userText, background)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	// Suggest-to-Remember
	if shouldSuggestRemember(userText, reply) {
		title, summary := extractRememberCandidate(userText, reply)
		if title != "" {
			pr := createPendingRemember(sessionID, title, summary)
			reply += fmt.Sprintf("\n\n---\n是否将此结论加入知识库？回复「确认」即可。")
			fmt.Printf("[思考] 检测到值得记住的结论: %s (session=%s, pending=%s)\n", title, sessionID, pr.ID)
		}
	}

	// 检索过程可视化（仅非纯 JSON 回答，如 workflow 步骤不追加）
	isJSON := len(reply) > 0 && reply[0] == '{' && json.Valid([]byte(reply))
	if !isJSON {
		traceText := tools.FormatTrace(trace)
		reply += traceText
	}

	reply = strings.TrimSpace(reply)
	var respPayload json.RawMessage
	if isJSON {
		respPayload = json.RawMessage(reply)
	} else {
		respPayload, _ = json.Marshal(reply)
	}
	fmt.Printf("[思考] %s\n", truncate(reply, 120))
	return kernel.Message{Type: "chat.response", Payload: respPayload}, nil
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

// extractSessionID 从 Payload 中提取 session_id。
// session_id 是 runtime concept，存储在 Payload 中（L4 层负责），
// 不依赖 L1 kernel.Message。
func extractSessionID(payload []byte) string {
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) == nil {
		if sid, ok := obj["session_id"].(string); ok {
			return sid
		}
	}
	return ""
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
