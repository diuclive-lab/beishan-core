package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"beishan/glue"
	"beishan/internal/tools"
	"beishan/kernel"
	"beishan/plugins"
)

func init() {
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

	// ─── 注册 Go 插件（L3/L4） ──────────────────────

	k.Register("search_plugin", &plugins.SearchPlugin{}, kernel.Meta{
		Description: "通用网络搜索，适用于查找资料、新闻、技术文档",
		Tags:        []string{"search", "retrieval"},
	})
	k.Register("write_plugin", &plugins.WritePlugin{}, kernel.Meta{
		Description: "文本生成与写作，适用于生成报告、摘要、邮件",
		Tags:        []string{"write", "generate"},
	})
	k.Register("memory_plugin", &plugins.MemoryPlugin{}, kernel.Meta{
		Description: "会话记忆管理，存储和召回跨轮上下文信息，按 session 组织",
		Tags:        []string{"memory", "session"},
	})
	k.Register("terminal_plugin", &plugins.TerminalPlugin{}, kernel.Meta{
		Description: "本地终端命令执行，执行 shell 命令和管理后台进程",
		Tags:        []string{"terminal", "shell"},
	})
	k.Register("scheduler_plugin", plugins.NewScheduler(k), kernel.Meta{
		Description: "多步任务编排，适用于需要多个插件协作的复杂任务",
		Tags:        []string{"orchestration", "planning"},
	})

	// 法律审查插件簇（L4 编排 + L3 执行）
	k.Register("legal_review_plugin", &plugins.LegalReviewPlugin{Kernel: k}, kernel.Meta{
		Description: "法律合同全链路审查，编排冷启动、检索、分析、生成报告",
		Tags:        []string{"legal", "orchestration"},
	})
	k.Register("cold_start_plugin", &plugins.ColdStartPlugin{}, kernel.Meta{
		Description: "合同冷启动识别，提取合同类型和法律领域",
		Tags:        []string{"legal", "classification"},
	})
	k.Register("legal_search_plugin", &plugins.LegalSearchPlugin{}, kernel.Meta{
		Description: "法律条文检索，查询法律法规和判例",
		Tags:        []string{"legal", "search"},
	})
	k.Register("clause_analyzer_plugin", &plugins.ClauseAnalyzerPlugin{}, kernel.Meta{
		Description: "合同条款分析，三段论推理合法性和风险等级",
		Tags:        []string{"legal", "analysis"},
	})
	k.Register("legal_write_plugin", &plugins.LegalWritePlugin{}, kernel.Meta{
		Description: "法律审查报告生成，输出结构化审查结论",
		Tags:        []string{"legal", "write"},
	})

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

		// 记录用户输入到 session
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

		// 同步模式
		resp, err := k.Call(msg, 120*time.Second)

		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"note":   err.Error(),
			})
			return
		}

		// 记录插件响应到 session
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
