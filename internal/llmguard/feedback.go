package llmguard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// buildRetryFeedback 为重试轮次生成结构化反馈消息。
//
// 比通用"违反契约，请重新输出"更好的地方：
//   - 字段缺失时，告诉 LLM 已有哪些字段（防止重写时把正确字段也丢掉）
//   - 解析失败时，给出具体的修正指示（不要 markdown 包裹、检查缩进等）
//   - 按违规类型分类，每类给出最有针对性的行动指令
//
// 设计原则：
//   - 反馈文本短而精确，冗长的反馈会干扰 LLM 的主任务
//   - 不重复整个 Contract 规则，只说这次具体违反了什么
//   - "✗ 违规" + "✓ 已有" + "修正" 三段结构，LLM 易于解析
func buildRetryFeedback(output string, violation error, c Contract) string {
	msg := violation.Error()

	switch c.OutputFormat {
	case "json":
		return buildJSONFeedback(output, msg)
	case "yaml":
		return buildYAMLFeedback(output, msg)
	}

	// 无格式约束（evidence / anti-lazy 违规）
	return fmt.Sprintf("上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：按要求重新输出，不要省略规则要求的内容。", msg)
}

// buildJSONFeedback 生成 JSON 相关的结构化反馈。
func buildJSONFeedback(output, violationMsg string) string {
	// 字段缺失：告知现有字段，精确指出缺失的
	if strings.Contains(violationMsg, "缺少必需字段") {
		cleaned := stripMarkdownFences(output)
		var obj map[string]interface{}
		if json.Unmarshal([]byte(cleaned), &obj) == nil {
			present := sortedKeys(obj)
			if len(present) > 0 {
				missing := extractMissingFields(violationMsg)
				return fmt.Sprintf(
					"上一次输出违反了契约规则。\n\n✗ 违规：%s\n✓ 已有字段：%s\n\n修正：补全缺失字段 %s，保持其他字段不变，重新输出完整 JSON。",
					violationMsg,
					strings.Join(present, ", "),
					missing,
				)
			}
		}
		return fmt.Sprintf(
			"上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：补全缺失字段，重新输出完整 JSON。",
			violationMsg,
		)
	}

	// JSON 解析失败：大概率是 markdown 包裹
	if strings.Contains(violationMsg, "不是合法 JSON") {
		hint := "直接输出 JSON，不要加 ```json 代码块或任何解释文字。"
		if strings.Contains(output, "```") {
			hint = "你的输出包含了 ```json 代码块，请去掉，直接输出裸 JSON。"
		}
		return fmt.Sprintf(
			"上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：%s",
			violationMsg, hint,
		)
	}

	return fmt.Sprintf("上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：按 JSON 格式要求重新输出。", violationMsg)
}

// buildYAMLFeedback 生成 YAML 相关的结构化反馈。
func buildYAMLFeedback(output, violationMsg string) string {
	// 字段缺失：告知现有字段，精确指出缺失的
	if strings.Contains(violationMsg, "缺少必需字段") {
		cleaned := stripMarkdownFences(output)
		var obj map[string]interface{}
		if yaml.Unmarshal([]byte(cleaned), &obj) == nil {
			present := sortedKeys(obj)
			if len(present) > 0 {
				missing := extractMissingFields(violationMsg)
				return fmt.Sprintf(
					"上一次输出违反了契约规则。\n\n✗ 违规：%s\n✓ 已有字段：%s\n\n修正：补全缺失字段 %s，保持其他字段不变，重新输出完整 YAML。",
					violationMsg,
					strings.Join(present, ", "),
					missing,
				)
			}
		}
		return fmt.Sprintf(
			"上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：补全缺失字段，重新输出完整 YAML。",
			violationMsg,
		)
	}

	// YAML 解析失败
	if strings.Contains(violationMsg, "不是合法 YAML") {
		hint := "检查 YAML 缩进和语法，直接输出纯 YAML，不加 ```yaml 代码块或解释文字。"
		if strings.Contains(output, "```") {
			hint = "你的输出包含了 ```yaml 代码块，请去掉，直接输出裸 YAML。"
		}
		return fmt.Sprintf(
			"上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：%s",
			violationMsg, hint,
		)
	}

	return fmt.Sprintf("上一次输出违反了契约规则。\n\n✗ 违规：%s\n\n修正：按 YAML 格式要求重新输出。", violationMsg)
}

// sortedKeys 返回 map 的键列表（排序，结果稳定）。
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// extractMissingFields 从违规消息中提取缺失字段部分。
// 例："输出 JSON 缺少必需字段：findings, risk_register" → "findings, risk_register"
func extractMissingFields(msg string) string {
	if idx := strings.LastIndex(msg, "："); idx >= 0 {
		return strings.TrimSpace(msg[idx+len("："):])
	}
	return msg
}
