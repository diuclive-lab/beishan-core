package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"beishan/internal/llm"
	"beishan/internal/llmguard"
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
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = extractSessionID(msg.Payload)
	}
	mode := extractMode(msg.Payload)

	switch mode {
	case ModeReviewExtract:
		return p.handleReviewExtract(userText, sessionID)
	case ModeNoRetrieval:
		return p.handleChatNoRetrieval(msg.Payload, msg.Provider)
	}

	cleanupExpiredPending()

	if isSystemCommand(userText, sessionID) {
		return p.handleSystemCommand(userText, sessionID)
	}
	return p.handleChat(userText, sessionID, mode == ModeTrace, msg.Provider, msg.Payload)
}

// isSystemCommand 判断是否为确定性系统指令（状态机操作，不做检索）。
func isSystemCommand(text, sessionID string) bool {
	return isBatchConfirm(text) ||
		isConfirmReply(sessionID, text) ||
		isForceSaveReply(sessionID, text) ||
		isMergeReply(sessionID, text) ||
		isRejectRemember(sessionID, text) ||
		isCorrectType(sessionID, text) ||
		isReviewTrigger(text) ||
		isListReviews(text) ||
		isConfirmAll(text) ||
		isSkipAll(text)
}

// handleSystemCommand 执行系统指令状态机。
// 不做检索，不调 LLM，纯状态机操作。
func (p *ThinkPlugin) handleSystemCommand(userText, sessionID string) (kernel.Message, error) {
	chatReply := func(s string) (kernel.Message, error) {
		b, _ := json.Marshal(s)
		return kernel.Message{Type: "chat.response", Payload: b}, nil
	}

	// 批量确认（"确认 1,2" 或 "确认 reviewID 1,2"）
	if isBatchConfirm(userText) {
		reviewID, indices, err := parseBatchConfirmWithID(userText)
		if err != nil {
			return chatReply("格式错误，请使用：确认 1,2 或 确认 reviewID 1,2")
		}
		var candidates []KnowledgeCandidate
		if reviewID != "" {
			rf, loadErr := loadReviewFromFile(reviewID)
			if loadErr != nil {
				return chatReply(fmt.Sprintf("找不到审查报告 %s", reviewID))
			}
			candidates = rf.Candidates
		} else {
			pr := getPendingReview(sessionID)
			if pr == nil {
				return chatReply("没有待确认的审查报告，请先运行知识审查工作流")
			}
			candidates = pr.Candidates
			reviewID = sessionID
		}
		var recorded []string
		for _, idx := range indices {
			if idx > 0 && idx <= len(candidates) {
				c := candidates[idx-1]
				res := tools.KnowledgeRemember(c.Title, c.Summary, "", nil, 0)
				recorded = append(recorded, fmt.Sprintf("%d. %s: %s", idx, c.Title, res.Output))
			}
		}
		clearPendingReview(reviewID)
		deleteReviewFile(reviewID)
		return chatReply(fmt.Sprintf("已记录 %d 条知识：\n%s", len(recorded), strings.Join(recorded, "\n")))
	}

	// 单条确认（"确认"/"是"/"yes"，有 pending 时）
	if isConfirmReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			var fcResult *tools.FactCheckResult
			if tools.ContainsVerifiableClaim(pr.Summary) {
				r := tools.FactCheck(pr.Summary)
				fcResult = &r
				if r.Status == "contradicted" {
					return chatReply(fmt.Sprintf("⚠️ 事实核查不通过：%s\n\n实际值：%s\n\n仍需记录？回复「是的，强制记录」", r.Reason, r.Actual))
				}
			}
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.ContentType, pr.Tags, 0)
			if pr.ContentType != "" {
				AppendCalibEvent(CalibrationEvent{ContentType: pr.ContentType, Confidence: pr.Confidence, Title: pr.Title, Action: "confirmed", SessionID: sessionID})
			}
			label := ""
			if fcResult != nil && fcResult.Status == "verified" {
				label = " ✅ 已核查"
			}
			return chatReply(fmt.Sprintf("已记录%s：%s\n%s", label, pr.Title, result.Output))
		}
	}

	// 强制记录（跳过事实核查）
	if isForceSaveReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.ContentType, pr.Tags, 0)
			if pr.ContentType != "" {
				AppendCalibEvent(CalibrationEvent{ContentType: pr.ContentType, Confidence: pr.Confidence, Title: pr.Title, Action: "confirmed", SessionID: sessionID})
			}
			return chatReply(fmt.Sprintf("已强制记录（跳过核查）：%s\n%s", pr.Title, result.Output))
		}
	}

	// 显式拒绝入库
	if isRejectRemember(sessionID, userText) {
		pr := sessionManager.ClearPending(sessionID)
		if pr != nil && pr.ContentType != "" {
			AppendCalibEvent(CalibrationEvent{ContentType: pr.ContentType, Confidence: pr.Confidence, Title: pr.Title, Action: "rejected", SessionID: sessionID})
		}
		return chatReply("已取消，不会入库。")
	}

	// 改正分类后入库
	if isCorrectType(sessionID, userText) {
		pr := sessionManager.ClearPending(sessionID)
		if pr != nil {
			newType := parseCorrectType(userText)
			if newType == "" {
				newType = pr.ContentType
			}
			if pr.ContentType != "" {
				AppendCalibEvent(CalibrationEvent{ContentType: pr.ContentType, Confidence: pr.Confidence, Title: pr.Title, Action: "corrected", SessionID: sessionID})
			}
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, newType, pr.Tags, 0)
			return chatReply(fmt.Sprintf("已修正分类为【%s】并入库：%s\n%s", contentTypeLabel(newType), pr.Title, result.Output))
		}
	}

	// 合并到已有条目
	if isMergeReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			searchResult := tools.KnowledgeSearch(pr.Title)
			var searchOut struct {
				Results []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"results"`
			}
			json.Unmarshal([]byte(searchResult.Output), &searchOut)
			if len(searchOut.Results) == 0 {
				result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.ContentType, pr.Tags, 0)
				return chatReply(fmt.Sprintf("未找到相似条目，已新建：%s\n%s", pr.Title, result.Output))
			}
			newResult := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.ContentType, pr.Tags, 0)
			var newEntry struct{ ID string `json:"id"` }
			json.Unmarshal([]byte(newResult.Output), &newEntry)
			targetID := searchOut.Results[0].ID
			if newEntry.ID != "" && newEntry.ID != targetID {
				mergeResult := tools.KnowledgeMerge(newEntry.ID, targetID)
				return chatReply(fmt.Sprintf("已合并到「%s」：%s", searchOut.Results[0].Title, mergeResult.Output))
			}
			return chatReply(fmt.Sprintf("已记录：%s\n%s", pr.Title, newResult.Output))
		}
	}

	// 知识审查 / 查看报告 / 全部确认 / 跳过
	if isReviewTrigger(userText) {
		return p.triggerReviewWorkflow()
	}
	if isListReviews(userText) {
		return p.handleListReviews()
	}
	if isConfirmAll(userText) {
		return p.handleConfirmAll()
	}
	if isSkipAll(userText) {
		return p.handleSkipAll()
	}

	// 理论上不可达（isSystemCommand 已过滤），保底返回
	return p.handleChat(userText, "", false, "", nil)
}

// handleChatNoRetrieval 处理 workflow 步骤：跳过检索，直接调 LLM。
// 用于需要精确 JSON 输出的步骤（如 classify/evaluate），避免检索上下文干扰。
// Payload 支持 JSON 对象格式：{"message": "...", "system": "..."}，
// 也兼容旧格式纯字符串。
func (p *ThinkPlugin) handleChatNoRetrieval(raw []byte, provider string) (kernel.Message, error) {
	var req struct {
		Message   string `json:"message"`
		System    string `json:"system,omitempty"`
	}
	if len(raw) > 0 && raw[0] == '{' {
		json.Unmarshal(raw, &req)
	}
	if req.Message == "" {
		req.Message = strings.Trim(string(raw), `"`)
	}

	sysPrompt := systemPrompt
	if req.System != "" {
		sysPrompt = req.System
	}

	var reply string
	var usage *llm.Usage
	var err error
	llmTimeout := 120 * time.Second
	if provider == "local" {
		llmTimeout = 600 * time.Second
	}
	// 维度：仅内容（ForContent）。
	// 这是"无检索直答"路径，输出是自然语言回复，无结构、无事实可校验。
	// AntiLazy 基线兜底 → 防止"我会做"、防编造、引用须有源。
	msgs := []llm.ChatMessage{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: req.Message},
	}
	if provider != "" {
		reply, usage, err = llmguard.ChatWithProvider(provider, msgs, llmguard.ForContent(), llmTimeout)
	} else {
		reply, usage, err = llmguard.Chat(msgs, llmguard.ForContent(), llmTimeout)
	}
	llm.RecordUsage("think_no_retrieval", usage)
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
// wantTrace 为 true 时，response payload 包含结构化 retrieval_trace 字段。
func (p *ThinkPlugin) handleChat(userText, sessionID string, wantTrace bool, provider string, payload json.RawMessage) (kernel.Message, error) {
	background := ""

	// 无效输入过滤：纯符号、过短无语义的内容不进检索管道
	if isNonsenseInput(userText) {
		return kernel.Message{
			Type:    "chat.response",
			Payload: json.RawMessage(`"抱歉，我没理解你的意思，能再说清楚一些吗？"`),
		}, nil
	}

	// 项目路径（从环境变量或固定配置）
	projectPath := os.Getenv("BEISHAN_PROJECT_PATH")
	if projectPath == "" {
		projectPath = "."
	}

		// 上下文意图分层：见 intent_keywords.go
		// ctxCurrentSession → 当前 session 刚发生的事（不检索）
		// ctxCrossSession   → 历史 session（episodic 检索）
		isCurrentSessionQ := false
		for _, w := range ctxCurrentSession {
			if strings.Contains(userText, w) {
				isCurrentSessionQ = true
				break
			}
		}

		// 加载最近对话历史（始终加载，不受检索策略影响）
		history := loadRecentSessionMessages(sessionID, 5)

		var (
			results []retrieval.RetrievalResult
			trace   *tools.RetrievalTrace
		)

		// 检索策略分层：
		//   当前 session 查询 → 跳过知识库检索，只跑轻量情景检索
		//   其他（含跨 session 查询）→ 跑完整三柱检索
		if isCurrentSessionQ && len(history) > 0 {
				fmt.Printf("[思考] 当前上下文查询，跳过知识库检索，轻量情景检索\n")

			// 只跑 EpisodicRetrieval（跨 session 情景仍然需要）
			episodic := RunEpisodicRetrieval(userText, 1, nil)
			if len(episodic) > 0 {
				results = episodic
				background = retrieval.FormatForPromptFull(results)
			}
		} else {
			searchQuery := userText
			if needsQueryRewrite(userText) {
				searchQuery = rewriteQuery(userText, sessionID)
			}
			results, trace = RunFullRetrieval(searchQuery, projectPath)
			if len(results) > 0 {
				background = retrieval.FormatForPromptFull(results)
				fmt.Printf("[思考] 检索到 %d 条相关记忆\n", len(results))
			}
			if trace != nil {
				trace.Log()
			}
			if len(history) == 0 {
				if wc := tools.BuildWorkspaceContext(""); wc != "" {
					if background != "" {
						background += "\n\n"
					}
					background += wc
				}
			}
		}


	// 构建多轮对话 messages
	userMsg := userText
	if background != "" {
		userMsg = "请参考以下背景知识回答：\n" + background + "\n\n用户问题：\n" + userText
	} else {
		// 无背景知识时，提示 LLM 在回复末尾附加免责说明
		userMsg = userText + "\n\n（注：本次无背景资料可参考，如有不确定请在回复末尾注明「此回复依赖训练知识，建议核实」）"
	}

	// 注入用户画像到 system prompt
	profilePrompt := tools.ProfileToPrompt()
	sysContent := systemPrompt
	if profilePrompt != "" {
		sysContent += profilePrompt
	}
	messages := []llm.ChatMessage{
		{Role: "system", Content: sysContent},
	}
	// 加载最近 5 轮对话作为上下文（已提前加载，直接用 history 变量）
	if len(history) > 0 {
		// token 截断保护：从最新往前累计，超 8000 rune 时丢弃最早的历史
		const maxHistoryRunes = 8000
		total := 0
		var safe []llm.ChatMessage
		for i := len(history) - 1; i >= 0; i-- {
			size := len([]rune(history[i].Content))
			if total+size > maxHistoryRunes {
				break
			}
			total += size
			safe = append([]llm.ChatMessage{history[i]}, safe...)
		}
		history = safe
		messages = append(messages, history...)
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: userMsg})

	var reply string
	var usage *llm.Usage
	var err error
	// 本地模型需要更长超时（大上下文推理慢）
	llmTimeout := 60 * time.Second
	if provider == "local" {
		llmTimeout = 300 * time.Second
	}
	// 维度：仅内容（ForContent）。
	// 这是路径 B（think_plugin 对话分发）唯一的 LLM 主调用 — 自然语言聊天，
	// 输出是非结构化文本。AntiLazy 基线兜底 → 防"我会做"、防编造、引用须有源。
	// provider 路径走 ChatWithProvider，共享同一契约；
	// 默认路径走 Chat，两者共享 chatCore 的校验+重试+critique 逻辑。
	if provider != "" {
		reply, usage, err = llmguard.ChatWithProvider(provider, messages, llmguard.ForContent(), llmTimeout)
	} else {
		reply, usage, err = llmguard.Chat(messages, llmguard.ForContent(), llmTimeout)
	}
	llm.RecordUsage("think", usage)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	// 输出质量门禁：检测 LLM 回复中的可验证事实
	allChecks := tools.StockCodeVerify(reply)
	allChecks = append(allChecks, tools.DateVerify(reply)...)
	allChecks = append(allChecks, tools.NumberRangeVerify(reply)...)
	allChecks = append(allChecks, tools.URLVerify(reply)...)
	for _, c := range allChecks {
		if c.Status == "contradicted" {
			suffix := c.Reason
			if c.Actual != "" {
				suffix += fmt.Sprintf("（实际值：%s）", c.Actual)
			}
			reply += fmt.Sprintf("\n\n⚠️ 事实修正：%s", suffix)
		}
	}

		// LLM 工具建议解析：LLM 可在回复中嵌入 JSON 表示需要调工具
		if suggestions := parseToolSuggestions(reply); len(suggestions) > 0 {
			// 从可见回复中去掉 JSON 块
			for _, sug := range suggestions {
				jsonMarker := fmt.Sprintf(`"tool":"%s"`, sug.Tool)
				if idx := strings.Index(reply, jsonMarker); idx >= 0 {
					braceStart := strings.LastIndex(reply[:idx], "{")
					// braceEnd 指向 } 的下一个位置
					braceEnd := strings.Index(reply[idx:], "}") + idx + 1
					if braceStart >= 0 && braceEnd > braceStart {
						prefix := strings.TrimRight(reply[:braceStart], " \n\r")
						suffix := strings.TrimLeft(reply[braceEnd:], " \n\r")
						reply = prefix + "\n" + suffix
					}
				}
				fmt.Printf("[思考] LLM 建议调工具: %s.%s \u2014 %s\n", sug.Tool, sug.Action, sug.Reason)
			}
			// 执行工具建议，收集结果后经 LLM 合成为自然语言
			var toolContents []string
			for _, sug := range suggestions {
				resp, err := p.Kernel.Call(kernel.Message{
					Recipient: sug.Tool,
					Type:      sug.Action,
					Payload:   payload,
				}, 60*time.Second)
				if err != nil {
					toolContents = append(toolContents, fmt.Sprintf("[%s 执行失败]: %v", sug.Action, err))
				} else {
					resultStr := string(resp.Payload)
					content := extractToolContent(resultStr)
					toolContents = append(toolContents, fmt.Sprintf("[%s]\n%s", sug.Action, content))
					// 原始结果写入 session，供下一轮 LLM 引用
					if sessionID != "" {
						toolResultPayload := fmt.Sprintf(`{"tool":"%s","action":"%s","result":%s}`, sug.Tool, sug.Action, resultStr)
						p.Kernel.Send(kernel.Message{
							Recipient: "memory_plugin",
							Type:      "session_add",
							Payload:   json.RawMessage(fmt.Sprintf(`{"session_id":"%s","role":"system","type":"tool_result","payload":%s}`, sessionID, toolResultPayload)),
						})
					}
				}
			}
			if len(toolContents) > 0 {
				// 维度：仅内容（ForContent）。
				// LLM 合成：把工具的 JSON 结果改写为自然语言。输出无结构、无事实可校验，
				// AntiLazy 基线防止 LLM 偷懒拼接原始 JSON 或编造工具没返回的内容。
				synthesisPrompt := fmt.Sprintf("用户的问题：%s\n\n工具执行结果：\n%s\n\n请根据以上结果用清晰自然的语言回答用户，不要展示 JSON 或技术细节。",
					userText, strings.Join(toolContents, "\n\n"))
				synthesized, synthUsage, synthErr := llmguard.Chat([]llm.ChatMessage{
					{Role: "system", Content: systemPrompt},
					{Role: "user", Content: synthesisPrompt},
				}, llmguard.ForContent(), 60*time.Second)
				llm.RecordUsage("tool_synthesis", synthUsage)
				if synthErr == nil && strings.TrimSpace(synthesized) != "" {
					reply = strings.TrimSpace(synthesized)
				} else {
					// 降级：直接拼接清理后的内容
					reply += "\n\n---\n" + strings.Join(toolContents, "\n")
				}
			}
		}

	// Suggest-to-Remember（LLM 分类器 + 自动化门槛）
	if ct, title, summary, confidence := classifyForKnowledge(userText, reply); ct != "" {
		if title == "" {
			title = truncate(userText, 20)
		}
		if summary == "" {
			summary = truncate(reply, 80)
		}
		label := contentTypeLabel(ct)
		if IsAutoMode(ct) {
			// 精度已达阈值：直接入库，不打扰用户
			tools.KnowledgeRemember(title, summary, ct, nil, 0)
			AppendCalibEvent(CalibrationEvent{ContentType: ct, Confidence: confidence, Title: title, Action: "auto_confirmed", SessionID: sessionID})
			reply += fmt.Sprintf("\n\n---\n✓ 已自动入库【%s】：%s", label, title)
			fmt.Printf("[思考] 自动入库: type=%s title=%s\n", ct, title)
		} else {
			pr := createPendingRemember(sessionID, title, summary)
			pr.ContentType = ct
			pr.Confidence = confidence
			reply += fmt.Sprintf("\n\n---\n发现一条【%s】记录，是否入库？\n> %s\n\n回复「确认」/「不用了」/「改成工作记录/决策/教训/事实」（置信度 %.0f%%）", label, title, confidence*100)
			fmt.Printf("[思考] 知识分类: type=%s title=%s confidence=%.2f session=%s\n", ct, title, confidence, sessionID)
		}
	}

	// 结构化 trace 模式：跳过文本 trace，返回结构化数据
	if wantTrace && trace != nil && len(trace.Stages) > 0 {
		tracePayload, _ := json.Marshal(map[string]interface{}{
			"reply":           reply,
			"retrieval_trace": trace,
		})
		fmt.Printf("[思考] %s\n", truncate(reply, 120))
		return kernel.Message{Type: "chat.response", Payload: tracePayload}, nil
	}


	isJSON := len(reply) > 0 && reply[0] == '{' && json.Valid([]byte(reply))
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

