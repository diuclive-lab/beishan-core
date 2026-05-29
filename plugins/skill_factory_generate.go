package plugins

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/llmguard"
)

// ─── 输出类型体系 ──────────────────────────────────────────
//
// 核心设计理念：
//   工作流的分类维度是「用户最终看到什么」，而不是「步骤结构是什么」。
//   同样是"分析项目"，输出是报告 vs 统计表 vs 决策建议，结构差异极大。
//
// 分类流程：
//   用户自然语言 → classifyOutputType(LLM) → 确定 outputTypeID
//   → 选对应 WorkflowOutputType.Template → LLM 填充细节
//   → validateV25() 强化校验 → 写入 workflows/
//
// 扩展说明：
//   新增输出类型时，在 workflowOutputTypes 追加一项即可。
//   不需要修改生成流程，classifyOutputType 会自动感知新类型。

// WorkflowOutputType 定义一种工作流输出类型的完整描述。
type WorkflowOutputType struct {
	ID           string   // 类型标识符，英文
	Name         string   // 中文名，展示用
	Description  string   // 适用场景，给 LLM 判断用
	Examples     []string // 典型请求例子，帮助 LLM 准确分类
	OutputTarget string   // 默认输出渠道（chat/dashboard/notify/knowledge）
	Template     string   // YAML 骨架，变量用 {{.VarName}} 占位，LLM 负责填充
}

