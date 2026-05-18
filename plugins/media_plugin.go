package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* MediaPlugin （媒体插件）

   视觉分析、图片生成、文本转语音。预留接口，
   需要配置外部 API 后完整启用。
*/
type MediaPlugin struct{}

func (p *MediaPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "vision_analyze", "image_generate", "text_to_speech":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[媒体] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("media_plugin: 未知类型 %s", msg.Type)
	}
}
