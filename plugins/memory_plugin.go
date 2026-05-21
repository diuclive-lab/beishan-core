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
	switch msg.Type {
	case "session_add", "session_get", "session_search",
		"session_list", "session_delete", "session_cleanup",
		"evidence_add", "evidence_search":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[记忆] %s: %s\n", msg.Type, result.Output)
		return kernel.Message{}, nil

	case "knowledge_add", "knowledge_search",
		"knowledge_list", "knowledge_get", "knowledge_delete", "knowledge_update", "knowledge_suggest_links", "knowledge_dedupe", "knowledge_merge", "knowledge_confirm_links", "knowledge_remember", "knowledge_reindex", "system_info", "knowledge_embed", "knowledge_embed_all", "knowledge_semantic_search", "knowledge_topic_map", "knowledge_timeline",
		"kb_audit", "kb_repair",
		"stock_quote", "stock_multi_quote",
		"image_generate", "image_to_image",
		"prompt_engineer", "prompt_analyze", "prompt_style_list":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[知识] %s: %s\n", msg.Type, result.Output)
		// 如果输出是 JSON 对象/数组则原样传递，否则封装为 JSON 字符串
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	default:
		return kernel.Message{}, fmt.Errorf("memory_plugin: 未知消息类型 %s", msg.Type)
	}
}