// workflowOutputTypes 6 种输出类型。
// 顺序影响 LLM 选择时的优先级（靠前的描述越具体越容易命中）。
var workflowOutputTypes = []WorkflowOutputType{
	{
		ID:           "structured_analysis",
		Name:         "结构化分析",
		Description:  "分条列点的发现+证据，适合需要可追溯来源的场景：安全审计、合规检查、依赖分析、代码质量扫描",
		Examples:     []string{"分析这段代码的安全问题", "检查项目依赖有没有漏洞", "对这份合同做合规审查"},
		OutputTarget: "chat",
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   collect      → 收集待分析对象（优先 memory_plugin/search_plugin，禁止 terminal 作为首选）
		//   analyze      → LLM 逐条输出 findings + risk_register，每条强制标 evidence 等级
		//   gap_analysis → LLM 声明本次分析未覆盖的范围（V25 分析类强制项，防止 LLM 隐藏盲区）
		//   format       → 把 findings+gap 合并格式化为 Markdown 报告
		//
		// V25 合规点（docs/V25_WORKFLOW_STANDARD.md）：
		//   §1 evidence 等级：E1=直接证据 E2=测试推断 E3=历史推断 E4=推测
		//   §2.2 risk_register：风险类必须有 severity+category+mitigation
		//   §2.3 gap_analysis：分析类必须声明未覆盖范围
		//   §2.4 Go 工具优先：collect 步骤用 memory_plugin，terminal 只作降级
		Template: `# 治理框架引用：
#   - 证据等级 E1-E4 → docs/ABSORPTION_GOVERNANCE.md §1
#   - 风险分类       → docs/ABSORPTION_GOVERNANCE.md §3
output_target: chat

steps:
  - id: collect
    plugin: {{.CollectPlugin}}
    type: {{.CollectType}}
    timeout: {{.CollectTimeout}}
    on_error: analyze
    inputs:
      {{.CollectInputKey}}: "${input}"
    next: analyze

  - id: analyze
    plugin: think_plugin
    type: chat
    timeout: 180
    retry: 1
    on_error: gap_analysis
    inputs:
      mode: "no_retrieval"
      message: >-
        {{.AnalyzePrompt}}

        待分析内容：
        ${steps.collect.output}

        V25 强制要求：每条发现必须标注 evidence 等级。
          E1 = 内容中直接可见的证据（引用原文/行号）
          E2 = 通过测试或行为推断
          E3 = 历史记录或讨论中推断
          E4 = 推测，必须注明 hypothesis 标记

        输出严格 JSON（不要其他文字）：
        {
          "findings": [
            {
              "item": "发现项描述",
              "severity": "high/medium/low",
              "evidence": "E1",
              "source": "来源行号/文件名/对话ID",
              "suggestion": "修复建议"
            }
          ],
          "risk_register": [
            {
              "risk": "风险描述",
              "category": "security/correctness/performance/compatibility/dependency",
              "severity": "critical/high/medium/low",
              "mitigation": "应对措施",
              "status": "open"
            }
          ]
        }
    next: gap_analysis

  - id: gap_analysis
    plugin: think_plugin
    type: chat
    timeout: 60
    on_error: format
    inputs:
      mode: "no_retrieval"
      message: >-
        基于以下分析结果，声明本次分析未覆盖的范围。
        禁止省略此步骤，禁止输出空的 skipped_areas。

        分析结果：${steps.analyze.output}
        原始输入：${input}

        输出严格 JSON（不要其他文字）：
        {
          "skipped_areas": [
            {"area": "未分析的区域或维度", "reason": "跳过原因（数据不足/超出范围/需人工介入）"}
          ],
          "coverage_pct": 0.0,
          "partial_failure": false
        }
    next: format

  - id: format
    plugin: think_plugin
    type: chat
    timeout: 60
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        将以下分析结果格式化为 Markdown 报告：

        发现与风险：${steps.analyze.output}
        分析盲区：${steps.gap_analysis.output}

        报告结构：
        1. 执行摘要（2-3 行）
        2. 发现列表（按 severity 降序，标 E 等级）
        3. 风险登记册（risk_register）
        4. 分析盲区声明
        5. 建议下一步行动`,
	},
	{
		ID:           "report",
		Name:         "叙述性报告",
		Description:  "连贯的叙述性文字，适合：代码审查报告、项目评估、技术说明、周报、总结",
		Examples:     []string{"给我写一份代码审查报告", "分析这个项目的技术状态", "总结本周的工作进展"},
		OutputTarget: "chat",
		// 骨架说明：
		//   research → 收集背景资料（可替换）
		//   draft    → LLM 生成报告初稿
		//   refine   → LLM 自我修订，提升质量（可删除以减轻成本）
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   research → 收集背景资料（优先 memory_plugin/search_plugin）
		//   draft    → LLM 生成初稿，每个结论必须有事实支撑
		//   refine   → LLM 自我修订（可删除以降低成本，但推荐保留）
		//
		// V25 合规点（docs/V25_WORKFLOW_STANDARD.md）：
		//   §1 叙述性报告不强制 risk_register，但结论必须有 E3+ 事实依据
		//   §2.4 Go 工具优先：research 步骤用 memory_plugin，terminal 只作降级
		Template: `# 治理框架引用：
#   - 叙述性报告不要求 risk_register，但结论必须有事实依据
#   - 证据等级参考 → docs/ABSORPTION_GOVERNANCE.md §1（E3+ 即可，E4 推测须注明）
output_target: chat

steps:
  - id: research
    plugin: {{.ResearchPlugin}}
    type: {{.ResearchType}}
    timeout: {{.ResearchTimeout}}
    on_error: draft
    inputs:
      {{.ResearchInputKey}}: "${input}"
    next: draft

  - id: draft
    plugin: think_plugin
    type: chat
    timeout: 180
    retry: 1
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        {{.DraftPrompt}}

        背景资料：
        ${steps.research.output}

        要求：
        - 结构清晰，分节阐述
        - 结论在前，细节在后
        - 每个结论必须有背景资料中的事实支撑（纯推测须注明"推测："）
        - 300-800 字
    next: refine

  - id: refine
    plugin: think_plugin
    type: chat
    timeout: 120
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        对以下报告做自我审查和修订：

        初稿：
        ${steps.draft.output}

        修订要点：
        1. 逻辑连贯性
        2. 每个结论是否有资料支撑（没有支撑的推测是否已注明"推测："）
        3. 表达是否简洁
        输出修订后的完整报告，不要输出修订说明。`,
	},
	{
		ID:           "stats",
		Name:         "统计报表",
		Description:  "数字/表格/分布统计，适合：使用量统计、健康度检查、错误率分析、定期巡检",
		Examples:     []string{"统计知识库有多少条各类型的记录", "给我看看今天的 API 使用量", "生成系统健康报告"},
		OutputTarget: "dashboard",
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   collect   → 收集原始数据（tool 调用，返回 JSON/文本）
		//   aggregate → LLM 提取数字指标，输出结构化 JSON
		//   输出路由到 dashboard（前端可轮询 /api/dashboard 接收）
		//
		// V25 合规点（docs/V25_WORKFLOW_STANDARD.md）：
		//   §2 数字来源不明时标注 "estimated"，来源不可验证时标注 "inferred"
		//   §2.4 Go 工具优先：collect 步骤用 memory_plugin/knowledge 等原生工具
		//
		// TODO(output-routing/dashboard): dashboard 推送实装后，
		//   engine 的 routeOutput 会把结果推给前端，当前仅记录日志。
		Template: `# 治理框架引用：
#   - 统计数据来源声明 → docs/ABSORPTION_GOVERNANCE.md §2
#   - 数字不明确时标注 "estimated"，来源不可验证时标注 "inferred"
output_target: dashboard

steps:
  - id: collect
    plugin: {{.CollectPlugin}}
    type: {{.CollectType}}
    timeout: 30
    on_error: aggregate
    inputs:
      {{.CollectInputKey}}: "${input}"
    next: aggregate

  - id: aggregate
    plugin: think_plugin
    type: chat
    timeout: 120
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        {{.AggregatePrompt}}

        原始数据：
        ${steps.collect.output}

        输出 JSON 统计报表（不要其他文字）：
        {
          "title": "报表标题",
          "generated_at": "{{.Timestamp}}",
          "metrics": [
            {
              "name": "指标名",
              "value": 0,
              "unit": "单位",
              "trend": "up/down/stable",
              "source": "数据来源（无法确认时填 estimated）"
            }
          ],
          "summary": "一句话总结"
        }`,
	},
	{
		ID:           "decision_support",
		Name:         "决策建议",
		Description:  "多方案对比+推荐，适合：技术选型、方案评估、风险权衡、是否采用某个依赖",
		Examples:     []string{"帮我选一个合适的数据库", "比较这两个方案的优劣", "评估是否应该升级这个依赖"},
		OutputTarget: "chat",
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   research  → 收集各方案信息（优先 memory_plugin/search_plugin）
		//   compare   → LLM 输出结构化对比表，每条优劣须标注证据等级
		//   recommend → LLM 基于对比给出有理由的推荐
		//
		// V25 合规点（docs/V25_WORKFLOW_STANDARD.md）：
		//   §1 evidence 等级：E1=直接引用 E2=测试推断 E3=历史经验 E4=推测（须注明）
		//   §3 高风险决策在 recommend 后加 human_confirm 步骤（用户确认后才执行）
		//   §2.4 Go 工具优先：research 步骤优先 memory_plugin，terminal 只作降级
		Template: `# 治理框架引用：
#   - 决策依据证据等级 E1-E4 → docs/ABSORPTION_GOVERNANCE.md §1
#   - 高风险决策在 recommend 后加 human_confirm 步骤 → docs/V25_WORKFLOW_STANDARD.md §3
output_target: chat

steps:
  - id: research
    plugin: {{.ResearchPlugin}}
    type: {{.ResearchType}}
    timeout: 30
    on_error: compare
    inputs:
      {{.ResearchInputKey}}: "${input}"
    next: compare

  - id: compare
    plugin: think_plugin
    type: chat
    timeout: 180
    retry: 1
    on_error: recommend
    inputs:
      mode: "no_retrieval"
      message: >-
        {{.ComparePrompt}}

        背景资料：
        ${steps.research.output}

        V25 要求：每条优劣须标注证据等级（E1直接引用/E2测试推断/E3历史经验/E4推测）。
        E4 推测须用括号注明"（推测）"。

        输出结构化对比 JSON（不要其他文字）：
        {
          "options": [
            {
              "name": "方案名",
              "pros": ["优点（E1）"],
              "cons": ["缺点（E2）"],
              "fit_score": 8,
              "evidence_level": "E1/E2/E3（总体依据质量）"
            }
          ]
        }
    next: recommend

  - id: recommend
    plugin: think_plugin
    type: chat
    timeout: 120
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        基于以下对比结果，给出明确推荐和理由：

        原始需求：${input}
        对比结果：${steps.compare.output}

        格式：
        1. 推荐方案（一句话）
        2. 3 条推荐理由（每条说明依据来源）
        3. 1 条注意事项（不确定的点须注明"待验证："）`,
	},
	{
		ID:           "action_result",
		Name:         "执行反馈",
		Description:  "批量执行操作并反馈结果，适合：批量入库、定期清理、数据迁移、自动化运维",
		Examples:     []string{"把这批 URL 都存入知识库", "清理30天前的过期记录", "把今天的日志归档"},
		OutputTarget: "knowledge",
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   validate → 校验输入格式，提前发现问题（可删除，但强烈推荐保留）
		//   execute  → 批量执行核心操作（batch 模式或单次调用）
		//   report   → 汇总执行结果，告知成功/失败数量
		//
		// V25 合规点（docs/V25_WORKFLOW_STANDARD.md）：
		//   §3 高风险操作（删除/修改/批量写入）：在 validate 后、execute 前加 human_confirm
		//   §3 execute 的 on_error 必须是 report（而非 done），保证摘要始终可见
		//   §2.4 Go 工具优先：execute 步骤优先 memory_plugin/knowledge 等原生工具
		Template: `# 治理框架引用：
#   - 批量操作风险分类 → docs/ABSORPTION_GOVERNANCE.md §3
#   - 高风险操作（删除/修改/写入）：在 validate 后、execute 前加 human_confirm 步骤
#   - execute 步骤的 on_error 不能是 done，必须是 report（保证摘要始终可见）
output_target: knowledge

steps:
  - id: validate
    plugin: think_plugin
    type: chat
    timeout: 30
    on_error: execute
    inputs:
      mode: "no_retrieval"
      message: >-
        检查以下输入是否合法，确认可以安全执行：

        输入：${input}

        输出 JSON（不要其他文字）：
        {
          "valid": true,
          "reason": "校验结论",
          "items_count": 0,
          "risk_level": "low/medium/high（high 建议加 human_confirm 步骤）"
        }
    next: execute

  - id: execute
    plugin: {{.ExecutePlugin}}
    type: {{.ExecuteType}}
    timeout: {{.ExecuteTimeout}}
    on_error: report
    inputs:
      {{.ExecuteInputKey}}: "${input}"
    next: report

  - id: report
    plugin: think_plugin
    type: chat
    timeout: 60
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        根据执行结果生成简洁的操作摘要：

        校验结果：${steps.validate.output}
        执行结果：${steps.execute.output}

        格式：一行总结（成功X条/失败Y条），再列出失败项（如有）。`,
	},
	{
		ID:           "alert",
		Name:         "告警通知",
		Description:  "检测异常并推送通知，适合：阈值监控、失败告警、定期巡检发现问题时通知",
		Examples:     []string{"知识库超过1000条时通知我", "每天检查 API 是否正常", "有新的安全漏洞时告警"},
		OutputTarget: "notify",
		// ── 骨架设计说明 ──────────────────────────────────────────
		//
		// 步骤结构：
		//   check    → 执行检测，返回原始状态数据
		//   evaluate → LLM 判断是否触发告警，必须说明证据等级
		//   notify   → 有告警时发送通知（skip_if 保证无告警时跳过）
		//
		// V25 告警规则（防止 LLM 推测告警）：
		//   E1（直接证据）/ E2（测试推断）→ 可触发告警
		//   E3（历史推断）→ 仅 low 级告警
		//   E4（推测）    → 禁止触发告警，仅记录日志
		//
		// 告警出口：当前通过 notify_plugin（邮件/Slack/企业微信）。
		// TODO(alert-ui): Web/App 端原生告警面板实装后，
		//   在 notify 步骤后追加 output_target: dashboard 推送。
		Template: `# 治理框架引用：
#   - 告警依据证据等级 → docs/ABSORPTION_GOVERNANCE.md §1
#   - 规则：E1/E2 级可触发告警，E3 仅 low 级，E4 推测禁止触发告警
#   - TODO(alert-ui): Web/App 原生告警面板集成后此注释可删除
output_target: notify

steps:
  - id: check
    plugin: {{.CheckPlugin}}
    type: {{.CheckType}}
    timeout: 30
    on_error: done
    inputs:
      {{.CheckInputKey}}: "${input}"
    next: evaluate

  - id: evaluate
    plugin: think_plugin
    type: chat
    timeout: 60
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >-
        {{.EvaluatePrompt}}

        检测结果：${steps.check.output}

        V25 告警证据规则：
          E1（检测结果直接可见）→ 可触发 high/medium 告警
          E2（通过阈值推断）    → 可触发 medium/low 告警
          E3（历史规律推断）    → 只能触发 low 告警
          E4（纯推测）         → 禁止触发告警，alert 必须设为 false

        输出 JSON（不要其他文字）：
        {
          "alert": false,
          "severity": "high/medium/low",
          "evidence": "E1/E2/E3（说明证据来源）",
          "message": "告警信息（无告警时留空）"
        }
    next: notify

  - id: notify
    plugin: notify_plugin
    type: notify_send
    timeout: 15
    on_error: done
    skip_if: "steps.evaluate.output.alert == 'false'"
    inputs:
      subject: "【告警】{{.AlertTitle}}"
      body: "${steps.evaluate.output.message}"`,
	},
}

