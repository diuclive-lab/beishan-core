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
		// 如果输出是有效 JSON，直接返回；否则包装为 JSON 字符串
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: respPayload,
		}, nil

	case "web_fetch", "web_extract", "web_render":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[抓取] %s\n", result.Output)
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{
			Type:    msg.Type + ".result",
			Payload: respPayload,
		}, nil

	default:
		return kernel.Message{}, fmt.Errorf("search_plugin: 未知消息类型 %s", msg.Type)
	}
}
