package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type TerminalPlugin struct{}

func (p *TerminalPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "terminal_exec", "terminal_list", "terminal_poll", "terminal_kill":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[终端] %s\n", result.Output[:min(len(result.Output), 120)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("terminal_plugin: 未知类型 %s", msg.Type)
	}
}
