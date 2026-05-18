package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* ImageGenPlugin （图片生成插件）

   AI 图片生成。用户可以说"帮我生成一张图"。
   预留接口，需要配置 DALL-E / Stable Diffusion 等后端后使用。
*/
type ImageGenPlugin struct{}

func (p *ImageGenPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "image_generate":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[图片] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("image_gen_plugin: 未知类型 %s", msg.Type)
	}
}
