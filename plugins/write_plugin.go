package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type WritePlugin struct{}

func (p *WritePlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "write_file", "read_file", "search_files", "patch":
		result := tools.Execute(msg.Type, string(msg.Payload))
		fmt.Printf("[文件] %s: %s\n", msg.Type, result.Output)
		return kernel.Message{}, nil

	default:
		return kernel.Message{}, fmt.Errorf("write_plugin: 未知消息类型 %s", msg.Type)
	}
}
