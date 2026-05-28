package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

type WritePlugin struct{}

func (p *WritePlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "write_file", "read_file", "search_files", "patch":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[文件] %s: %s\n", msg.Type, result.Output)
		if !result.Success {
			return kernel.Message{}, fmt.Errorf("write_plugin: %s 执行失败: %s", msg.Type, result.Error)
		}
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	case "file_parse":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[文件] 解析: %s\n", result.Output[:min(len(result.Output), 120)])
		if !result.Success {
			return kernel.Message{}, fmt.Errorf("write_plugin: file_parse 执行失败: %s", result.Error)
		}
		// 返回 JSON 结果供 workflow 使用
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	default:
		return kernel.Message{}, fmt.Errorf("write_plugin: 未知消息类型 %s", msg.Type)
	}
}
