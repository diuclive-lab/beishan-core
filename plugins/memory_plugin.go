package plugins

import (
	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* MemoryPlugin 会话记忆管理。

   消息类型:
   - "session_add"       → 添加消息到会话
   - "session_get"       → 获取会话完整内容
   - "session_search"    → 跨会话搜索
   - "session_list"      → 列出所有会话
   - "session_delete"    → 删除会话
   - "evidence_add"      → 添加结构化证据
   - "evidence_search"   → 搜索证据
*/
type MemoryPlugin struct{}

func (p *MemoryPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "session_add", "session_get", "session_search",
		"session_list", "session_delete",
		"evidence_add", "evidence_search":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[记忆] %s: %s\n", msg.Type, result.Output)
		return kernel.Message{}, nil

	default:
		return kernel.Message{}, fmt.Errorf("memory_plugin: 未知消息类型 %s", msg.Type)
	}
}
