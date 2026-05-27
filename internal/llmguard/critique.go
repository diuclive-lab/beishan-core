package llmguard

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"beishan/internal/llm"
)

// critiqueRevise 二次调用让 LLM 审查并改进自己的输出。
//
// 三段流程：
//  1. critique：让 LLM 用 Contract 规则审查第一次输出，列出问题
//  2. 若无问题 → 直接返回原输出
//  3. 若有问题 → 让 LLM 根据问题列表重写
//
// 设计动机：
//   人类工作中常见的"先写后改"模式。LLM 单次生成容易遗漏规则，
//   显式让它"换个视角"审查自己，能纠正约 30-50% 的偷懒/幻觉。
//   这是 Anthropic 的 Constitutional AI 思想的轻量实现。
//
// 成本：
//   每次 critique-revise 多调用 1-2 次 LLM（critique + 可选 revise），
//   总成本约翻倍。仅推荐：决策建议/审计报告/复杂分析等高价值场景。
//
// 失败语义：
//   critique 自身出错（网络/解析）不会失败整个 Chat，
//   只是回退到原输出。这避免 critique 成为新的失败点。
//
// fn 参数：
//   传入的 chatFunction 与 chatCore 使用同一闭包，
//   保证 critique 走相同的 provider（不会一半 DeepSeek 一半 Local）。
func critiqueRevise(origMessages []llm.ChatMessage, firstOutput string, c Contract, timeout time.Duration, fn chatFunction) (string, *llm.Usage, error) {
	// ── 第1步：critique ─────────────────────────────────────
	critiquePrompt := buildCritiquePrompt(c, firstOutput)
	critMessages := []llm.ChatMessage{
		{Role: "system", Content: "你是严格的输出审查员。按用户给的规则审查输出，只输出 JSON。"},
		{Role: "user", Content: critiquePrompt},
	}

	critOutput, critUsage, err := fn(critMessages, timeout)
	if err != nil {
		// critique 自身失败：返回原输出，附带说明
		return firstOutput, critUsage, fmt.Errorf("critique 调用失败: %w", err)
	}

	// 解析 critique 结果
	cleaned := stripMarkdownFences(critOutput)
	var critResult struct {
		HasIssues bool     `json:"has_issues"`
		Issues    []string `json:"issues"`
	}
	if err := json.Unmarshal([]byte(cleaned), &critResult); err != nil {
		// critique 自己输出格式错（讽刺地违反了它自己审查的规则），
		// 视为"无问题"，回退原输出
		return firstOutput, critUsage, nil
	}
	if !critResult.HasIssues || len(critResult.Issues) == 0 {
		return firstOutput, critUsage, nil
	}

	// ── 第2步：revise ───────────────────────────────────────
	reviseMsg := fmt.Sprintf(`你上一次的输出被审查发现以下问题：

%s

请重新输出，修复这些问题。注意保持原任务的核心信息，不要因为修问题改变结论方向。`,
		formatIssueList(critResult.Issues))

	// 在原 messages 后追加 assistant 输出 + revise 指令
	reviseMessages := make([]llm.ChatMessage, 0, len(origMessages)+2)
	reviseMessages = append(reviseMessages, origMessages...)
	reviseMessages = append(reviseMessages,
		llm.ChatMessage{Role: "assistant", Content: firstOutput},
		llm.ChatMessage{Role: "user", Content: reviseMsg})

	revised, reviseUsage, err := fn(reviseMessages, timeout)
	totalUsage := critUsage
	if reviseUsage != nil {
		if totalUsage == nil {
			totalUsage = &llm.Usage{}
		}
		accumulateUsage(totalUsage, reviseUsage)
	}
	if err != nil {
		return firstOutput, totalUsage, fmt.Errorf("revise 调用失败: %w", err)
	}
	return revised, totalUsage, nil
}

// buildCritiquePrompt 构造 critique 阶段的提示词。
//
// 设计要点：
//   - 明确列出 Contract 启用的规则，让 LLM 有具体审查依据
//   - 要求 JSON 输出，方便后续解析
//   - 不让 LLM 自由发挥，只针对契约规则审查
func buildCritiquePrompt(c Contract, output string) string {
	var rules []string
	if c.OutputFormat == "json" {
		rules = append(rules, "1. 必须是合法 JSON，不能有 markdown 代码块包裹")
	}
	if c.JSONSchema != "" {
		rules = append(rules, fmt.Sprintf("2. JSON 必须包含字段：%s", c.JSONSchema))
	}
	if c.RequireEvidence {
		rules = append(rules, "3. 每条结论必须有 E1/E2/E3/E4 证据等级或\"证据\"字样")
	}
	if c.AntiLazy {
		rules = append(rules, "4. 不能用\"将会做\"等未完成语态；不能编造事实；引用必须有来源")
	}

	rulesText := strings.Join(rules, "\n")
	if rulesText == "" {
		rulesText = "（无强制规则，请评估输出是否完整、可读、有据）"
	}

	return fmt.Sprintf(`请审查以下 LLM 输出，按规则列出所有问题。

规则：
%s

待审查输出：
%s

输出严格 JSON（不要 markdown 包裹，不要解释文字）：
{"has_issues": true/false, "issues": ["问题1的具体描述", "问题2的具体描述"]}

如果没有问题，输出 {"has_issues": false, "issues": []}`,
		rulesText, output)
}

// formatIssueList 把 issue 数组格式化成带序号的列表。
// 方便 LLM 在 revise 时逐条对应处理。
func formatIssueList(issues []string) string {
	var sb strings.Builder
	for i, issue := range issues {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, issue))
	}
	return strings.TrimRight(sb.String(), "\n")
}
