package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type MemoryPlugin struct{}

func (p *MemoryPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	var result *tools.ToolResult

	switch msg.Type {
	case "memory_read":
		result = tools.ValidateAndExecute("memory_read", msg.Payload)
	case "memory_add":
		result = tools.ValidateAndExecute("memory_add", msg.Payload)
	case "memory_search":
		result = tools.ValidateAndExecute("memory_search", msg.Payload)
	default:
		return kernel.Message{}, fmt.Errorf("memory_plugin: 未知消息类型 %s", msg.Type)
	}

	fmt.Printf("[记忆] %s: %s\n", msg.Type, result.Output)
	return kernel.Message{}, nil
}
