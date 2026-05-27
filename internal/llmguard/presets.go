package llmguard

// ─── 维度化契约构造 ─────────────────────────────────────────────
//
// 设计哲学（核心洞察）：
//   「强制性应该按维度分配，不是整体调高/调低。」
//
//   - 结构（output schema）：机器可校验的，强制不影响内容质量，可满强度
//   - 内容（reasoning quality）：主观的，强制过头让模型变笨，半强度足够
//   - 事实（grounding/facts）：客观的可校验，但 critique 成本翻倍，按需启用
//
// 维度 → 层级映射：
//   ForStructure → 层 2 强制结构（OutputFormat + JSONSchema + retry）
//   ForContent   → 层 1 基线（AntiLazy 反偷懒+反编造+引用源）
//   ForFacts     → 层 1+4 事实强制（RequireEvidence + Critique 自审）
//
// 与 V25 工作流标准的映射：
//   V25 §1 evidence    → ForFacts()
//   V25 §2 JSON schema → ForStructure(...)
//   V25 §4 反偷懒      → ForContent()
//   V25 全合规         → ForStructure(...).WithContent().WithFacts()
//
// 使用示例：
//
//	// 结构化分析：JSON 结构 + 内容半强制 + 事实 critique
//	c := llmguard.ForStructure("json", "findings,risk_register", 1).
//	    WithContent().
//	    WithFacts()
//
//	// 自然语言聊天：只要内容质量
//	c := llmguard.ForContent()
//
//	// 单词分类：什么都不要（零值，向后兼容）
//	c := llmguard.Contract{}

// ForStructure 创建结构维度契约（层 2 强制）。
//
// 哲学：结构是机器可校验的，强制约束不影响内容质量，所以可以满强度。
// 模型按 schema 输出 100% 听话即可，不需要给"发挥空间"。
//
// 参数：
//   format  — 输出格式，目前支持 "json"。空字符串等价于 ForContent + 零结构。
//   fields  — 必须存在的顶层字段（逗号分隔），例如 "findings,risk_register"
//   retries — 输出违规时的重试次数，建议 1（DeepSeek/Local 偶尔被 markdown 包裹）
//
// 注意：当前是事后校验+重试策略。未来接入 provider 原生 response_format API
// 后，强度从层 2 提升到层 2.5（生成时强制）。
func ForStructure(format, fields string, retries int) Contract {
	return Contract{
		OutputFormat: format,
		JSONSchema:   fields,
		MaxRetries:   retries,
	}
}

// ForContent 创建内容维度契约（层 1 半强制）。
//
// 哲学：内容质量是主观的。强制过头会让模型变笨（生硬重复模板词）。
// 基线注入 AntiLazy 规则兜底就够，留出发挥空间。
//
// 仅启用 AntiLazy 基线（反偷懒+反编造+引用源）。
// 不启用 Critique（成本翻倍，留给调用方按需启用）。
func ForContent() Contract {
	return Contract{
		AntiLazy: true,
	}
}

// ForFacts 创建事实维度契约（层 1+4 强制）。
//
// 哲学：事实是客观的，可以也应该校验。
// 启用 RequireEvidence 基线（要求 E1-E4 证据等级）+ AntiLazy 兜底
// + Critique 二次自审（LLM 检查输出是否真的有出处）。
//
// 成本：约翻倍（critique 多一次 LLM 调用）。
// 推荐场景：分析报告、决策建议、审计任务、安全检查。
func ForFacts() Contract {
	return Contract{
		RequireEvidence: true,
		AntiLazy:        true,
		Critique:        true,
	}
}

// ─── 维度叠加（fluent API） ──────────────────────────────────
//
// 设计：每个 With* 都是幂等的，不会破坏其他维度的设置。
// 这样调用方可以自由组合而不用担心顺序。
//
//	c := ForStructure(...).WithContent().WithFacts() // 三维度全开
//	c := ForFacts().WithStructure(...)               // 等价上一行

// WithStructure 在现有契约上叠加结构维度。
// 如果已经设置了 MaxRetries，取较大值（不降低重试预算）。
func (c Contract) WithStructure(format, fields string, retries int) Contract {
	c.OutputFormat = format
	c.JSONSchema = fields
	if retries > c.MaxRetries {
		c.MaxRetries = retries
	}
	return c
}

// WithContent 在现有契约上叠加内容维度（AntiLazy 基线）。
func (c Contract) WithContent() Contract {
	c.AntiLazy = true
	return c
}

// WithFacts 在现有契约上叠加事实维度（RequireEvidence + AntiLazy + Critique）。
//
// 注意：会强制启用 Critique（成本翻倍）。
// 如果只想要 evidence 标注但不想要 critique，使用 WithEvidence() 替代。
func (c Contract) WithFacts() Contract {
	c.RequireEvidence = true
	c.AntiLazy = true
	c.Critique = true
	return c
}

// WithEvidence 仅启用证据等级标注（层 1 半强制，不启用 critique）。
// 用于只想要 evidence 但不想付 critique 成本的场景。
func (c Contract) WithEvidence() Contract {
	c.RequireEvidence = true
	return c
}

// WithCritique 显式启用 critique-revise（层 4 强制）。
// 用于已有契约上单独叠加 critique，例如：
//
//	ForStructure("json", "x", 1).WithCritique()  // 结构强制 + critique
func (c Contract) WithCritique() Contract {
	c.Critique = true
	return c
}

// WithRetries 覆盖重试次数。
// 用于在 preset 之上调整重试预算。
func (c Contract) WithRetries(n int) Contract {
	c.MaxRetries = n
	return c
}
