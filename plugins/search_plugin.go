package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/internal/tools"
	"beishan/kernel"
)

type SearchPlugin struct{}

func (p *SearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "web_search":
		result := tools.ValidateAndExecute("web_search", msg.Payload)
		fmt.Printf("[搜索] %s\n", result.Output)
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: json.RawMessage(`"` + jsonEscape(result.Output) + `"`),
		}, nil

	case "web_fetch", "web_extract", "web_render":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[抓取] %s\n", result.Output)
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: json.RawMessage(`"` + jsonEscape(result.Output) + `"`),
		}, nil

	default:
		return kernel.Message{}, fmt.Errorf("search_plugin: 未知消息类型 %s", msg.Type)
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
