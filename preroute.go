package main

import (
	"encoding/json"
	"strings"

	"beishan/kernel"
)

type preroutePattern struct {
	keywords  []string
	recipient string
	msgType   string
	extract   func(userText string) json.RawMessage // nil = keep original payload
}

var preroutePatterns = []preroutePattern{
	// 搜索意图：提取关键词作为 query
	{
		keywords:  []string{"搜索", "搜一下", "查找资料", "帮我搜"},
		recipient: "search_plugin",
		msgType:   "web_search",
		extract: func(text string) json.RawMessage {
			q := text
			for _, kw := range []string{"帮我搜索", "帮我搜一下", "搜索一下", "搜一下", "查找资料", "搜索", "帮我搜"} {
				q = strings.TrimPrefix(q, kw)
			}
			q = strings.TrimSpace(q)
			if q == "" {
				return nil
			}
			b, _ := json.Marshal(map[string]string{"query": q})
			return b
		},
	},
	// 记住意图：保持原始消息给 think_plugin
	{
		keywords:  []string{"记住", "记录一下"},
		recipient: "think_plugin",
		msgType:   "chat",
		extract:   nil, // keep original payload
	},
	// 创建工作流
	{
		keywords:  []string{"创建工作流", "新建工作流", "生成工作流"},
		recipient: "skill_factory_plugin",
		msgType:   "skill_create",
		extract:   func(text string) json.RawMessage { return json.RawMessage(`{}`) },
	},
	// 待办列表
	{
		keywords:  []string{"查看待办", "待办列表", "列出待办", "我的待办"},
		recipient: "todo_plugin",
		msgType:   "todo_list",
		extract:   func(text string) json.RawMessage { return json.RawMessage(`{}`) },
	},
	// 添加待办
	{
		keywords:  []string{"添加待办", "新建待办", "新增待办"},
		recipient: "todo_plugin",
		msgType:   "todo_add",
		extract: func(text string) json.RawMessage {
			task := text
			for _, kw := range []string{"添加待办", "新建待办", "新增待办"} {
				task = strings.TrimPrefix(task, kw)
			}
			task = strings.TrimSpace(task)
			if task == "" {
				return nil
			}
			b, _ := json.Marshal(map[string]string{"task": task})
			return b
		},
	},
	// 知识库搜索
	{
		keywords:  []string{"搜索知识库", "查知识", "我的笔记"},
		recipient: "memory_plugin",
		msgType:   "knowledge_search",
		extract: func(text string) json.RawMessage {
			kw := text
			for _, prefix := range []string{"搜索知识库", "查知识", "我的笔记"} {
				kw = strings.TrimPrefix(kw, prefix)
			}
			kw = strings.TrimSpace(kw)
			if kw == "" {
				return nil
			}
			b, _ := json.Marshal(map[string]string{"keyword": kw})
			return b
		},
	},
}

// preRoute 确定性预路由：高频意图关键词匹配，50ms 内完成，跳过 LLM Router。
// 返回 true 表示已匹配，msg 的 Recipient/Type/Payload 已就绪。
func preRoute(msg *kernel.Message) bool {
	if msg.Recipient != "" {
		return false // 已指定收件人，跳过
	}

	// 从 payload 提取用户文本
	userText := extractUserText(msg.Payload)
	if userText == "" {
		return false
	}

	for _, p := range preroutePatterns {
		for _, kw := range p.keywords {
			if strings.Contains(userText, kw) {
				msg.Recipient = p.recipient
				msg.Type = p.msgType
				if p.extract != nil {
					if payload := p.extract(userText); payload != nil {
						msg.Payload = payload
					}
				}
				return true
			}
		}
	}
	return false
}

// extractUserText 从 message payload 中提取用户文本
func extractUserText(payload json.RawMessage) string {
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return ""
	}
	if txt, ok := obj["message"].(string); ok {
		return txt
	}
	return ""
}
