package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* ClarifyPlugin （澄清插件）

   用户输入模糊时向用户提问确认。同时记录用户澄清历史，
   学习用户语言习惯，多次相同模糊输入后自动推断意图，
   不再反复提问。
*/
type ClarifyPlugin struct{}

func (p *ClarifyPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "clarify":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[澄清] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	case "clarify_learn":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[澄清学习] %s\n", result.Output[:min(len(result.Output), 200)])
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("clarify_plugin: 未知类型 %s", msg.Type)
	}
}