// classifyOutputType 第一步：LLM 从用户描述中判断输出类型（6 选 1）。
//
// 使用独立的短提示词，确保分类准确，不受生成逻辑干扰。
// 返回 WorkflowOutputType.ID，如 "report" / "alert" 等。
func (p *SkillFactoryPlugin) classifyOutputType(description string) (string, error) {
	var typeDescs []string
	for i, t := range workflowOutputTypes {
		examples := strings.Join(t.Examples, " / ")
		typeDescs = append(typeDescs, fmt.Sprintf(
			"%d. [%s] %s\n   适用：%s\n   例子：%s",
			i+1, t.ID, t.Name, t.Description, examples,
		))
	}

	prompt := fmt.Sprintf(`根据用户的需求描述，判断他想要哪种类型的工作流输出。

输出类型选项：
%s

用户描述：%s

只输出类型 ID（如 report / stats / alert），不要其他文字。`, strings.Join(typeDescs, "\n\n"), description)

	// 维度：仅内容（ForContent）。
	// 分类输出是单个词，没有结构维度可言；也没有事实可校验。
	// AntiLazy 基线防止 LLM 加"我认为是..."解释。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你是工作流输出类型分类器。只输出一个类型 ID，不要解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.ForContent(),
		20*time.Second,
	)
	llm.RecordUsage("skill_factory_classify", usage)
	if err != nil {
		return "", err
	}

	typeID := strings.TrimSpace(strings.ToLower(content))
	// 校验返回值是合法类型 ID
	for _, t := range workflowOutputTypes {
		if t.ID == typeID {
			return typeID, nil
		}
	}
	// LLM 返回了非法值，降级为 report（最通用）
	fmt.Printf("[技能工场] 分类结果 %q 不在已知类型中，降级为 report\n", typeID)
	return "report", nil
}

