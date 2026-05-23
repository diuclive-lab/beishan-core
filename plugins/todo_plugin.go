package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/internal/tools"
	"beishan/kernel"
)

type TodoPlugin struct{}

func (p *TodoPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "todo_list", "todo_add", "todo_done", "todo_clear", "todo_by_source":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[待办] %s\n", result.Output[:min(len(result.Output), 200)])
		var respPayload json.RawMessage
		if result.Success && result.Output != "" {
			if len(result.Output) > 0 && (result.Output[0] == '{' || result.Output[0] == '[') && json.Valid([]byte(result.Output)) {
				respPayload = json.RawMessage(result.Output)
			} else {
				respPayload, _ = json.Marshal(result.Output)
			}
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil
	default:
		return kernel.Message{}, fmt.Errorf("todo_plugin: 未知类型 %s", msg.Type)
	}
}
