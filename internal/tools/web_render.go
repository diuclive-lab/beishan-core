package tools

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
)

/* ─── web_render L3 工具 ───────────────────────

   用 Playwright 驱动 headless Chromium 渲染 JavaScript 页面并提取正文。
   适用于公众号文章、SPA 页面等需要 JS 渲染的场景。

   浏览器由 Playwright 自动管理（首次运行时下载），不依赖系统安装。
*/

type RenderOutput struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func webRenderHandler(args map[string]interface{}) *ToolResult {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return errorResult("url is required")
	}
	waitSec := 5
	if w, ok := args["wait"].(float64); ok && w > 0 {
		waitSec = int(w)
		if waitSec > 30 {
			waitSec = 30
		}
	}

	pw, err := playwright.Run()
	if err != nil {
		out := RenderOutput{URL: rawURL, Success: false, Error: fmt.Sprintf("playwright init failed: %v", err)}
		b, _ := json.Marshal(out)
		return successResult(string(b))
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		out := RenderOutput{URL: rawURL, Success: false, Error: fmt.Sprintf("launch browser failed: %v", err)}
		b, _ := json.Marshal(out)
		return successResult(string(b))
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		out := RenderOutput{URL: rawURL, Success: false, Error: fmt.Sprintf("new page failed: %v", err)}
		b, _ := json.Marshal(out)
		return successResult(string(b))
	}

	if _, err = page.Goto(rawURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(float64((waitSec + 15) * 1000)),
	}); err != nil {
		out := RenderOutput{URL: rawURL, Success: false, Error: fmt.Sprintf("navigate failed: %v", err)}
		b, _ := json.Marshal(out)
		return successResult(string(b))
	}

	// 额外等待 JS 渲染
	time.Sleep(time.Duration(waitSec) * time.Second)

	// 获取标题和正文
	title, _ := page.Title()
	bodyText, err := page.Evaluate(`document.body.innerText`)
	if err != nil {
		out := RenderOutput{URL: rawURL, Success: false, Error: fmt.Sprintf("extract content failed: %v", err)}
		b, _ := json.Marshal(out)
		return successResult(string(b))
	}

	content := ""
	if s, ok := bodyText.(string); ok {
		content = s
	}

	if len(content) > 100000 {
		content = content[:100000] + fmt.Sprintf("\n... [truncated, total %d chars]", len(content))
	}

	out := RenderOutput{
		URL:     rawURL,
		Title:   title,
		Content: content,
		Success: true,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return successResult(string(b))
}

func registerWebRenderTool() {
	Register("web_render", "用 Playwright headless 浏览器渲染 JS 页面并提取正文文本。适用于公众号文章、SPA 页面。浏览器由 Playwright 自动管理，不依赖系统安装。",
		map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties": map[string]interface{}{
				"url": stringParam("需要渲染的页面 URL"),
				"wait": map[string]interface{}{
					"type":        "integer",
					"description": "等待 JS 渲染的秒数（默认 5，最大 30）",
				},
			},
			"required": []string{"url"},
		},
		webRenderHandler,
	)
}
