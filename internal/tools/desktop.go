package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

/* ─── desktop_actuator L3 工具 ───────────────

   吸收自 FangLab's desktop_actuator.py。
   操作桌面鼠标键盘、获取窗口树、截屏。

   安全规则：
   - 所有操作通过 Python 子进程执行
   - 禁止未授权的文件系统写入（通过命令行参数白名单）
   - 操作结果不包含敏感信息（密码/密钥）
*/

func registerDesktopTools() {
	Register("desktop_actuator", "Execute desktop operations: click, type, list windows, screenshot. Requires macOS accessibility permission.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"click", "type_text", "get_window_tree", "get_menu_bar_tree", "click_menu_item"},
					"description": "Action type: click, type_text, get_window_tree, get_menu_bar_tree, click_menu_item",
				},
				"x":        intParam("X coord for click"),
				"y":        intParam("Y coord for click"),
				"text":     stringParam("Text to type"),
				"submit":   boolParam("Press Enter"),
				"button":   stringParam("Button: left/right"),
				"menuPath": stringParam("Menu path, comma-separated"),
			},
		},
		desktopActuatorHandler,
	)
}

func desktopActuatorHandler(args map[string]interface{}) *ToolResult {
	action := strArg(args, "action")
	log.Printf("[desktop] action=%s args=%v", action, args)

	// 安全检查：只允许已知的操作类型
	allowedActions := map[string]bool{
		"click": true, "type_text": true,
		"get_window_tree": true, "get_menu_bar_tree": true,
		"click_menu_item": true, 
	}
	if !allowedActions[action] {
		return errorResult(fmt.Sprintf("不支持的操作: %s", action))
	}

	// 构造传给 Python 脚本的 payload
	payload := map[string]interface{}{"action": action}
	for _, k := range []string{"x", "y", "text", "button", "menuPath"} {
		if v, ok := args[k]; ok {
			payload[k] = v
		}
	}
	if v, ok := args["submit"]; ok {
		payload["submit"] = v
	}

	input, _ := json.Marshal(payload)

	cmd := exec.Command("python3", "/Users/dc/Desktop/0/scripts/desktop_actuator.py")
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.Output()
	if err != nil {
		return errorResult(fmt.Sprintf("桌面操作失败: %v", err))
	}

	var result struct {
		Status  string `json:"status"`
		Summary string `json:"summary"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return errorResult(fmt.Sprintf("解析结果失败: %v", err))
	}
	if result.Status == "error" || result.Error != "" {
		return errorResult(result.Error)
	}
	if result.Summary != "" {
		return successResult(result.Summary)
	}
	return successResult(fmt.Sprintf("桌面操作 %s 执行成功", action))
}

/* ─── document_extract L3 工具 ──────────────

   吸收自 FangLab's document_extract.py。
   从各种文件格式中提取文本内容（txt/md/docx/pdf/html/csv 等）。
*/

func registerDocumentTools() {
	Register("document_extract", "Extract text content from documents (txt, md, pdf, docx, csv, html, code). Input: file path. Output: extracted text.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path":     stringParam("Path to the document file"),
				"max_chars": intParam("Maximum characters to extract (default 12000)"),
			},
		},
		documentExtractHandler,
	)
}

func documentExtractHandler(args map[string]interface{}) *ToolResult {
	path := strArg(args, "path")
	if path == "" {
		return errorResult("path is required")
	}

	maxChars := 12000
	if m, ok := args["max_chars"]; ok {
		if mi, ok := m.(float64); ok {
			maxChars = int(mi)
		}
	}

	input, _ := json.Marshal(map[string]interface{}{
		"path":      path,
		"max_chars": maxChars,
	})

	cmd := exec.Command("python3", "/Users/dc/Desktop/0/scripts/document_extract.py")
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.Output()
	if err != nil {
		return errorResult(fmt.Sprintf("document extraction failed: %v", err))
	}

	var result struct {
		Status  string `json:"status"`
		Summary string `json:"summary"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return errorResult(fmt.Sprintf("parse error: %v", err))
	}
	if result.Status == "error" || result.Error != "" {
		return errorResult(result.Error)
	}
	if result.Summary != "" {
		return successResult(result.Summary)
	}
	return successResult("document extracted successfully")
}
