package plugins

import (
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
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("tts_plugin: 未知类型 %s", msg.Type)
	}
}
