package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type TodoPlugin struct{}

func (p *TodoPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "todo_list", "todo_add", "todo_done", "todo_clear", "todo_by_source":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[待办] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("todo_plugin: 未知类型 %s", msg.Type)
	}
}
