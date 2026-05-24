package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"beishan/kernel"
)

type preroutePattern struct {
	keywords  []string
	recipient string
	msgType   string
	extract   func(userText string) json.RawMessage // nil = keep original payload
}

// 匹配顺序：长关键词在前，避免短关键词截胡。
// 例："搜索知识库" 必须在 "搜索" 之前匹配。
var preroutePatterns = []preroutePattern{
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
	// 对比/差异类
	{
		keywords:  []string{"对比", "区别", "差异", "不同", "差距"},
		recipient: "",
		msgType:   "",
	},
	// 搜索兜底
	{
		keywords:  []string{"搜一下", "帮我查", "查查", "查一下"},
		recipient: "search_plugin",
		msgType:   "web_search",
		extract: func(text string) json.RawMessage {
			kw := text
			for _, p := range []string{"搜一下", "帮我查", "查查", "查一下"} {
				kw = strings.TrimPrefix(kw, p)
			}
			kw = strings.TrimSpace(kw)
			if kw == "" { return nil }
			b, _ := json.Marshal(map[string]string{"query": kw})
			return b
		},
	},
	// 创建工作流
	{
		keywords:  []string{"创建工作流", "新建工作流", "生成工作流"},
		recipient: "skill_factory_plugin",
		msgType:   "skill_create",
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
	// 待办列表
	{
		keywords:  []string{"查看待办", "待办列表", "列出待办", "我的待办"},
		recipient: "todo_plugin",
		msgType:   "todo_list",
		extract:   func(text string) json.RawMessage { return json.RawMessage(`{}`) },
	},
	// 搜索意图（放后面，避免截胡"搜索知识库"等长关键词）
	{
		keywords:  []string{"搜一下", "查找资料", "帮我搜", "搜索"},
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
	// 记住意图
	// 股票行情（必须有 6 位数字代码）
	{
		keywords:  []string{"股价", "行情", "股票"},
		recipient: "memory_plugin",
		msgType:   "stock_multi_quote",
		extract: func(text string) json.RawMessage {
			re := regexp.MustCompile(`(\d{6})`)
			matches := re.FindAllStringSubmatch(text, -1)
			if len(matches) == 0 {
				return nil
			}
			var codes []string
			seen := make(map[string]bool)
			for _, m := range matches {
				if !seen[m[1]] {
					seen[m[1]] = true
					codes = append(codes, m[1])
				}
			}
			b, _ := json.Marshal(map[string]interface{}{"codes": codes})
			return b
		},
	},
	{
		keywords:  []string{"记住", "记录一下"},
		recipient: "think_plugin",
		msgType:   "chat",
		extract:   nil, // keep original payload
	},
}

// preRoute 确定性预路由：高频意图关键词匹配，50ms 内完成，跳过 LLM Router。
// 返回 true 表示已匹配，msg 的 Recipient/Type/Payload 已就绪。
func preRoute(msg *kernel.Message) bool {
	if msg.Recipient != "" {
		return false // 已指定收件人，跳过
	}
	// 记录匹配结果用于通知
	var matchedPattern string
	_ = matchedPattern // reserved for routing notification

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

				// 搜索歧义二次检查：命中 search_plugin 但内容指知识库/记忆时改路由
				if p.recipient == "search_plugin" && p.msgType == "web_search" {
					if strings.Contains(userText, "知识库") || strings.Contains(userText, "记忆") {
						msg.Recipient = "memory_plugin"
						msg.Type = "knowledge_search"
						if payload := buildSearchPayload(userText); payload != nil {
							msg.Payload = payload
						}
						return true
					}
				}

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

// buildSearchPayload 从用户文本中提取搜索关键词，构造 knowledge_search payload
func buildSearchPayload(text string) json.RawMessage {
	for _, prefix := range []string{"搜索一下", "搜索知识", "查知识", "搜一下", "搜索", "搜"} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]string{"keyword": text})
	return b
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