// fillTemplate 第二步：LLM 根据输出类型骨架，填充用户场景专属的变量。
//
// 骨架中用 {{.VarName}} 占位的部分，由 LLM 根据用户描述填充。
// 简单变量替换，不引入额外的模板引擎依赖。
func (p *SkillFactoryPlugin) fillTemplate(outputType WorkflowOutputType, description, preferredName string) (string, error) {
	// 提取骨架中所有 {{.VarName}} 占位符，告诉 LLM 需要填哪些
	var placeholders []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(outputType.Template, "{{.") {
		if idx := strings.Index(part, "}}"); idx > 0 {
			name := part[:idx]
			if !seen[name] {
				placeholders = append(placeholders, name)
				seen[name] = true
			}
		}
	}

	nameHint := preferredName
	if nameHint == "" {
		nameHint = "（根据描述自动生成，英文短横线分隔，如 daily-kb-check）"
	}

	prompt := fmt.Sprintf(`根据用户需求，为 %s 类型的工作流骨架填充变量。

用户需求：%s

工作流 id（name 字段）：%s

需要填充的变量（输出 JSON，每个变量对应一个字段）：
%s

填充规则：
- Plugin 类变量：优先选用 memory_plugin / search_plugin / knowledge_plugin（Go 原生工具）；
  terminal_plugin 仅作最后降级，不是数据收集的首选；
  可选范围：（%s）
- Type 类变量：该插件支持的消息类型（参考插件说明）
- Timeout 类变量：整数秒数（搜索30，内存操作10，LLM生成120-180，通知15）
- InputKey 类变量：该步骤输入参数的字段名（如 keyword / query / url）
- Prompt 类变量：该步骤 think_plugin 的提示词（中文，50字以内，说清楚让 LLM 做什么）
- Title/AlertTitle 类变量：标题文字
- Timestamp 类变量：填 "动态时间" 四字

只输出 JSON，不要其他文字。`, outputType.Name, description, nameHint,
		`{"name":"工作流ID","`+strings.Join(placeholders, `":"填充值","`)+`":"填充值"}`,
		p.buildPluginListCompact())

	// 维度：结构 + 内容。
	//   ForStructure("json", "name", 1) → 强制合法 JSON 且必含 name 字段，1 次重试
	//   .WithContent()                  → 叠加 AntiLazy 基线（防止 LLM 加解释）
	// 不启用事实维度 — 这是模板填充任务，没有事实可校验。
	// llmguard 内部的 stripMarkdownFences 自动处理 ```json 包裹，
	// 此处保留 cleanJSON 兼容旧错误模式。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你是工作流变量填充器。只输出 JSON，不要解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.ForStructure("json", "name", 1).WithContent(),
		40*time.Second,
	)
	llm.RecordUsage("skill_factory_fill", usage)
	if err != nil {
		// 即使 llmguard 重试用尽，content 仍是最后一次输出，
		// 尝试解析一次再决定是否真的失败（兜底降级）
		if content == "" {
			return "", fmt.Errorf("变量填充失败: %w", err)
		}
		fmt.Printf("[技能工场] llmguard 校验未过但尝试降级解析: %v\n", err)
	}

	// 解析 LLM 返回的变量 JSON
	content = cleanJSON(content)
	var vars map[string]string
	if err := json.Unmarshal([]byte(content), &vars); err != nil {
		return "", fmt.Errorf("变量解析失败 (raw=%q): %w", content[:min(len(content), 200)], err)
	}

	// 用变量填充骨架
	result := outputType.Template
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{."+k+"}}", v)
	}

	// 组装最终 YAML：加 id 行 + 治理注释头
	workflowName := vars["name"]
	if workflowName == "" {
		workflowName = preferredName
	}
	if workflowName == "" {
		workflowName = "generated-" + outputType.ID
	}

	header := fmt.Sprintf(`# 工作流：%s
# 输出类型：%s — %s
# 生成时间：%s
# 生成方式：skill_factory_plugin（输出类型分类 → 骨架填充）
#
# 修改说明：
#   - 步骤 plugin/type 参考 docs/DIRECTORY.md 中已注册插件列表
#   - on_error 字段：指定失败后跳转步骤，"done" = 安全终止
#   - think_plugin 步骤必须保留 mode: no_retrieval（避免干扰 JSON 输出）

id: %s
`, workflowName, outputType.ID, outputType.Name, time.Now().Format("2006-01-02"), workflowName)

	return header + "\n" + result, nil
}

