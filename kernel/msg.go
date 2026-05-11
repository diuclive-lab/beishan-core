package kernel

import "encoding/json"

/* Message 是系统中唯一的通信单元。

   Sender 和 Recipient 由微内核解析，用于路由。
   Type 和 Payload 对内核完全不透明，直接透传给插件。
   内核永不解包 Payload。

   CorrelationID 用于 Call() 请求-响应配对。
   L4 编排 L3 时，Call 设置此 ID，L3 返回的消息带相同 ID，
   内核通过此 ID 把响应送回等待的 Call 调用方。
*/
type Message struct {
	Sender        string          `json:"sender"`
	Recipient     string          `json:"recipient"`
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	CorrelationID string          `json:"correlation_id,omitempty"` // 请求-响应配对
}
