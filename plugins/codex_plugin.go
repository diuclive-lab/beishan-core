package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* CodexSessionPlugin Codex 对话导入。

   消息类型:
   - "codex_session_list"     → 列出 Codex 对话
   - "codex_session_extract"  → 提取指定对话内容
*/
type CodexSessionPlugin struct{}

func (p *CodexSessionPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "codex_session_list", "codex_session_extract":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[Codex] %s: %s\n", msg.Type, result.Output[:min(len(result.Output), 200)])
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	default:
		return kernel.Message{}, fmt.Errorf("codex_plugin: 未知消息类型 %s", msg.Type)
	}
}