// generateWorkflow 工作流生成主入口。
//
// 两阶段策略：
//
//	阶段1（推荐）：输出类型分类 → 骨架填充
//	  优点：结构可预期，V25 规范由骨架保证，LLM 只填细节
//	阶段2（降级）：当阶段1失败时，给 LLM 完整规范约束，从零生成
//	  缺点：结构质量依赖 LLM，但仍然强制 V25 规则
func (p *SkillFactoryPlugin) generateWorkflow(description, preferredName string) (string, error) {
	// ── 阶段1：分类 + 骨架填充 ─────────────────────────────
	typeID, err := p.classifyOutputType(description)
	if err == nil {
		var outputType WorkflowOutputType
		for _, t := range workflowOutputTypes {
			if t.ID == typeID {
				outputType = t
				break
			}
		}
		if outputType.ID != "" {
			yaml, err := p.fillTemplate(outputType, description, preferredName)
			if err == nil && yaml != "" {
				fmt.Printf("[技能工场] 阶段1成功：类型=%s，名称=%s\n", typeID, preferredName)
				return yaml, nil
			}
			fmt.Printf("[技能工场] 阶段1骨架填充失败（%v），降级到阶段2\n", err)
		}
	}

	// ── 阶段2：带 V25 约束的全量生成 ─────────────────────────
	// 当骨架填充失败时，仍然用强约束 prompt 保证基本质量。
	fmt.Printf("[技能工场] 阶段2：V25 约束全量生成\n")
	pluginList := p.buildPluginList()
	nameHint := ""
	if preferredName != "" {
		nameHint = "工作流 id 必须是: " + preferredName
	}

	prompt := fmt.Sprintf(`生成一个 beishan-core YAML 工作流。只输出 YAML，不加任何解释或 markdown 代码块。

用户需求：%s
%s

YAML 结构要求：
id: <工作流名>
output_target: chat   # chat/dashboard/notify/knowledge 选一个
steps:
  - id: <步骤ID>
    plugin: <插件名>
    type: <消息类型>
    timeout: <秒数>
    on_error: done      # ← V25强制：每步必须有 on_error
    inputs:
      <参数>: <值，支持 ${input} 和 ${steps.<id>.output} 插值>
    next: <下一步ID>   # 最后一步省略

V25 强制规则（违反则验证失败）：
1. 每个步骤必须有 on_error 字段（值为步骤ID或"done"）
2. plugin=think_plugin 的步骤，inputs 必须包含 mode: "no_retrieval"
3. timeout: 搜索类30，内存操作10，LLM生成120-180，通知15
4. 步骤数 2-8 个，不要过度设计
5. 最后一步不加 next 字段

可用插件：
%s`, description, nameHint, pluginList)

	// 维度：结构（ForStructure "yaml"）+ 内容（WithContent）。
	// 层 2 校验：必须是合法 YAML 且包含顶层字段 id 和 steps。
	// 层 1 基线：AntiLazy 防止 LLM 编造插件名、加 markdown 包裹。
	// MaxRetries=1：YAML 生成偶尔有 markdown 包裹，一次重试即可纠正。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你生成 beishan-core 工作流 YAML。只输出纯 YAML，不加 markdown 代码块或解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.ForStructure("yaml", "id,steps", 1).WithContent(),
		90*time.Second,
	)
	llm.RecordUsage("skill_factory_generate", usage)
	if err != nil {
		return "", fmt.Errorf("生成失败: %w", err)
	}

	return cleanYAML(content), nil
}

// cleanJSON 去除 LLM 返回内容中可能包裹的 markdown 代码块标记。
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// cleanYAML 去除 LLM 返回内容中可能包裹的 markdown 代码块标记。
func cleanYAML(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```yaml")
	s = strings.TrimPrefix(s, "```yml")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// buildPluginListCompact 返回紧凑格式的插件列表（只有名称和类型），用于填充 prompt。
func (p *SkillFactoryPlugin) buildPluginListCompact() string {
	metas := p.kernel.KnownPluginsMeta()
	var names []string
	for name := range metas {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		if name == "scheduler_plugin" || name == "workflow_plugin" || name == "skill_factory_plugin" {
			continue
		}
		m := metas[name]
		if len(m.Types) > 0 {
			parts = append(parts, fmt.Sprintf("%s(%s)", name, strings.Join(m.Types[:min(len(m.Types), 3)], "|")))
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}
