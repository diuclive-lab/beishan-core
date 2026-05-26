package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/internal/tools"
	"beishan/kernel"
)

type TerminalPlugin struct{}

func (p *TerminalPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "terminal_exec", "terminal_list", "terminal_poll", "terminal_kill":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		if len(result.Output) > 0 {
			fmt.Printf("[终端] %s\n", result.Output[:min(len(result.Output), 120)])
		}
		if result.Error != "" {
			return kernel.Message{Type: msg.Type + ".error", Payload: []byte(result.Error)}, nil
		}
		var respPayload json.RawMessage
		if len(result.Output) > 0 && result.Output[0] == '{' && json.Valid([]byte(result.Output)) {
			respPayload = json.RawMessage(result.Output)
		} else {
			respPayload, _ = json.Marshal(result.Output)
		}
		return kernel.Message{Type: msg.Type, Payload: respPayload}, nil
	default:
		return kernel.Message{}, fmt.Errorf("terminal_plugin: 未知类型 %s", msg.Type)
	}
}
