package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/internal/tools"
	"beishan/kernel"
)

type SessionSearchPlugin struct{}

func (p *SessionSearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "session_search", "session_list":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[会话搜索] %s\n", truncate(result.Output, 200))

		// 与 memory_plugin 保持一致：JSON 对象直接透传，其他类型 JSON 序列化
		var payload json.RawMessage
		output := result.Output
		if len(output) > 0 && (output[0] == '{' || output[0] == '[') && json.Valid([]byte(output)) {
			payload = json.RawMessage(output)
		} else {
			payload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: payload}, nil
	default:
		return kernel.Message{}, fmt.Errorf("session_search_plugin: 未知类型 %s", msg.Type)
	}
}
