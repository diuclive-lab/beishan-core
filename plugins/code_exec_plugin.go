package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type CodeExecPlugin struct{}

func (p *CodeExecPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "code_exec":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[执行] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("code_exec_plugin: 未知类型 %s", msg.Type)
	}
}
