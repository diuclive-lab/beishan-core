package main

import (
	"log"
	"os"

	"beishan/glue"
	"beishan/kernel"
	"beishan/plugins"
)

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

	// 启动胶水层，自动扫描 plugins/ 目录并 spawn Python 子进程
	gl := glue.New(k, "./plugins")
	if err := gl.Start(); err != nil {
		log.Fatalf("胶水层启动失败: %v", err)
	}

	// 发送测试消息（Recipient 留空，强制 DeepSeek 路由）
	messages := []kernel.Message{
		{Sender: "user", Type: "query", Payload: []byte(`"你好"`)},
	}

	for _, msg := range messages {
		if err := k.Send(msg); err != nil {
			log.Printf("[错误] %v", err)
		}
	}

	gl.Shutdown()
}
