package glue

import "encoding/json"

/* IPC 协议：插件子进程与胶水层之间通过 stdin/stdout 通信。

   三种消息类型：

   register  → 子进程启动后发，告诉胶水层自己叫什么名字
   dispatch  → 胶水层发往子进程 stdin，转发内核的消息
   response  → 子进程处理完后发回 stdout
   shutdown  → 胶水层通知子进程退出
*/

type ProtocolMessage struct {
	Type   string          `json:"type"`   // register | dispatch | response | shutdown
	Name   string          `json:"name,omitempty"`   // register 时的插件名
	ID     string          `json:"id,omitempty"`     // dispatch 时的请求 ID，用于配对
	Status string          `json:"status,omitempty"` // response 时的状态 ok/error
	Error  string          `json:"error,omitempty"`
	Sender string          `json:"sender,omitempty"`
	MsgType string         `json:"msg_type,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
