// Package llmguard 是 beishan-core 的 LLM 行为契约层（L3）。
//
// 设计动机：
//   原本所有 LLM 约束规则都散落在 workflow YAML 的提示词里（V25 标准），
//   每个 workflow 自己写一遍 evidence/risk_register/反偷懒 规则。
//   缺点：
//     1. 规则无法集中维护，改一处要扫所有 yaml
//     2. plugin 直接调 llm.* 时没有任何约束兜底
//     3. 同一个规则在不同地方写法不一致，LLM 解释也不一致
//
// 设计目标：
//   提供一个"LLM 行为契约"中间件，所有 LLM 调用通过 Contract 声明约束，
//   框架统一注入基线提示词 + 输出校验 + 重试 + 可选的 critique-revise。
//
// 强度分层（按调用成本递增）：
//   层1 提示词规则     — 注入基线 system prompt（零成本）
//   层2 输出格式校验   — 解析后校验，违规带反馈重试（一次重试成本）
//   层3 Critique-Revise — 二次调用让 LLM 自审改写（约翻倍成本）
//
// 使用示例：
//
//	reply, usage, err := llmguard.Chat(messages, llmguard.Contract{
//	    OutputFormat:    "json",
//	    JSONSchema:      "findings,risk_register",
//	    RequireEvidence: true,
//	    MaxRetries:      1,
//	}, 60*time.Second)
//
// 与 internal/llm 的关系：
//   llmguard.Chat 是高层入口，内部仍调用 llm.ChatCompletionWithUsage。
//   plugin 仍可直接调 llm.* (用于不需要契约约束的低阶场景，如 router 路由判断)。
//   推荐所有"用户可见输出"的 LLM 调用走 llmguard.Chat。
//
// 与 V25 workflow standard 的关系：
//   V25 是工作流文本级约束（每个 yaml 自己写）。
//   llmguard 是框架级约束（一次声明，处处生效）。
//   未来 V25 的提示词规则可迁移到 Contract 字段（"配置代替文本"）。
package llmguard

// Contract 描述一次 LLM 调用的约束契约。
//
// 字段语义按强度分组：
//   - 提示词类（AntiLazy/RequireEvidence）→ 注入 system prompt 基线
//   - 格式类（OutputFormat/JSONSchema）  → 校验 + 重试
//   - 流程类（Critique/MaxRetries）       → 二次调用 / 重试次数
//
// 安全默认：
//   零值 Contract{} 等价于"无任何约束"，行为与直接调 llm.ChatCompletionWithUsage 一致。
//   调用方按需启用各字段，不会因为忘填字段产生副作用。
type Contract struct {
	// OutputFormat 期望的输出格式。
	//   ""           — 不校验（默认）
	//   "json"       — 解析后必须是合法 JSON（自动剥离 ```json 包裹）
	//   "yaml"       — 解析后必须是合法 YAML（自动剥离 ```yaml 包裹）
	//   "markdown"   — 不校验，仅用作语义标记
	//   "free_text"  — 不校验，仅用作语义标记
	//
	// 注意：当前是"事后校验+重试"策略。
	// 未来 provider 支持结构化输出 API 后（DeepSeek/OpenAI response_format），
	// 这里可升级为"生成时强制"，强度从层2 提升到层2.5。
	OutputFormat string

	// RequiredFields 可选，输出必须包含的顶层字段名（逗号分隔）。
	// 例：RequiredFields: "findings,risk_register"
	// 适用于 OutputFormat="json" 和 OutputFormat="yaml"。
	// 仅做字段存在性检查，不做类型/嵌套校验（避免引入完整 schema validator 依赖）。
	// 留空表示只校验格式可解析性。
	RequiredFields string

	// RequireEvidence 强制输出包含证据等级标注（E1/E2/E3/E4 或"证据"字样）。
	// 对应 V25 标准 §1（docs/V25_WORKFLOW_STANDARD.md）。
	// 适用于分析类/决策类任务。
	RequireEvidence bool

	// AntiLazy 注入反偷懒+反编造基线提示。
	// 规则：
	//   - 不许说"将会做"/"可以做"，只能说"已做"或"做不到"+具体原因
	//   - 不许编造，不知道就说"不知道"
	//   - 引用必须有来源（文件/行号/对话ID/URL）
	// 适用于所有用户可见输出（聊天主回答、报告生成等）。
	AntiLazy bool

	// Critique 启用 critique-revise 二次调用。
	//   1. 第一遍生成
	//   2. 让 LLM 用契约规则审查第一遍输出
	//   3. 有问题则让 LLM 重写
	// 成本约翻倍，仅推荐用于高价值场景（决策建议/分析报告/审计）。
	Critique bool

	// MaxRetries 输出不符合契约时的重试次数（默认 0 = 不重试）。
	// 重试在同一个 provider 上做，附加错误反馈到 messages 后重新生成。
	// 建议值：JSON 类场景填 1（DeepSeek/Local 偶尔输出 markdown 包裹）。
	MaxRetries int
}
