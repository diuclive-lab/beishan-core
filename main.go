package main

import (
	"log"
	"os"

	"beishan/glue"
	"beishan/kernel"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("请设置环境变量 DEEPSEEK_API_KEY")
	}

	k := kernel.NewKernel(apiKey)

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
