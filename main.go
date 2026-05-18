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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"beishan/glue"
	"beishan/internal/tools"
	"beishan/internal/workflow"
	"beishan/kernel"
	"beishan/plugins"
)

//go:embed web/index.html
var IndexHTML string
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

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("请设置环境变量 DEEPSEEK_API_KEY")
	}

	k := kernel.NewKernel(apiKey)

	tools.Init()

	// ─── 注册 Go 插件 ─────────────────────────────

	k.Register("search_plugin", &plugins.SearchPlugin{}, kernel.Meta{
		Description: "通用网络搜索，适用于查找资料、新闻、技术文档",
		Tags:        []string{"search", "retrieval"},
		Types:       []string{"web_search", "web_fetch"},
	})
	k.Register("write_plugin", &plugins.WritePlugin{}, kernel.Meta{
		Description: "文本生成与写作，适用于生成报告、摘要、邮件",
		Tags:        []string{"write", "generate"},
		Types:       []string{"write_file", "read_file", "search_files", "patch"},
	})
	k.Register("memory_plugin", &plugins.MemoryPlugin{}, kernel.Meta{
		Description: "会话记忆管理，存储和召回跨轮上下文信息",
		Tags:        []string{"memory", "session"},
		Types:       []string{"session_add", "session_get", "session_search", "session_list", "session_delete", "evidence_add", "evidence_search"},
	})
	k.Register("terminal_plugin", &plugins.TerminalPlugin{}, kernel.Meta{
		Description: "本地终端命令执行，执行 shell 命令和管理后台进程",
		Tags:        []string{"terminal", "shell"},
		Types:       []string{"terminal_exec", "terminal_list", "terminal_poll", "terminal_kill"},
	})
	k.Register("browser_plugin", &plugins.BrowserPlugin{}, kernel.Meta{
		Description: "浏览器自动化，导航、点击、滚动、提取网页内容",
		Tags:        []string{"browser", "web"},
		Types:       []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_scroll", "browser_back"},
	})
	k.Register("session_search_plugin", &plugins.SessionSearchPlugin{}, kernel.Meta{
		Description: "历史会话搜索，按关键词搜索所有存储的对话记录",
		Tags:        []string{"session", "search"},
		Types:       []string{"session_search", "session_list"},
	})
	k.Register("todo_plugin", &plugins.TodoPlugin{}, kernel.Meta{
		Description: "待办事项管理，添加、列出、标记完成、清除任务",
		Tags:        []string{"todo", "task"},
		Types:       []string{"todo_list", "todo_add", "todo_done", "todo_clear"},
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
	k.Register("think_plugin", &plugins.ThinkPlugin{}, kernel.Meta{
		Description: "通用对话与问答，处理用户的聊天、闲聊、创意写作等非特定任务请求",
		Tags:        []string{"chat", "dialogue", "general"},
		Types:       []string{"chat"},
	})
	k.Register("scheduler_plugin", plugins.NewScheduler(k), kernel.Meta{
		Description: "多步任务编排，适用于需要多个插件协作的复杂任务",
		Tags:        []string{"orchestration", "planning"},
	})
	k.Register("skill_factory_plugin", plugins.NewSkillFactory(k, "./workflows"), kernel.Meta{
		Description: "技能工场，根据自然语言描述自动生成 YAML 工作流",
		Tags:        []string{"skill", "workflow", "generation"},
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

	// 工作流引擎
	wfEngine := workflow.New(k, "./workflows")
	k.Register("workflow_plugin", &plugins.WorkflowPlugin{Engine: wfEngine}, kernel.Meta{
		Description: "工作流引擎，执行 workflows/*.yaml 定义的多步骤编排任务，包括法律审查、尽职调查等场景",
		Tags:        []string{"workflow", "orchestration", "legal"},
		Types:       []string{"workflow_run"},
	})


	// 扫描 workflows/ 目录，注入 Router workflow 摘要
	workflowDir := "./workflows"
	workflowSummary := buildWorkflowSummary(workflowDir)
	k.Router.SetWorkflowSummary(workflowSummary)

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

	// ─── HTTP API ──────────────────────────────────

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
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

		sessionID := newSessionID()
		msg := kernel.Message{Sender: "user"}
		async := false

		if txt, ok := raw["message"].(string); ok {
			msg.Type = "chat"
			msg.Payload = json.RawMessage(`"` + txt + `"`)
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

		saveToSession(sessionID, "user", msg.Type, msg.Payload)

		w.Header().Set("Content-Type", "application/json")

		if async {
			msg.ReplyTo = "session:" + sessionID

			go func(sID string) {
				if err := k.Send(msg); err != nil {
					log.Printf("[main] 异步请求失败: %v", err)
				}
			}(sessionID)

			json.NewEncoder(w).Encode(map[string]string{
				"status":     "pending",
				"session_id": sessionID,
			})
			return
		}

		resp, err := k.Call(msg, 120*time.Second)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"note":   err.Error(),
			})
			return
		}

		saveToSession(sessionID, msg.Recipient, resp.Type, resp.Payload)
		json.NewEncoder(w).Encode(resp)
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

	addr := ":8013"
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
		close(idleConnsClosed)
	}()

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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || e.Name() == "_template.yaml" {
			continue
		}

		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		// 读取 id 和描述
		var id, desc string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// 提取头部注释作为描述
			if strings.HasPrefix(line, "#") && desc == "" {
				desc = strings.TrimSpace(strings.TrimPrefix(line, "#"))
				continue
			}
			// 提取 id 字段
			if strings.HasPrefix(line, "id:") {
				id = strings.TrimSpace(line[3:])
				break
			}
			// 遇到非空、非注释行就停
			if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				break
			}
		}
		f.Close()

		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".yaml")
		}
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", id, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s\n", id))
		}
	}

	return sb.String()
}
