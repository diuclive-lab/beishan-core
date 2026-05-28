package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"beishan/glue"
	"beishan/internal/discovery"
	"beishan/internal/llm"
	"beishan/internal/agent"
	"beishan/internal/observatory"
	"beishan/internal/tools"
	"beishan/internal/rightflower"

	"beishan/internal/workflow"
	"beishan/kernel"
	"beishan/plugins"
)

//go:embed web/index.html
var IndexHTML string

//go:embed web/dashboard.html
var DashboardHTML string

//go:embed web/chat.html
var ChatHTML string
func init() {
	_ = IndexHTML
	if f, err := os.Open(".env"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				if os.Getenv(key) == "" {
					os.Setenv(key, val)
				}
			}
		}
	}
}

func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

var startTime = time.Now()

// chatCounter 统计对话轮次，每 10 轮触发一次习惯推断
var chatCounter int64

func main() {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("请设置环境变量 LLM_API_KEY 或 DEEPSEEK_API_KEY")
	}

	k := kernel.NewKernel(apiKey)

	tools.Init()

	// ─── 注册 Go 插件 ─────────────────────────────

	k.Register("search_plugin", &plugins.SearchPlugin{Kernel: k}, kernel.Meta{
		Description: "通用网络搜索，适用于查找资料、新闻、技术文档",
		Tags:        []string{"search", "retrieval"},
		Types:       []string{"web_search", "web_fetch", "web_extract", "web_render"},
		Example:     `type="web_search" payload={"query":"Go语言热门项目2026"}`,
	})
	k.Register("write_plugin", &plugins.WritePlugin{}, kernel.Meta{
		Description: "长文本生成、格式化写作、文件处理，适合：写文章、写报告、写代码、解析文件。不适合：输出JSON、做逻辑判断",
		Tags:        []string{"file", "filesystem"},
		Types:       []string{"write_file", "read_file", "search_files", "patch", "file_parse"},
		Example:     `type="write_file" payload={"path":"test.md","content":"hello"}`,
	})
	k.Register("memory_plugin", &plugins.MemoryPlugin{}, kernel.Meta{
		Description: "会话记忆管理，存储和召回跨轮上下文信息",
		Tags:        []string{"memory", "session"},
		Types:       []string{"session_add", "session_get", "session_search", "session_list", "session_delete", "session_cleanup", "evidence_add", "evidence_search", "knowledge_add", "knowledge_search", "knowledge_list", "knowledge_get", "knowledge_delete", "knowledge_update", "knowledge_suggest_links", "knowledge_dedupe", "knowledge_merge", "knowledge_confirm_links", "knowledge_remember", "knowledge_reindex", "knowledge_embed", "knowledge_embed_all", "knowledge_semantic_search", "knowledge_topic_map", "knowledge_timeline", "system_info", "stock_quote", "stock_multi_quote",
		"github_readme",
		"code_security_check",
		"code_read", "code_diff", "code_apply", "code_rollback",
		"rss_fetch", "rss_default",
		"profile_show", "profile_update",
		"knowledge_history", "knowledge_version_get", "knowledge_heal", "knowledge_feedback", "knowledge_export", "knowledge_import", "kb_dedup",
		"session_summarize",
		"kb_audit", "kb_repair",
		"image_generate", "image_to_image",
		"prompt_engineer", "prompt_analyze", "prompt_style_list"},
		Example:     `type="knowledge_add" payload={"source_type":"web","title":"标题","summary":"摘要"} 或 type="knowledge_search" payload={"keyword":"Go语言"} 或 type="knowledge_remember" payload={"title":"事实","summary":"内容"}`,
	})
	k.Register("terminal_plugin", &plugins.TerminalPlugin{}, kernel.Meta{
		Description: "本地终端命令执行，执行 shell 命令和管理后台进程",
		Tags:        []string{"terminal", "shell"},
		Types:       []string{"terminal_exec", "terminal_list", "terminal_poll", "terminal_kill"},
		Example:     `type="terminal_exec" payload={"command":"ls -la"}`,
	})
	k.Register("browser_plugin", &plugins.BrowserPlugin{}, kernel.Meta{
		Description: "浏览器自动化，导航、点击、滚动、提取网页内容",
		Tags:        []string{"browser", "web"},
		Types:       []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_scroll", "browser_back"},
		Example:     `type="browser_navigate" payload={"url":"https://example.com"}`,
	})
	k.Register("session_search_plugin", &plugins.SessionSearchPlugin{}, kernel.Meta{
		Description: "历史会话搜索，按关键词搜索所有存储的对话记录",
		Tags:        []string{"session", "search"},
		Types:       []string{"session_search", "session_list"},
	})
	k.Register("todo_plugin", &plugins.TodoPlugin{}, kernel.Meta{
		Description: "待办事项管理，添加、列出、标记完成、清除任务",
		Tags:        []string{"todo", "task"},
		Types:       []string{"todo_list", "todo_add", "todo_done", "todo_clear", "todo_by_source"},
		Example:     `type="todo_list" 或 type="todo_add" payload={"todos":["买牛奶"]} 或 type="todo_done" payload={"id":1}`,
	})
	k.Register("tts_plugin", &plugins.TTSPlugin{}, kernel.Meta{
		Description: "文本转语音（TTS），使用系统引擎把文字转为音频文件",
		Tags:        []string{"tts", "audio"},
		Types:       []string{"text_to_speech"},
	})
	k.Register("image_gen_plugin", &plugins.ImageGenPlugin{}, kernel.Meta{
		Description: "AI 图片生成，根据文字描述生成图片。预留接口，需配置外部 API",
		Tags:        []string{"image", "generate"},
		Types:       []string{"image_generate"},
	})
	k.Register("think_plugin", &plugins.ThinkPlugin{Kernel: k}, kernel.Meta{
		Description: "推理、分析、判断、结构化输出JSON，适合：分析代码、提取字段、做决策、生成大纲。不适合：直接生成长文本",
		Tags:        []string{"chat", "dialogue", "general"},
		Types:       []string{"chat"},
		Example:     `type="chat" payload={"message":"你好"}`,
	})
	schedulerPlugin := plugins.NewScheduler(k)
	k.Register("scheduler_plugin", schedulerPlugin, kernel.Meta{
		Description: "多步任务编排，适用于需要多个插件协作的复杂任务",
		Tags:        []string{"orchestration", "planning"},
	})

	// 注册默认定时任务
	defaultSchedule, _ := json.Marshal(map[string]string{
		"name":     "kb_hygiene_weekly",
		"workflow": "kb_hygiene",
		"cron":     "0 3 * * 0", // 每周日 03:00
	})
	schedulerPlugin.OnMessage(kernel.Message{Type: "schedule_add", Payload: defaultSchedule})

	observerSchedule, _ := json.Marshal(map[string]string{
		"name":     "agent_observer_daily",
		"workflow": "agent_observer",
		"cron":     "0 8 * * 1-5", // 工作日 08:00
	})
	schedulerPlugin.OnMessage(kernel.Message{Type: "schedule_add", Payload: observerSchedule})

	healSchedule, _ := json.Marshal(map[string]string{
		"name":     "knowledge_heal_weekly",
		"workflow": "knowledge_heal",
		"cron":     "0 4 * * 1", // 每周一 04:00
	})
	schedulerPlugin.OnMessage(kernel.Message{Type: "schedule_add", Payload: healSchedule})

	backupSchedule, _ := json.Marshal(map[string]string{
		"name":     "kb_backup_daily",
		"workflow": "kb_backup",
		"cron":     "0 2 * * *", // 每日 02:00
	})
	schedulerPlugin.OnMessage(kernel.Message{Type: "schedule_add", Payload: backupSchedule})

	// 每周日 03:00 运行检索质量探针，测量 L0/L1 recall@3 并追加到 probe_history.jsonl
	probeSchedule, _ := json.Marshal(map[string]string{
		"name":     "knowledge_probe_weekly",
		"workflow": "knowledge_probe",
		"cron":     "0 3 * * 0", // 每周日 03:00
	})
	schedulerPlugin.OnMessage(kernel.Message{Type: "schedule_add", Payload: probeSchedule})
	k.Register("codex_plugin", &plugins.CodexSessionPlugin{}, kernel.Meta{
		Description: "Codex 对话导入：列出和提取本地 Codex 对话，用于知识库入库",
		Tags:        []string{"codex", "import"},
		Types:       []string{"codex_session_list", "codex_session_extract"},
	})

	k.Register("claude_plugin", &plugins.ClaudePlugin{}, kernel.Meta{
		Description: "Claude 记忆导入：列出和导入 Claude 记忆文件到知识库",
		Tags:        []string{"claude", "memory", "import"},
		Types:       []string{"claude_memory_list", "claude_memory_import"},
	})

	k.Register("notify_plugin", &plugins.NotifyPlugin{}, kernel.Meta{
		Description: "通知发送：邮件/Slack/企业微信，适用于 workflow 执行完成后推送结果",
		Tags:        []string{"notify", "push"},
		Types:       []string{"notify_send"},
	})

	k.Register("skill_factory_plugin", plugins.NewSkillFactory(k, "./workflows"), kernel.Meta{
		Description: "技能工场，根据自然语言描述自动生成 YAML 工作流",
		Tags:        []string{"skill", "workflow", "generation"},
		Types:       []string{"skill_create", "skill_list", "skill_view", "skill_delete", "skill_preview", "skill_evaluate"},
	})

	// 法律审查插件簇（L3 执行插件，编排由 workflow_plugin 处理）
	k.Register("cold_start_plugin", &plugins.ColdStartPlugin{}, kernel.Meta{
		Description: "合同冷启动识别，提取合同类型和法律领域",
		Tags:        []string{"legal", "classification"},
		Types:       []string{"cold_start"},
	})
	k.Register("legal_search_plugin", &plugins.LegalSearchPlugin{}, kernel.Meta{
		Description: "法律条文检索，查询法律法规和判例",
		Tags:        []string{"legal", "search"},
		Types:       []string{"legal_search"},
	})
	k.Register("clause_analyzer_plugin", &plugins.ClauseAnalyzerPlugin{}, kernel.Meta{
		Description: "合同条款分析，三段论推理合法性和风险等级",
		Tags:        []string{"legal", "analysis"},
		Types:       []string{"clause_analysis"},
	})
	k.Register("legal_write_plugin", &plugins.LegalWritePlugin{}, kernel.Meta{
		Description: "法律审查报告生成，输出结构化审查结论",
		Tags:        []string{"legal", "write"},
		Types:       []string{"legal_generate_report", "legal_write_opinion"},
	})
	// ─── Go-DSL 工作流注册 ─────────────────────────
	// toolHost: tool 名 → 宿主插件名
	toolHost := map[string]string{
		"web_search":   "search_plugin",
		"web_fetch":    "search_plugin",
		"web_extract":  "search_plugin",
		"web_render":   "search_plugin",
		"write_file":   "write_plugin",
		"read_file":    "write_plugin",
		"search_files": "write_plugin",
		"patch":        "write_plugin",
		"file_parse":   "write_plugin",
	}
	ensureTool := func(name string) bool {
		_, ok := tools.GetToolSchema(name)
		return ok
	}

	// 示例：用 NewGoToolPlugin 注册简单 L3 插件
	_ = workflow.NewGoToolPlugin(k, toolHost, ensureTool, map[string]string{
		"web_search": "web_search",
	})

	// 注册 Go-DSL legal_review（等价于 YAML 版 legal_review）
	registerLegalReviewGoDSL(k, toolHost, ensureTool)
	// 工作流引擎
	wfEngine := workflow.New(k, "./workflows")
	k.Register("workflow_plugin", &plugins.WorkflowPlugin{Engine: wfEngine}, kernel.Meta{
		Description: "工作流引擎，执行 workflows/*.yaml 定义的多步骤编排任务，包括法律审查、尽职调查等场景",
		Tags:        []string{"workflow", "orchestration", "legal"},
		Types:       []string{"workflow_run"},
	})

		wfEngine.InitEventSubscriptions()

	// 扫描 workflows/ 目录，注入 Router workflow 摘要
	workflowDir := "./workflows"
	workflowSummary := buildWorkflowSummary(workflowDir)
	k.Router.SetWorkflowSummary(workflowSummary)

	// 加载右花（外部工具注册）
	if err := rightflower.RegisterAll(k, "./right_flowers"); err != nil {
		log.Printf("[rightflower] 加载失败: %v（右花为可选能力，继续启动）", err)
	}

	// ─── Observatory Trace Recorder ──────────────
	recorderPath := filepath.Join("eval", "run", "traces")
	os.MkdirAll(recorderPath, 0o755)
	// Agent registry + tools
	agent.Register(agent.Definition{
		ID:           "researcher",
		Description:  "Research using web search tools",
		SystemPrompt: "You are a research assistant. Use web search to find information. Cite sources.",
		Tools:        []string{"web_search", "web_fetch"},
		MaxIterations: 5,
	})
	agent.Register(agent.Definition{
		ID:           "summarizer",
		Description:  "Summarize files and text into concise structured output",
		SystemPrompt: "You are a summarization specialist. Read content and produce clear, concise summaries.",
		Tools:        []string{"read_file", "search_files", "grep"},
		MaxIterations: 4,
	})
	// MCP skills: 当前无外部 MCP server 接入，框架保留供后续使用

	log.Printf("[main] agent registry: %d definitions", len(agent.List()))
	// Register per-agent delegation tools (delegate_to_researcher, etc.)
	for _, aid := range agent.List() {
		def, _ := agent.Get(aid)
		tname := def.ToolName()
		tdesc := def.ToolDescription()
		tools.Register(tname, tdesc,
			map[string]interface{}{
				"type":     "object",
				"required": []string{"prompt"},
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Clear task description with all necessary context",
					},
				},
			},
			func(args map[string]interface{}) *tools.ToolResult {
				prompt := ""
				if p, ok := args["prompt"].(string); ok {
					prompt = p
				}
				def, ok := agent.Get(aid)
				if !ok {
					return tools.ErrorResult("agent not found: " + aid)
				}
				result := agent.RunSubagent(context.Background(), "delegate-"+aid, prompt, def, 120*time.Second)
				if result.Error != "" {
					return tools.ErrorResult(result.Error)
				}
				return tools.SuccessResult(result.Output)
			},
		)
		log.Printf("[agent] delegation tool registered: %s", tname)
	}

	// Register agent event subscriber for conversation persistence
	observatory.Subscribe("agent.complete", func(evt observatory.Event) {
		if data, ok := evt.Data.(observatory.AgentCompleteData); ok {
			log.Printf("[events] DEBUG: agent=%s iter=%d msgs=%d", data.AgentID, data.Iterations, len(data.Messages))
			log.Printf("[events] agent %s completed: %d iter in %dms, output=%d chars", data.AgentID, data.Iterations, data.ElapsedMs, len(data.Output))
			// Persist conversation for backtracking
			if len(data.Messages) > 0 {
				convPath := filepath.Join("eval", "run", "conversations")
				os.MkdirAll(convPath, 0755)
				fname := fmt.Sprintf("%s_%s.json", data.AgentID, evt.Timestamp.Format("20060102_150405"))
				conv := map[string]interface{}{
					"agent_id":   data.AgentID,
					"iterations": data.Iterations,
					"elapsed_ms": data.ElapsedMs,
					"output":     data.Output,
					"messages":   data.Messages,
				}
				// Write asynchronously — disk I/O should not block agent completion
				go func(path, name string, content map[string]interface{}) {
					if f, err := os.Create(path); err == nil {
						json.NewEncoder(f).Encode(content)
						f.Close()
						log.Printf("[events] conversation saved: %s", name)
					}
				}(filepath.Join(convPath, fname), fname, conv)
			}
		}
	})
	observatory.Subscribe("agent.failed", func(evt observatory.Event) {
		if data, ok := evt.Data.(observatory.AgentFailedData); ok {
			log.Printf("[events] agent %s failed: %s", data.AgentID, data.Error)
			// 写告警 trace，让 /metrics 和 eval/run/traces 可观测到失败事件
			observatory.RecordTrace(observatory.Trace{
				ID:          newSessionID(),
				Mode:        "agent",
				Route:       "agent.failed",
				Plugin:      data.AgentID,
				Status:      "failed",
				RouteReason: data.Error,
			})
		}
	})

	tools.AgentSpawn = func(agentID, prompt string, timeout time.Duration) *tools.ToolResult {
		def, ok := agent.Get(agentID)
		if !ok {
			return tools.ErrorResult(fmt.Sprintf("agent %q not found. Available: %s", agentID, strings.Join(agent.List(), ", ")))
		}
		result := agent.RunSubagent(context.Background(), "spawn-"+agentID, prompt, def, timeout)
		if result.Error != "" {
			return tools.ErrorResult(result.Error)
		}
		return tools.SuccessResult("[subagent " + agentID + "]:\n" + result.Output)
	}
	tools.AgentParallel = func(tasksJSON string) *tools.ToolResult {
		var tasks []agent.ParallelTask
		if err := json.Unmarshal([]byte(tasksJSON), &tasks); err != nil {
			return tools.ErrorResult("parse error: " + err.Error())
		}
		results := agent.RunParallel(context.Background(), tasks, 120*time.Second)
		var out []string
		for _, r := range results {
			if r.Error != "" {
				out = append(out, fmt.Sprintf("[%s] ERROR: %s", r.AgentID, r.Error))
			} else {
				out = append(out, fmt.Sprintf("[%s] %s", r.AgentID, r.Output))
			}
		}
		return tools.SuccessResult(strings.Join(out, "\n---\n"))
	}
	tools.RegisterAgentTools()

		observatory.InitEvents(filepath.Join("eval", "run", "events"))

		observatory.SetDefaultRecorder(observatory.NewPersistentRecorder(
		filepath.Join(recorderPath, fmt.Sprintf("traces_%s.jsonl", time.Now().Format("20060102")))))

	// ─── 本地引擎扫描 + 自动故障切换 ────────────────
	engines := discovery.ScanWithModel(2 * time.Second)
	if len(engines) > 0 {
		engine, ok := selectFallbackEngine(engines)
		if ok {
			log.Printf("[main] 发现本地推理引擎:%s", discovery.Summary(engines))
			log.Printf("[main] 故障切换候选: %s (%s)", engine.Name, engine.Endpoint)

			state := discovery.NewStrategyState()
			go monitorFailover(k, state, engine)
		}
	} else {
		log.Println("[main] 未发现本地推理引擎，故障切换不可用")
	}

	// 启动胶水层
	gl := glue.New(k, "./plugins")
	if err := gl.Start(); err != nil {
		log.Fatalf("胶水层启动失败: %v", err)
	}

	// ─── Session 结果队列 ──────────────────────────

	var sessionResults sync.Map

	k.SessionHandler = func(sessionID string, msg kernel.Message) {
		sessionResults.Store(sessionID, msg)
		log.Printf("[main] session 结果已存储: %s", sessionID)
	}

	// ─── Embedding sidecar ──────────────────────────
	// 启动 nomic-embed-text-v1.5 作为 embedding 推理服务
	embeddingModel := "/Users/dc/.lmstudio/.internal/bundled-models/nomic-ai/nomic-embed-text-v1.5-GGUF/nomic-embed-text-v1.5.Q4_K_M.gguf"
	embeddingPort := 8092
	if err := gl.StartSidecar("nomic-embed", "llama-server", []string{
		"--embeddings", "--pooling", "mean",
		"--model", embeddingModel,
		"--port", fmt.Sprintf("%d", embeddingPort),
		"--api-key", "local-dev",
	}, embeddingPort); err != nil {
		log.Printf("[main] embedding sidecar 启动失败: %v（语义搜索不可用）", err)
	} else {
		// 等待 sidecar 就绪
		for i := 0; i < 15; i++ {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", embeddingPort), 1*time.Second)
			if err == nil {
				conn.Close()
				tools.SetEmbeddingEndpoint(fmt.Sprintf("http://127.0.0.1:%d/v1/embeddings", embeddingPort))
				os.Setenv("EMBEDDING_API_KEY", "local-dev")
				log.Printf("[main] embedding sidecar 就绪，语义搜索已启用")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// ─── HTTP API ──────────────────────────────────

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		var snapshot map[string]interface{}
		json.Unmarshal(observatory.CollectSnapshotJSON(), &snapshot)
		if snapshot == nil {
			snapshot = make(map[string]interface{})
		}
		snapshot["knowledge_calibration"] = plugins.CalibStatus()
		data, _ := json.MarshalIndent(snapshot, "", "  ")
		w.Write(data)
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := map[string]interface{}{
			"uptime_seconds": time.Since(startTime).Seconds(),
			"tools":          len(tools.Registry),
			"plugins":        k.KnownPlugins(),
			"observatory":    json.RawMessage(observatory.CollectSnapshotJSON()),
			"subprocesses":   gl.ProcStatus(),
			"right_flowers":  gl.RightFlowerStatus(),
			"calibration":    plugins.CalibStatus(),
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		w.Write(data)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(IndexHTML))
	})

	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(ChatHTML))
})

mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(DashboardHTML))
	})

	saveToSession := func(sessionID, role, msgType string, payload []byte) {
		s := string(payload)
		s = strings.Trim(s, `"`)
		escaped := jsonEscape(s)
		body := fmt.Sprintf(
			`{"session_id":"%s","role":"%s","type":"%s","payload":"%s"}`,
			sessionID, role, msgType, escaped)
		k.Send(kernel.Message{
			Recipient: "memory_plugin",
			Type:      "session_add",
			Payload:   json.RawMessage(body),
		})
	}

	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var raw map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}

		// 允许客户端传入 session_id 以维持会话连续性（如 Suggest-to-Remember 确认流程）
		// 未传入时自动生成新 sessionID
		sessionID := ""
		if sid, ok := raw["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
		if sessionID == "" {
			sessionID = newSessionID()
		}
		msg := kernel.Message{Sender: "user"}
		async := false

		msg.SessionID = sessionID

	if txt, ok := raw["message"].(string); ok {
			msg.Type = "chat"
			payloadObj := map[string]interface{}{"message": txt}
			// 透传 mode 字段（trace/no_retrieval 等）
			if m, ok := raw["mode"].(string); ok && m != "" {
				payloadObj["mode"] = m
			}
			pb, _ := json.Marshal(payloadObj)
			msg.Payload = pb
			if a, ok := raw["async"].(bool); ok {
				async = a
			}
		} else {
			if t, ok := raw["type"].(string); ok {
				msg.Type = t
			}
			if s, ok := raw["sender"].(string); ok {
				msg.Sender = s
			}
			if rcp, ok := raw["recipient"].(string); ok {
				msg.Recipient = rcp
			}
			if p, ok := raw["payload"]; ok {
				pb, _ := json.Marshal(p)
				msg.Payload = pb
			}
			if a, ok := raw["async"].(bool); ok {
				async = a
			}
		}

		// 确定性预路由：高频意图关键词匹配，跳过 LLM Router
		preRoute(&msg)

		saveToSession(sessionID, "user", msg.Type, msg.Payload)

		w.Header().Set("Content-Type", "application/json")

		if async {
			msg.ReplyTo = "session:" + sessionID

			go func(m kernel.Message) {
				if err := k.Send(m); err != nil {
					// Router 降级：preRoute 未命中（Recipient 空）+ chat 类型 + 路由失败
					// → 降级到 think_plugin 做纯对话，避免用户看到路由错误
					if m.Type == "chat" && m.Recipient == "" {
						log.Printf("[main] 异步 LLM Router 不可达，降级到 think_plugin: %v", err)
						m.Recipient = "think_plugin"
						if err2 := k.Send(m); err2 != nil {
							log.Printf("[main] 异步降级也失败: %v", err2)
						}
					} else {
						log.Printf("[main] 异步请求失败: %v", err)
					}
				}
			}(msg)

			json.NewEncoder(w).Encode(map[string]string{
				"status":     "pending",
				"session_id": sessionID,
			})
			return
		}

		resp, err := k.Call(msg, 120*time.Second)

		// Router 降级：当 preRoute 未命中（Recipient 空）且 LLM Router 不可达时，
		// chat 消息降级到 think_plugin 做纯对话处理，系统保持基本可用。
		// 非 chat 类型（工作流触发、工具调用等）不降级，保留原错误以便排查。
		if err != nil && msg.Type == "chat" && msg.Recipient == "" {
			log.Printf("[main] LLM Router 不可达，降级到 think_plugin: %v", err)
			fallbackMsg := msg
			fallbackMsg.Recipient = "think_plugin"
			resp, err = k.Call(fallbackMsg, 120*time.Second)
		}

		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"note":   err.Error(),
			})
			return
		}

		saveToSession(sessionID, msg.Recipient, resp.Type, resp.Payload)

		// 异步更新 session 摘要：等待 session_add 消息落盘后生成
		// 摘要供跨 session 历史检索（SessionSearchStructured Phase 1）使用
		go func(sid string) {
			time.Sleep(500 * time.Millisecond)
			if sum := tools.GenerateSessionSummary(sid); sum != nil {
				tools.SaveSessionSummary(sum)
			}
			// 每 10 轮对话推断一次用户习惯，异步更新画像
			if n := atomic.AddInt64(&chatCounter, 1); n%10 == 0 {
				go tools.InferAndUpdateProfile()
			}
		}(sessionID)

		// 包装响应，附加 session_id 供客户端后续请求使用
		wrapped := map[string]interface{}{
			"session_id": sessionID,
			"sender":     resp.Sender,
			"type":       resp.Type,
			"payload":    json.RawMessage(resp.Payload),
		}
		json.NewEncoder(w).Encode(wrapped)
	})

	mux.HandleFunc("/api/result/", func(w http.ResponseWriter, r *http.Request) {
		sessionID := strings.TrimPrefix(r.URL.Path, "/api/result/")
		if sessionID == "" {
			http.Error(w, `{"error":"missing session_id"}`, http.StatusBadRequest)
			return
		}

		val, ok := sessionResults.Load(sessionID)
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			json.NewEncoder(w).Encode(map[string]string{
				"session_id": sessionID,
				"status":     "pending",
			})
			return
		}

		sessionResults.Delete(sessionID)
		json.NewEncoder(w).Encode(val)
	})

	// ── 仪表盘 API ──
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		type dashData struct {
			Knowledge    interface{}            `json:"knowledge"`
			Sessions     interface{}            `json:"sessions"`
			Usage        map[string]interface{} `json:"usage"`
			Workflows    []string               `json:"workflows"`
			Plugins      []string               `json:"plugins"`
			Tools        int                    `json:"tools"`
			Health       string                 `json:"health"`
			Uptime       string                 `json:"uptime"`
		}

		// 知识库统计
		kbStats := tools.KnowledgeStats()

		// 会话统计
		sessStats := tools.SessionStats()

		// LLM 使用统计
		usageToday := tools.UsageToday()

		// 工作流列表
		var wfList []string
		entries, _ := os.ReadDir(workflowDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				wfList = append(wfList, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}

		// 插件列表
		pluginList := k.KnownPlugins()

		// 工具数
		toolCount := len(tools.Registry)

		data := dashData{
			Knowledge: kbStats,
			Sessions:  sessStats,
			Usage:     usageToday,
			Workflows: wfList,
			Plugins:   pluginList,
			Tools:     toolCount,
			Health:    "ok",
			Uptime:    time.Since(startTime).Round(time.Second).String(),
		}
		json.NewEncoder(w).Encode(data)
	})

		// ── 知识入库 API（多渠道采集）──
		mux.HandleFunc("/api/ingest", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
				return
			}

			var req struct {
				SourceType string   `json:"source_type"`
				Title      string   `json:"title"`
				Summary    string   `json:"summary"`
				URL        string   `json:"url"`
				Content    string   `json:"content"`
				Tags       []string `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON: " + err.Error()})
				return
			}

			if req.SourceType == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "source_type is required (link / open_source_community / image / article / note)"})
				return
			}
			if req.Title == "" && req.Summary == "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "title or summary is required"})
				return
			}

			rawRef := req.URL
			summary := req.Summary
			if req.URL != "" && summary != "" {
				summary = summary + "\n来源: " + req.URL
			} else if req.URL != "" {
				summary = "来源: " + req.URL
			}

			result := tools.KnowledgeAdd(req.SourceType, req.Title, summary, req.Tags, nil, nil, nil, rawRef, req.Content, "")
			w.Header().Set("Content-Type", "application/json")
			if !result.Success {
				w.WriteHeader(http.StatusInternalServerError)
			}
			w.Write([]byte(result.Output))
		})

	addr := ":8013"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[main] 收到退出信号，关闭服务...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		gl.Shutdown()
		tools.CloseBrowser()
		close(idleConnsClosed)
	}()


	// 注册右花到 glue 统一健康监控（glue 不管理生命周期，只检查状态）
	gl.RegisterRightFlower("openhuman", "http://localhost:9529/health")

	log.Printf("[main] beishan-core HTTP 服务启动于 %s", addr)
	log.Printf("[main] 已注册 %d 个插件", len(k.KnownPlugins()))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[main] 服务异常: %v", err)
	}
	<-idleConnsClosed
	log.Println("[main] 服务已安全关闭")
}


/* buildWorkflowSummary 扫描 workflows/ 目录，提取每个 YAML 工作流的 id 和描述。
   用于注入 Router 提示词，让 DeepSeek 知道可用工作流。 */
func buildWorkflowSummary(dir string) string {
	var sb strings.Builder
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") ||
			info.Name() == "_template.yaml" || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		var id, desc string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#") && desc == "" {
				desc = strings.TrimSpace(strings.TrimPrefix(line, "#"))
				continue
			}
			if strings.HasPrefix(line, "id:") {
				id = strings.TrimSpace(line[3:])
				break
			}
			if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				break
			}
		}

		if id == "" {
			rel, _ := filepath.Rel(dir, path)
			id = strings.TrimSuffix(rel, ".yaml")
		}
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", id, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s\n", id))
		}
		return nil
	})

	return sb.String()
}

// ─── 本地模型故障切换 ─────────────────────────

// selectFallbackEngine picks the best engine for API failover.
// Prefers OpenAI-compatible engines which support /v1/chat/completions.
func selectFallbackEngine(engines []discovery.Engine) (discovery.Engine, bool) {
	for _, e := range engines {
		if e.Type == "openai" || e.Type == "llamacpp" {
			return e, true
		}
	}
	if len(engines) > 0 {
		return engines[0], true
	}
	return discovery.Engine{}, false
}

func checkAPIReachable() bool {
	client := &http.Client{Timeout: 5 * time.Second}
	u := llm.ChatEndpoint()
	u = u[:len(u)-len("/chat/completions")] + "/models"
	resp, err := client.Get(u)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func checkEngineReachable(endpoint string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint + "/v1/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// monitorFailover periodically checks API/local health and switches provider/strategy.
// Uses hysteresis to prevent flapping (2 consecutive successes to switch back).
func monitorFailover(k *kernel.Kernel, state *discovery.StrategyState, engine discovery.Engine) {
	log.Printf("[failover] 监控启动，候选引擎: %s (%s)", engine.Name, engine.Endpoint)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		apiOK := checkAPIReachable()
		localOK := checkEngineReachable(engine.Endpoint)
		decision := state.Decide(apiOK, localOK)

		switch {
		case decision == "local" && state.OnAPI():
			// API → local 切换
			log.Printf("[failover] API 不可用，切换到本地模型 (%s)", engine.Name)
			observatory.RecordTrace(observatory.Trace{
				ID: newSessionID(), Mode: "failover",
				Route: "failover_switch", Plugin: "failover_controller",
				Status: "switched_to_local",
				RouteReason: fmt.Sprintf("API unreachable, fallback to %s (%s)", engine.Name, engine.Model),
			})
			os.Setenv("LLM_BASE_URL", engine.Endpoint)
			if engine.Model != "" {
				os.Setenv("LLM_MODEL", engine.Model)
			}
			llm.SetProvider("local")
			k.Router.SetStrategy(kernel.NewLocalRouteStrategy(k.Router, engine.Endpoint, engine.Model))

		case decision == "api" && !state.OnAPI():
			// local → API 恢复回切
			log.Println("[failover] API 恢复，切回 DeepSeek")
			observatory.RecordTrace(observatory.Trace{
				ID: newSessionID(), Mode: "failover",
				Route: "failover_recovery", Plugin: "failover_controller",
				Status: "recovered_to_api",
				RouteReason: "API reachable after hysteresis, restored to DeepSeek",
			})
			os.Unsetenv("LLM_BASE_URL")
			os.Unsetenv("LLM_MODEL")
			llm.SetProvider("")
			k.Router.SetStrategy(nil)
		}
	}
}
