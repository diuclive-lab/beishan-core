package kernel

import "encoding/json"

/* Message 是系统中唯一的通信单元。

   Sender 和 Recipient 由微内核解析，用于路由。
   Type 和 Payload 对内核完全不透明，直接透传给插件。
   内核永不解包 Payload。

   CorrelationID 用于 Call() 请求-响应配对。
   L4 编排 L3 时，Call 设置此 ID，L3 返回的消息带相同 ID，
   内核通过此 ID 把响应送回等待的 Call 调用方。

   ReplyTo 用于异步回程路由。Send() 完成后内核检查此字段，
   由 deliverReply() 根据前缀分派：

     "plugin:xxx"       → 内核直接路由到插件
     "session:xxx"      → 存入 session 结果队列（HTTP 轮询）
     "callback:http://…" → HTTP POST 回调（webhook）
     ""                 → fire-and-forget，不回程
*/
type Message struct {
	Sender        string          `json:"sender"`
	Recipient     string          `json:"recipient"`
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	ReplyTo       string          `json:"reply_to,omitempty"`
	Provider      string          `json:"provider,omitempty"` // 可选，指定 LLM provider（local/deepseek/xiaomi/openai）
	SessionID     string          `json:"session_id,omitempty"` // 会话 ID，用于 request-scoped 状态
}
