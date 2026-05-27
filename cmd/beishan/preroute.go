package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"beishan/internal/tools"
	"beishan/kernel"
)

// highFreqRoute 高频功能白名单：精确匹配常见命令，不走 LLM Router
// 优先级规则：更具体的匹配在前（如股票代码 > 网络搜索 > 知识库）
func highFreqRoute(text string) (tool, msgType string, matched bool) {
	// 股票代码（连续 6 位数字）— 最具体，放最前面
	re := regexp.MustCompile(`\b\d{6}\b`)
	if re.MatchString(text) {
		return "memory_plugin", "stock_multi_quote", true
	}
	// 知识库 — 在搜索之前，避免"搜索知识库"路由到 web_search
	if strings.Contains(text, "知识库") || strings.Contains(text, "记忆") {
		return "memory_plugin", "knowledge_search", true
	}
	// 网络搜索
	for _, p := range []string{"帮我搜索", "帮我搜一下", "搜索一下", "搜一下", "帮我搜", "帮我查", "查查", "查一下"} {
		if strings.HasPrefix(text, p) {
			return "search_plugin", "web_search", true
		}
	}
	if strings.HasPrefix(text, "搜索") || strings.HasPrefix(text, "搜 ") {
		return "search_plugin", "web_search", true
	}
	// 读文件
	if strings.HasPrefix(text, "读一下") || strings.HasPrefix(text, "读取") || strings.HasPrefix(text, "打开文件") {
		return "write_plugin", "read_file", true
	}
	// 写文件
	if strings.HasPrefix(text, "写一个") || strings.HasPrefix(text, "创建文件") || strings.HasPrefix(text, "新建文件") {
		return "write_plugin", "write_file", true
	}
	// 终端命令
	if strings.HasPrefix(text, "运行") || strings.HasPrefix(text, "执行") || strings.HasPrefix(text, "终端") {
		return "terminal_plugin", "terminal_exec", true
	}
	// 待办
	if strings.HasPrefix(text, "添加待办") || strings.HasPrefix(text, "新建待办") || strings.HasPrefix(text, "新增待办") || strings.HasPrefix(text, "待办列表") || strings.HasPrefix(text, "我的待办") {
		return "todo_plugin", "todo_add", true
	}
	return "", "", false
}

func preRoute(msg *kernel.Message) bool {
	if msg.Recipient != "" {
		return false
	}
	userText := extractUserText(msg.Payload)
	if userText == "" {
		return false
	}

	// ① 高频白名单：精确匹配不走 EvidenceRouter
	if tool, msgType, ok := highFreqRoute(userText); ok {
		msg.Recipient = tool
		msg.Type = msgType
		if payload := buildPayload(tool, msgType, userText); payload != nil {
			msg.Payload = payload
		}
		return true
	}

	// ② EvidenceRouter：模糊匹配，置信度 >= 0.8 才放行
	router := tools.NewEvidenceRouter(tools.DefaultRoutingRules())
	result := router.Route(userText)
	if result == nil || result.Confidence < 0.8 {
		return false
	}

	msg.Recipient = result.Tool
	msg.Type = result.MsgType

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
		if q == "" {
			q = text
		}
		return rawMsg(map[string]string{"query": q})
	case tool == "memory_plugin" && msgType == "knowledge_search":
		kw := text
		for _, prefix := range []string{"搜索知识库", "查知识", "我的笔记"} {
			kw = strings.TrimPrefix(kw, prefix)
		}
		kw = strings.TrimSpace(kw)
		if kw == "" {
			kw = text
		}
		return rawMsg(map[string]string{"keyword": kw})
	case tool == "todo_plugin" && msgType == "todo_add":
		task := text
		for _, kw := range []string{"添加待办", "新建待办", "新增待办"} {
			task = strings.TrimPrefix(task, kw)
		}
		task = strings.TrimSpace(task)
		if task != "" {
			return rawMsg(map[string]string{"task": task})
		}
	case tool == "todo_plugin" && msgType == "todo_list":
		return rawMsg(map[string]interface{}{})
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
