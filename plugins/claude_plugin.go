package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* ClaudePlugin Claude 记忆导入。

   消息类型:
   - "claude_memory_list"      → 列出 Claude 记忆文件
   - "claude_memory_import"    → 导入指定或全部记忆
*/
type ClaudePlugin struct{}

func (p *ClaudePlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "claude_memory_list", "claude_memory_import":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[Claude] %s: %s\n", msg.Type, result.Output[:min(len(result.Output), 200)])
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	default:
		return kernel.Message{}, fmt.Errorf("claude_plugin: 未知消息类型 %s", msg.Type)
	}
}
