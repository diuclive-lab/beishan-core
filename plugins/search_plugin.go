package plugins

import (
	"encoding/json"
	"fmt"
	"time"

	"beishan/internal/tools"
	"beishan/kernel"
)

type SearchPlugin struct {
	Kernel *kernel.Kernel
}

func (p *SearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "web_search":
		result := tools.ValidateAndExecute("web_search", msg.Payload)
		fmt.Printf("[搜索] %s\n", result.Output)

		// 结果回传 think_plugin 做自然语言总结
		if p.Kernel != nil && result.Success && result.Output != "" {
			summaryPayload, _ := json.Marshal(map[string]string{
				"message": fmt.Sprintf("请用中文总结以下搜索结果：%s", result.Output),
				"mode":    "no_retrieval",
			})
			summary, err := p.Kernel.Call(kernel.Message{
				Recipient: "think_plugin",
				Type:      "chat",
				Payload:   summaryPayload,
			}, 30*time.Second)
			if err == nil {
				return summary, nil
			}
			fmt.Printf("[搜索] 总结失败，退回原始结果: %v\n", err)
		}

		// 降级：直接返回原始结果
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
