package llmguard

import (
	"encoding/json"
	"fmt"
	"strings"
)

// validateOutput 检查 LLM 输出是否符合 Contract。
// 返回 nil 表示通过；返回 error 时，错误消息会被作为反馈
// 拼接到下一轮请求中，让 LLM 知道哪里违规。
//
// 校验顺序（从轻到重，提前失败减少无效检查）：
//  1. OutputFormat="json" → 必须是合法 JSON（自动剥离 markdown 包裹）
//  2. JSONSchema 字段名 → 顶层字段必须存在
//  3. RequireEvidence → 必须包含 E1-E4 或"证据"字样
//
// 设计选择：只做"框架级"通用校验，业务层 schema 仍由调用方处理。
// 这避免 llmguard 引入完整 JSON Schema validator 依赖。
func validateOutput(output string, c Contract) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("输出为空")
	}

	// ── 检查1：JSON 格式 ─────────────────────────────────────
	if c.OutputFormat == "json" {
		cleaned := stripMarkdownFences(output)
		if !json.Valid([]byte(cleaned)) {
			return fmt.Errorf("输出不是合法 JSON（前 100 字符: %q）", truncate(output, 100))
		}

		// ── 检查2：JSONSchema 字段存在 ───────────────────────
		if c.JSONSchema != "" {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(cleaned), &obj); err != nil {
				// 顶层不是对象（可能是数组），跳过字段检查
				// 这是允许的，不视为契约违规
			} else {
				for _, field := range strings.Split(c.JSONSchema, ",") {
					field = strings.TrimSpace(field)
					if field == "" {
						continue
					}
					if _, ok := obj[field]; !ok {
						return fmt.Errorf("输出 JSON 缺少必需字段 %q", field)
					}
				}
			}
		}
	}

	// ── 检查3：证据等级标注 ───────────────────────────────
	if c.RequireEvidence {
		if !hasEvidenceMarker(output) {
			return fmt.Errorf("输出缺少证据等级标注（应包含 E1/E2/E3/E4 或\"证据\"字样）")
		}
	}

	return nil
}

// stripMarkdownFences 去除 LLM 偶尔返回的 ```json 或 ``` 代码块包裹。
// 这是 LLM 输出 JSON 时常见的"违反指示"问题，单独处理避免误判。
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```yaml")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// hasEvidenceMarker 检查文本是否包含证据等级标注。
// 接受 E1/E2/E3/E4 大小写不敏感，或中文"证据"字样。
func hasEvidenceMarker(s string) bool {
	lower := strings.ToLower(s)
	for _, marker := range []string{"e1", "e2", "e3", "e4"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(s, "证据")
}

// truncate 截断字符串供错误消息使用，避免一个超长输出污染日志。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
