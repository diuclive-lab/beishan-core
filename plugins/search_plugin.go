package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type SearchPlugin struct{}

func (p *SearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "web_search":
		result := tools.ValidateAndExecute("web_search", msg.Payload)
		fmt.Printf("[搜索] %s\n", result.Output)
		return kernel.Message{}, nil

	case "web_fetch":
		result := tools.ValidateAndExecute("web_fetch", msg.Payload)
		fmt.Printf("[抓取] %s\n", result.Output)
		return kernel.Message{}, nil

	default:
		return kernel.Message{}, fmt.Errorf("search_plugin: 未知消息类型 %s", msg.Type)
	}
}
