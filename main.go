package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
		Description: "会话记忆管理，适用于存储和召回上下文信息",
		Tags:        []string{"memory", "session"},
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

	// 启动胶水层，自动扫描 plugins/ 目录并 spawn 子进程
	gl := glue.New(k, "./plugins")
	if err := gl.Start(); err != nil {
		log.Fatalf("胶水层启动失败: %v", err)
	}

	// ─── HTTP API ──────────────────────────────────

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

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

		msg := kernel.Message{Sender: "user"}

		if txt, ok := raw["message"].(string); ok {
			// 简单格式: {"message":"写一份合同审查"}
			msg.Type = "chat"
			msg.Payload = json.RawMessage(`"` + txt + `"`)
		} else {
			// 完整格式: {"type":"...","payload":"...","recipient":"..."}
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
		}

		resp, err := k.Call(msg, 120*time.Second)

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"note":   err.Error(),
			})
			return
		}
		json.NewEncoder(w).Encode(resp)
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
