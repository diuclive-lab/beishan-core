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
	"beishan/kernel"
	"beishan/plugins"
)

func init() {
	// 自动加载 .env 文件（如果存在）
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

	// ─── 注册 Go 插件（L3/L4） ──────────────────────
	// 通用工具插件
	k.Register("search_plugin", &plugins.SearchPlugin{})
	k.Register("write_plugin", &plugins.WritePlugin{})
	k.Register("memory_plugin", &plugins.MemoryPlugin{})
	k.Register("scheduler_plugin", plugins.NewScheduler(k))

	// 法律审查插件簇（L4 编排 + L3 执行）
	k.Register("legal_review_plugin", &plugins.LegalReviewPlugin{Kernel: k})
	k.Register("cold_start_plugin", &plugins.ColdStartPlugin{})
	k.Register("legal_search_plugin", &plugins.LegalSearchPlugin{})
	k.Register("clause_analyzer_plugin", &plugins.ClauseAnalyzerPlugin{})
	k.Register("legal_write_plugin", &plugins.LegalWritePlugin{})

	// 启动胶水层，自动扫描 plugins/ 目录并 spawn 子进程
	gl := glue.New(k, "./plugins")
	if err := gl.Start(); err != nil {
		log.Fatalf("胶水层启动失败: %v", err)
	}

	// ─── HTTP API ──────────────────────────────────
	mux := http.NewServeMux()

	// GET /health — 健康检查（供 smoke test preflight 使用）
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// POST /api/chat — 发送消息并等待响应
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Sender    string          `json:"sender"`
			Recipient string          `json:"recipient"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request: `+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		if req.Sender == "" {
			req.Sender = "user"
		}

		// 通过 kernel.Call 发送消息并等待响应（最长 120 秒）
		// Recipient 为空时强制走 DeepSeek 路由
		resp, err := k.Call(kernel.Message{
			Sender:    req.Sender,
			Recipient: req.Recipient,
			Type:      req.Type,
			Payload:   req.Payload,
		}, 120*time.Second)

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			// 超时或无响应：消息已发送，但插件未返回响应
			json.NewEncoder(w).Encode(map[string]string{
				"status": "sent",
				"note":   err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(resp)
	})

	// HTTP 服务器
	addr := ":8013"
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// 优雅关闭
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
