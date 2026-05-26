package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"beishan/internal/tools"
	"beishan/kernel"
)

// preRoute 用 evidence_router 做确定性预路由，跳过 LLM Router。
func preRoute(msg *kernel.Message) bool {
	if msg.Recipient != "" {
		return false
	}

	userText := extractUserText(msg.Payload)
	if userText == "" {
		return false
	}

	router := tools.NewEvidenceRouter(tools.DefaultRoutingRules())
	result := router.Route(userText)
	if result == nil || result.Confidence < 0.3 {
		return false
	}

	msg.Recipient = result.Tool
	msg.Type = result.MsgType

	// 搜索歧义二次检查
	if result.Tool == "search_plugin" && (strings.Contains(userText, "知识库") || strings.Contains(userText, "记忆")) {
		msg.Recipient = "memory_plugin"
		msg.Type = "knowledge_search"
		if payload := buildSearchPayload(userText); payload != nil {
			msg.Payload = payload
		}
		return true
	}

	if payload := buildPayload(result.Tool, result.MsgType, userText); payload != nil {
		msg.Payload = payload
	}

	fmt.Printf("[preRoute] %s → %s(%s) %.2f\n", truncateStr(userText, 40), result.Tool, result.MsgType, result.Confidence)
	return true
}

func buildPayload(tool, msgType, text string) json.RawMessage {
	switch {
	case tool == "terminal_plugin" && msgType == "terminal_exec":
		if strings.Contains(text, "桌面") || strings.Contains(text, "Desktop") {
			return rawMsg(map[string]string{"command": "ls -la ~/Desktop/"})
		}
	case tool == "memory_plugin" && msgType == "desktop_actuator":
		action := "get_window_tree"
		if strings.Contains(text, "截图") || strings.Contains(text, "截屏") {
			action = "screenshot"
		}
		if strings.Contains(text, "点击") || strings.Contains(text, "按") {
			action = "click"
		}
		return rawMsg(map[string]string{"action": action})
	case tool == "search_plugin" && msgType == "web_search":
		q := text
		for _, p := range []string{"帮我搜索", "帮我搜一下", "搜索一下", "搜一下", "查找资料", "搜索", "帮我搜", "帮我查", "查查", "查一下"} {
			q = strings.TrimPrefix(q, p)
		}
		q = strings.TrimSpace(q)
		if q != "" {
			return rawMsg(map[string]string{"query": q})
		}
	case tool == "memory_plugin" && msgType == "knowledge_search":
		kw := text
		for _, prefix := range []string{"搜索知识库", "查知识", "我的笔记"} {
			kw = strings.TrimPrefix(kw, prefix)
		}
		kw = strings.TrimSpace(kw)
		if kw != "" {
			return rawMsg(map[string]string{"keyword": kw})
		}
	case tool == "todo_plugin" && (msgType == "todo_add" || msgType == "todo_list"):
		if msgType == "todo_list" {
			return rawMsg(map[string]interface{}{})
		}
		task := text
		for _, kw := range []string{"添加待办", "新建待办", "新增待办"} {
			task = strings.TrimPrefix(task, kw)
		}
		task = strings.TrimSpace(task)
		if task != "" {
			return rawMsg(map[string]string{"task": task})
		}
	case tool == "memory_plugin" && msgType == "stock_multi_quote":
		re := regexp.MustCompile(`(\d{6})`)
		matches := re.FindAllStringSubmatch(text, -1)
		if len(matches) > 0 {
			var codes []string
			seen := make(map[string]bool)
			for _, m := range matches {
				if !seen[m[1]] {
					seen[m[1]] = true
					codes = append(codes, m[1])
				}
			}
			return rawMsg(map[string]interface{}{"codes": codes})
		}
	}
	return nil
}

func rawMsg(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func buildSearchPayload(text string) json.RawMessage {
	for _, prefix := range []string{"搜索一下", "搜索知识", "查知识", "搜一下", "搜索", "搜"} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return rawMsg(map[string]string{"keyword": text})
}

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

func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
