package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* TTSPlugin （语音合成插件）

   文本转语音。用户可以说"帮我把这段话转成语音"。
   macOS 使用 say 命令，Linux 使用 espeak。
*/
type TTSPlugin struct{}

func (p *TTSPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "text_to_speech":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[语音] %s\n", result.Output[:min(len(result.Output), 200)])
		if !result.Success {
			return kernel.Message{}, fmt.Errorf("tts_plugin: %s 执行失败: %s", msg.Type, result.Error)
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
		return kernel.Message{}, fmt.Errorf("tts_plugin: 未知类型 %s", msg.Type)
	}
}