当用户需要上述能力时，你可以主动调用工具。
在回复末尾添加一行 JSON 来描述需要调用的工具：
{"tool":"插件名","action":"消息类型","reason":"工具用途"}

支持的工具调用：
- 搜索网络：search_plugin → web_search / web_fetch
- 读写文件：write_plugin → write_file / read_file
- 执行命令：terminal_plugin → terminal_exec
- 浏览网页：browser_plugin → browser_navigate

对于你能直接回答的问题（闲聊、知识、创作等），直接回答。
不需要说明自己是AI助手。`

// callDeepSeek 调 LLM API 生成回答。
// 使用共享 llm.ChatCompletion（不经过 think_plugin 自身路径，避免递归）。
// background 为检索到的知识上下文，为空时不注入。
// toolSuggestion is a structured tool invocation request from LLM.
type toolSuggestion struct {
	Tool    string `json:"tool"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
}

// parseToolSuggestions 从 LLM 回复末尾提取工具建议 JSON。
// 系统 prompt 要求 LLM 在回复末尾追加：
//   {"tool":"插件名","action":"消息类型","reason":"工具用途"}
// 从最后一个 { 开始向后扫描，找到完整的 JSON 对象并校验 tool 字段非空。
func parseToolSuggestions(text string) []toolSuggestion {
	// 从末尾往前找最后一个 {，避免误匹配正文中的 JSON
	braceStart := strings.LastIndex(text, `{"tool":`)
	if braceStart < 0 {
		return nil
	}
	// 找对应的闭合 }
	braceEnd := strings.Index(text[braceStart:], "}")
	if braceEnd < 0 {
		return nil
	}
	sugText := text[braceStart : braceStart+braceEnd+1]
	var sug toolSuggestion
	if json.Unmarshal([]byte(sugText), &sug) == nil && sug.Tool != "" && sug.Action != "" {
		return []toolSuggestion{sug}
	}
	return nil
}

