package plugins

import (
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
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("browser_plugin: 未知类型 %s", msg.Type)
	}
}
