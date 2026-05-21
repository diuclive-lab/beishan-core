package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* NotifyPlugin 通知发送。

   消息类型:
   - "notify_send"     → 发送通知（email/slack/wechat）
*/
type NotifyPlugin struct{}

func (p *NotifyPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "notify_send":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[通知] %s\n", result.Output)
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		respType := msg.Type + ".result"
		if result.Error != "" {
			respType = msg.Type + ".error"
		}
		return kernel.Message{Type: respType, Payload: respPayload}, nil

	default:
		return kernel.Message{}, fmt.Errorf("notify_plugin: 未知消息类型 %s", msg.Type)
	}
}