// extractToolContent 从工具响应 JSON 中提取可读内容，剥掉协议外壳。
// 支持：glue payload 包装、data 数组、results 数组、HTML 页面内容。
func extractToolContent(raw string) string {
	var obj map[string]interface{}
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return truncate(raw, 400)
	}

	// 剥 glue 协议外壳：{"payload": {...}}
	if inner, ok := obj["payload"]; ok {
		if m, ok := inner.(map[string]interface{}); ok {
			obj = m
		}
	}

	// 剥 data 数组（浏览器插件格式：{"success":true,"data":[{...}]}）
	if arr, ok := obj["data"].([]interface{}); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]interface{}); ok {
			obj = m
		}
	}

	// 优先提取 content/text/output/answer 字段
	for _, key := range []string{"content", "text", "output", "answer", "summary"} {
		if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
			s := strings.TrimSpace(v)
			// HTML 页面：剥掉标签，只保留文字
			if strings.Contains(s[:min(len(s), 200)], "<html") ||
				strings.Contains(s[:min(len(s), 200)], "<!DOCTYPE") {
				s = stripHTML(s)
			}
			return truncate(s, 1500)
		}
	}

	// results 数组（搜索结果）格式化为列表
	if arr, ok := obj["results"].([]interface{}); ok && len(arr) > 0 {
		var lines []string
		for i, item := range arr {
			if i >= 8 {
				break
			}
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			title, _ := m["title"].(string)
			snippet, _ := m["snippet"].(string)
			url, _ := m["url"].(string)
			if title != "" {
				line := fmt.Sprintf("• %s", title)
				if snippet != "" {
					line += "\n  " + truncate(snippet, 120)
				}
				if url != "" {
					line += "\n  " + url
				}
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}

	// 兜底：找第一个较长的字符串字段
	for _, v := range obj {
		if s, ok := v.(string); ok && len([]rune(s)) > 30 {
			return truncate(s, 400)
		}
	}
	return truncate(raw, 400)
}

// stripHTML 粗糙地剥掉 HTML 标签，提取可读文字。
// 不依赖任何外部库，够用即可。
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteRune(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	// 压缩连续空白
	result := strings.Join(strings.Fields(b.String()), " ")
	return result
}

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

// stripRetrievalTrace 去掉 assistant 回复中的检索过程后缀。
// 标记：\n\n---\n 或 \n\n*检索过程* 或 \n\n**L0_
func stripRetrievalTrace(s string) string {
	for _, marker := range []string{"\n\n---\n", "\n\n*检索过程*", "\n\n**L0_"} {
		if idx := strings.Index(s, marker); idx > 0 {
			return s[:idx]
		}
	}
	return s
}

// loadRecentSessionMessages 从会话文件加载最近 N 轮对话，用于多轮上下文。
// role 映射：user → "user"，其他（插件响应）→ "assistant"。
func rawMsg(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func loadRecentSessionMessages(sessionID string, limit int) []llm.ChatMessage {
	if sessionID == "" || limit <= 0 {
		return nil
	}
	res := tools.SessionGet(sessionID)
	if !res.Success || res.Output == "" {
		return nil
	}
	data := []byte(res.Output)
	var session struct {
		Messages []struct {
			Role    string `json:"role"`
			Type    string `json:"type"`
			Payload string `json:"payload"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return nil
	}

	// 取最后 2*limit 条消息（limit 轮 = limit 条 user + limit 条 assistant）
	msgs := session.Messages
	start := 0
	if len(msgs) > 2*limit {
		start = len(msgs) - 2*limit
	}

	var result []llm.ChatMessage
	for _, m := range msgs[start:] {
		// 跳过 session_add 等系统消息
		if m.Type != "chat" && m.Type != "chat.response" {
			continue
		}
		content := m.Payload
		// user 消息的 payload 是 JSON {"message":"..."}，需要提取
		if m.Role == "user" {
			var obj map[string]interface{}
			if json.Unmarshal([]byte(content), &obj) == nil {
				if msg, ok := obj["message"].(string); ok {
					content = msg
				}
			}
		}
		role := "assistant"
		if m.Role == "user" {
			role = "user"
		}
		// assistant 消息去掉 retrieval trace 后缀，减少上下文 token 消耗
		if role == "assistant" {
			content = stripRetrievalTrace(content)
		}
		result = append(result, llm.ChatMessage{Role: role, Content: content})
	}
	return result
}

// ─── Query 改写（口语化 → 精确检索词）───────────────────

// needsQueryRewrite 判断查询是否需要改写（确定性检测）。
// 口语指代词（ctxVagueRef）或跨 session 引用词（ctxCrossSession）时改写。
func needsQueryRewrite(query string) bool {
	for _, p := range ctxVagueRef {
		if strings.Contains(query, p) {
			return true
		}
	}
	for _, p := range ctxCrossSession {
		if strings.Contains(query, p) {
			return true
		}
	}
	return false
}

// rewriteQuery 用 LLM 将口语化查询改写为精确检索关键词。
// 失败时静默降级返回原查询。
func rewriteQuery(query string, sessionID string) string {
	// 加载最近 3 轮对话作为改写上下文
	var contextLines []string
	if history := loadRecentSessionMessages(sessionID, 3); len(history) > 0 {
		for _, m := range history {
			contextLines = append(contextLines, m.Content)
		}
	}

	contextBlock := ""
	if len(contextLines) > 0 {
		contextBlock = "\n\n最近对话上下文：\n" + strings.Join(contextLines, "\n")
	}

	prompt := fmt.Sprintf(`将以下用户口语化查询改写为精确的知识库检索关键词。
要求：
- 去掉指代不明的词（"那个""这个""上次"）
- 保留核心实体和主题
- 输出 3-5 个空格分隔的检索关键词
- 不要解释，只输出关键词

用户查询：%s%s`, query, contextBlock)

	// 维度：零契约（Contract{}）。
	// 这是机械变换 — "口语化查询 → 检索关键词"，没有内容质量的发挥空间，
	// 也没有事实/结构可校验。降级路径已存在（err 或空串 → 返回原查询），
	// 走零契约能省去基线注入的 token 开销。
	reply, usage, err := llmguard.Chat([]llm.ChatMessage{
		{Role: "system", Content: "你是查询改写器。只输出检索关键词，不要解释。"},
		{Role: "user", Content: prompt},
	}, llmguard.Contract{}, 10*time.Second)
	llm.RecordUsage("query_rewrite", usage)

	if err != nil || strings.TrimSpace(reply) == "" {
		return query // 降级：用原查询
	}

	rewritten := strings.TrimSpace(reply)
	// 清理可能的 markdown 标记
	rewritten = strings.TrimPrefix(rewritten, "```")
	rewritten = strings.TrimSuffix(rewritten, "```")
	rewritten = strings.TrimSpace(rewritten)

	fmt.Printf("[query_rewrite] %q → %q\n", query, rewritten)
	return rewritten
}

// isNonsenseInput 判断用户输入是否为无意义内容（纯符号、过短无语义）
func isNonsenseInput(s string) bool {
	s = strings.TrimSpace(s)
	// 常见确认词允许通过
	allowList := []string{"ok", "okay", "好的", "是的", "对", "是", "好", "嗯", "确认", "继续", "知道了"}
	for _, w := range allowList {
		if strings.EqualFold(s, w) {
			return false
		}
	}
	// 过短且不含中文
	if len([]rune(s)) <= 3 {
		hasChinese := false
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				hasChinese = true
				break
			}
		}
		if !hasChinese {
			return true
		}
	}
	// 纯标点符号
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return true
	}
	return false
}
