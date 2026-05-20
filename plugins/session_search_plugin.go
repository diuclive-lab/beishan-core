package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type SessionSearchPlugin struct{}

func (p *SessionSearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "session_search", "session_list":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[会话搜索] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: []byte(fmt.Sprintf("%q", result.Output)),
		}, nil
	default:
		return kernel.Message{}, fmt.Errorf("session_search_plugin: 未知类型 %s", msg.Type)
	}
}
