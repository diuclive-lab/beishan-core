package plugins

import (
	"encoding/json"

	"beishan/internal/tools"
	"beishan/kernel"
	"fmt"
)

/* MemoryPlugin 会话记忆管理 + 知识条目管理。

   会话消息类:
   - "session_add"       → 添加消息到会话
   - "session_get"       → 获取会话完整内容
   - "session_search"    → 跨会话搜索
   - "session_list"      → 列出所有会话
   - "session_delete"    → 删除会话
   - "evidence_add"      → 添加结构化证据
   - "evidence_search"   → 搜索证据

   知识条目类（统一 schema）:
   - "knowledge_add"     → 添加结构化知识条目
   - "knowledge_search"  → 搜索知识条目
   - "knowledge_list"    → 列出知识条目
   - "knowledge_get"     → 获取单个知识条目详情
   - "knowledge_delete"  → 删除知识条目
   - "knowledge_update"  → 更新知识条目的字段
   - "knowledge_suggest_links"  → 推荐关联知识条目
   - "knowledge_dedupe"  → 查找可能重复的条目
   - "knowledge_merge"  → 合并两个重复条目
*/
type MemoryPlugin struct{}

func (p *MemoryPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	// 动态转发：自动匹配 tools.Registry 中已注册的工具，无需硬编码 case 列表。
	// 新增工具只需在 tools.go Init() 中注册，memory_plugin 自动可达。
	if tools.HasTool(msg.Type) {
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[memory] %s: %s\n", msg.Type, truncate(result.Output, 200))
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil
	}

	return kernel.Message{}, fmt.Errorf("memory_plugin: 未知消息类型 %s（未在 tools.Registry 中注册）", msg.Type)
}
