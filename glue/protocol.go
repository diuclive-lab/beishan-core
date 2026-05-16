package glue

import "encoding/json"

/* IPC 协议：插件子进程与胶水层之间通过 stdin/stdout 通信。

   三种消息类型：

   register  → 子进程启动后发，告诉胶水层自己叫什么名字
   dispatch  → 胶水层发往子进程 stdin，转发内核的消息
   response  → 子进程处理完后发回 stdout
   shutdown  → 胶水层通知子进程退出

   元数据字段（TraceID / Timestamp / RetryCount）：
   - TraceID：全链路追踪 ID，glue 层在 dispatch 时自动注入。
     用于 L3 校验失败时将错误关联回原始请求。
   - Timestamp：消息创建时的 Unix 时间戳（秒）。
     供 L3 实现超时熔断，防止死消息阻塞。
   - RetryCount：当前重试次数（默认 0）。
     供 L3 安全地实现自动重试，防止无限循环。
*/

type ProtocolMessage struct {
	Type    string          `json:"type"`              // register | dispatch | response | shutdown
	Name    string          `json:"name,omitempty"`    // register 时的插件名
	ID      string          `json:"id,omitempty"`      // dispatch 时的请求 ID，用于配对
	TraceID string          `json:"trace_id,omitempty"` // 全链路追踪 ID
	Timestamp int64         `json:"timestamp,omitempty"` // Unix 时间戳（秒）
	RetryCount int          `json:"retry_count,omitempty"` // 重试次数
	Status  string          `json:"status,omitempty"`  // response 时的状态 ok/error
	Error   string          `json:"error,omitempty"`
	Sender  string          `json:"sender,omitempty"`
	MsgType string          `json:"msg_type,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
