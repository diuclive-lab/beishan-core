package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type BrowserPlugin struct{}

func (p *BrowserPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "browser_navigate", "browser_snapshot", "browser_click", "browser_scroll", "browser_back":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[浏览器] %s\n", result.Output[:min(len(result.Output), 200)])
		if !result.Success {
			return kernel.Message{}, fmt.Errorf("browser_plugin: %s 执行失败: %s", msg.Type, result.Error)
		}
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil
	default:
		return kernel.Message{}, fmt.Errorf("browser_plugin: 未知类型 %s", msg.Type)
	}
}
