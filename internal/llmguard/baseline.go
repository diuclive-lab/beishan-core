package llmguard

import (
	"strings"

	"beishan/internal/llm"
)

// 基线提示词。这是框架级 LLM 行为约束的"文本载体"。
//
// 设计原则：
//   - 短：基线越短，对 LLM 主任务的干扰越小
//   - 强：每条规则措辞明确，留下重试时可指出的违规点
//   - 正交：每条规则之间互不矛盾，可独立启用
//
// 维护说明：
//   修改基线文本前先评估对所有调用方的影响。
//   这是"一处改，处处生效"的杠杆点，比改 yaml 提示词风险高。
//   建议：调整时同步更新 docs/V25_WORKFLOW_STANDARD.md，保持文本一致。

// baselineAntiLazy 反偷懒+反编造基线（对应 Contract.AntiLazy）。
//
// 三条核心规则：
//   1. 强制完成语义：不允许使用"将要"语态描述未做的事
//   2. 反编造：明确说"不知道"比编造内容更可取
//   3. 引用强制：所有外部引用必须可追溯
const baselineAntiLazy = `[基线规则·防偷懒]
1. 禁止使用"将会做"/"可以做"等未完成语态。只能说"已做"+证据，或"做不到"+具体原因。
2. 禁止编造事实。不知道的内容明确说"不知道"或"无相关资料"，不要凭空生成。
3. 引用外部信息必须附来源：文件名/行号/对话ID/URL。无来源的判断标"推测："前缀。`

// baselineEvidence 证据等级强制（对应 Contract.RequireEvidence）。
//
// 与 V25 §1 同步，是分析/决策类任务的核心防"幻觉"机制。
const baselineEvidence = `[基线规则·证据等级]
每条结论必须标注证据等级：
  E1 = 直接证据（引用原文/行号/数据）
  E2 = 测试或行为推断
  E3 = 历史或经验推断
  E4 = 推测（必须以"推测："前缀注明）`

// baselineJSON JSON 输出强制（对应 Contract.OutputFormat="json"）。
//
// 当前 LLM API 普遍支持但形式不统一（DeepSeek 用 response_format=json_object，
// OpenAI 用 json_schema，Anthropic 用 tool_use 间接达成）。
// 暂以提示词强制为主，未来按 provider 升级到原生 API。
const baselineJSON = `[基线规则·JSON 输出]
严格输出 JSON：不要 markdown 代码块（如 ` + "```" + `json），不要解释文字，不要前后缀。
JSON 必须可被标准 JSON.parse 解析。`

// baselineYAML YAML 输出强制（对应 Contract.OutputFormat="yaml"）。
const baselineYAML = `[基线规则·YAML 输出]
严格输出纯 YAML：不要 markdown 代码块（如 ` + "```" + `yaml），不要解释文字，不要前后缀。
YAML 必须是合法语法，可被 yaml.Unmarshal 无错误解析。`

// buildBaseline 根据 Contract 拼接需要注入的基线提示词。
// 返回空字符串表示无需注入任何基线（零值 Contract 的语义）。
func buildBaseline(c Contract) string {
	var parts []string
	if c.AntiLazy {
		parts = append(parts, baselineAntiLazy)
	}
	if c.RequireEvidence {
		parts = append(parts, baselineEvidence)
	}
	switch c.OutputFormat {
	case "json":
		parts = append(parts, baselineJSON)
		if c.RequiredFields != "" {
			parts = append(parts, "[基线规则·字段] 输出 JSON 必须包含顶层字段："+c.RequiredFields)
		}
	case "yaml":
		parts = append(parts, baselineYAML)
		if c.RequiredFields != "" {
			parts = append(parts, "[基线规则·字段] 输出 YAML 必须包含顶层字段："+c.RequiredFields)
		}
	}
	return strings.Join(parts, "\n\n")
}

// injectBaseline 把基线提示词融合进 messages。
//
// 策略（优先级从高到低）：
//  1. 如果存在 system message，追加到现有 system 内容末尾（保留调用方语义）
//  2. 否则插入新的 system message 到最前
//
// 不修改入参 messages（防止上游持有的 slice 被污染）。
func injectBaseline(messages []llm.ChatMessage, baseline string) []llm.ChatMessage {
	if baseline == "" {
		return messages
	}
	// 复制避免修改入参
	cp := make([]llm.ChatMessage, len(messages))
	copy(cp, messages)

	for i, m := range cp {
		if m.Role == "system" {
			cp[i].Content = m.Content + "\n\n" + baseline
			return cp
		}
	}
	// 无 system message，插入新的到开头
	return append([]llm.ChatMessage{{Role: "system", Content: baseline}}, cp...)
}
