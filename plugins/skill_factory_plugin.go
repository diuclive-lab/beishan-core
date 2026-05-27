package plugins

// PRIVILEGED PLUGIN: skill_factory manages YAML workflow files directly.
// These filesystem operations are inherent to its function as a workflow editor.
// See docs/reports/boundary_debt_register.md#D03

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/llmguard"
	"beishan/internal/tools"
	"beishan/kernel"
	"gopkg.in/yaml.v3"
)

var ErrSkillExists = errors.New("skill already exists")

/* SkillFactoryPlugin （技能工场插件）

   根据自然语言描述，用 DeepSeek 生成标准 YAML 工作流并保存到 workflows/。
   让用户不需要手写 YAML，说一句"把这个变成一个技能"就能生成可复用的工作流。

   消息类型：
   - skill_create:  根据描述生成 YAML 工作流并保存
   - skill_preview: 根据描述生成 YAML，返回预览，不写入磁盘
   - skill_list:    列出所有已有 skill
   - skill_view:    查看某个 skill 的 YAML 内容
   - skill_delete:  删除一个 skill
*/
type SkillFactoryPlugin struct {
	kernel    *kernel.Kernel
	workflows string // workflows/ 目录的绝对路径
}

func NewSkillFactory(k *kernel.Kernel, workflowsDir string) *SkillFactoryPlugin {
	return &SkillFactoryPlugin{
		kernel:    k,
		workflows: workflowsDir,
	}
}

func (p *SkillFactoryPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "skill_evaluate":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[技能评估] %s\n", result.Output[:min(len(result.Output), 200)])
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	case "skill_create":
		return p.handleCreate(msg)
	case "skill_list":
		return p.handleList()
	case "skill_view":
		return p.handleView(msg)
		case "skill_delete":
			return p.handleDelete(msg)
		case "skill_preview":
			return p.handlePreview(msg)
	default:
		return kernel.Message{}, fmt.Errorf("skill_factory: 未知类型 %s", msg.Type)
	}
}

// ─── skill_create ─────────────────────────────────────────

type createRequest struct {
	Description string `json:"description"`
	Name        string `json:"name,omitempty"`  // 可选，不提供则由 DeepSeek 生成
	Force       bool   `json:"force,omitempty"` // 同名文件存在时是否覆盖
	Preview     bool   `json:"preview,omitempty"` // true=仅预览不写入
}

func (p *SkillFactoryPlugin) handleCreate(msg kernel.Message) (kernel.Message, error) {
	var req createRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 参数解析失败: %w", err)
	}
	if req.Description == "" {
		return kernel.Message{}, fmt.Errorf("skill_factory: 需要 description 参数")
	}

	// 1. 用 DeepSeek 生成 YAML
	yamlContent, err := p.generateWorkflow(req.Description, req.Name)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 生成失败: %w", err)
	}

	// 2. 硬化层验证：YAML 能解析为合法的 WorkflowDef
		name, err := p.validateAndSave(yamlContent, req.Force)
		if errors.Is(err, ErrSkillExists) {
			payload, _ := json.Marshal(map[string]string{
				"name":   name,
				"status": "exists",
				"note":   fmt.Sprintf("工作流 %s.yaml 已存在，如需覆盖请设置 force:true", name),
			})
			return kernel.Message{Type: "skill.result", Payload: payload}, nil
		}
		if err != nil {
			return kernel.Message{}, fmt.Errorf("skill_factory: 验证失败: %w", err)
		}

		payload, _ := json.Marshal(map[string]string{
			"name":   name,
			"status": "created",
			"note":   fmt.Sprintf("工作流 %s.yaml 已创建，可通过 workflow_plugin 执行", name),
		})
		return kernel.Message{Type: "skill.result", Payload: payload}, nil
	}

func (p *SkillFactoryPlugin) handlePreview(msg kernel.Message) (kernel.Message, error) {
	var req createRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 参数解析失败: %w", err)
	}
	if req.Description == "" {
		return kernel.Message{}, fmt.Errorf("skill_factory: 需要 description 参数")
	}

	yamlContent, err := p.generateWorkflow(req.Description, req.Name)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 生成失败: %w", err)
	}

	// 只验证不保存
	if _, err := p.validateOnly(yamlContent); err != nil {
		payload, _ := json.Marshal(map[string]string{
			"status": "preview_invalid",
			"note":   fmt.Sprintf("YAML 验证未通过: %s", err),
			"yaml":   yamlContent,
		})
		return kernel.Message{Type: "skill.preview", Payload: payload}, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"status": "preview_ok",
		"yaml":   yamlContent,
		"note":   "预览通过四层验证，使用 skill_create 确认创建",
	})
	return kernel.Message{Type: "skill.preview", Payload: payload}, nil
}

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

	// 经 llmguard：AntiLazy 基线防止 LLM 加解释或编造分类。
	// 分类输出是单个词，不强制 JSON / 不需要重试（重试也只会再吐一次解释）。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你是工作流输出类型分类器。只输出一个类型 ID，不要解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.Contract{AntiLazy: true},
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

	// 经 llmguard：
	//   - OutputFormat=json + JSONSchema=name → 强制 JSON 且必含 name 字段
	//   - AntiLazy → 防止 LLM 加解释或包 markdown
	//   - MaxRetries=1 → JSON 偶尔会被 markdown 包裹，给一次重试机会
	// llmguard 内部的 stripMarkdownFences 会自动处理 ```json 包裹，
	// 此处保留 cleanJSON 兼容旧错误模式。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你是工作流变量填充器。只输出 JSON，不要解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.Contract{
			OutputFormat: "json",
			JSONSchema:   "name",
			AntiLazy:     true,
			MaxRetries:   1,
		},
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
//   阶段1（推荐）：输出类型分类 → 骨架填充
//     优点：结构可预期，V25 规范由骨架保证，LLM 只填细节
//   阶段2（降级）：当阶段1失败时，给 LLM 完整规范约束，从零生成
//     缺点：结构质量依赖 LLM，但仍然强制 V25 规则
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

	// 经 llmguard：YAML 不是 JSON，OutputFormat 不适用，
	// 但 AntiLazy 基线仍有价值（防止 LLM 加解释或在 YAML 里编造插件名）。
	// 未来若 llmguard 支持 OutputFormat="yaml"，再升级此处的校验强度。
	content, usage, err := llmguard.Chat(
		[]llm.ChatMessage{
			{Role: "system", Content: "你生成 beishan-core 工作流 YAML。只输出纯 YAML，不加 markdown 代码块或解释。"},
			{Role: "user", Content: prompt},
		},
		llmguard.Contract{AntiLazy: true},
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

// ─── 硬化层验证 ─────────────────────────────────────────

// workflowDef 和 stepDef 用于 YAML 验证的最小结构。
// 只解析验证所需字段，不影响引擎的完整解析。
//
// 注意：这里故意不复用 internal/workflow.WorkflowDef，
// 避免插件层对引擎层产生强依赖（两层保持独立演化）。
type workflowDef struct {
	ID    string    `yaml:"id"`
	Steps []stepDef `yaml:"steps"`
}
type stepDef struct {
	ID      string                 `yaml:"id"`
	Plugin  string                 `yaml:"plugin"`
	Type    string                 `yaml:"type"`
	Timeout int                    `yaml:"timeout,omitempty"`
	OnError string                 `yaml:"on_error,omitempty"`
	Inputs  map[string]interface{} `yaml:"inputs,omitempty"`
}

// validateOnly 对生成的 YAML 做四层校验：
//
//	L1 结构检查  — YAML 可解析，有 id，有步骤
//	L2 插件检查  — 每步引用的 plugin 已注册
//	L3 类型检查  — step.type 在插件支持的 types 列表内
//	L4 V25 规范  — on_error、think_plugin mode、timeout 合理性
//
// V25 规范说明（详见 docs/V25_WORKFLOW_STANDARD.md）：
//   - 每步必须有 on_error（human_confirm 例外，它本身就是终止点）
//   - think_plugin 步骤的 inputs 必须有 mode 字段
//   - 每步 timeout 必须 > 0
func (p *SkillFactoryPlugin) validateOnly(yamlContent string) (string, error) {
	// ── L1 结构检查 ──────────────────────────────────────────
	var def workflowDef
	if err := yaml.Unmarshal([]byte(yamlContent), &def); err != nil {
		return "", fmt.Errorf("YAML 解析失败: %w", err)
	}
	if def.ID == "" {
		return "", fmt.Errorf("工作流缺少 id 字段")
	}
	if len(def.Steps) == 0 {
		return "", fmt.Errorf("工作流没有步骤")
	}

	// ── L2 插件检查 ──────────────────────────────────────────
	known := p.kernel.KnownPlugins()
	pluginSet := make(map[string]bool, len(known))
	for _, name := range known {
		pluginSet[name] = true
	}
	// human_confirm 是引擎内置伪插件，不需要注册
	pluginSet["human_confirm"] = true

	metas := p.kernel.KnownPluginsMeta()

	for _, step := range def.Steps {
		if step.ID == "" {
			return "", fmt.Errorf("存在缺少 id 字段的步骤")
		}
		if step.Plugin == "" {
			return "", fmt.Errorf("步骤 %q 缺少 plugin 字段", step.ID)
		}
		if !pluginSet[step.Plugin] {
			return "", fmt.Errorf("步骤 %q 引用了未注册插件 %q（可用: %s）",
				step.ID, step.Plugin, strings.Join(known, ", "))
		}

		// human_confirm 是伪插件，跳过 type/V25 校验
		if step.Plugin == "human_confirm" {
			continue
		}

		if step.Type == "" {
			return "", fmt.Errorf("步骤 %q 缺少 type 字段", step.ID)
		}

		// ── L3 类型检查 ──────────────────────────────────────
		if m, ok := metas[step.Plugin]; ok && len(m.Types) > 0 {
			valid := false
			for _, t := range m.Types {
				if t == step.Type {
					valid = true
					break
				}
			}
			if !valid {
				return "", fmt.Errorf("步骤 %q 的 type %q 不在插件 %q 支持的类型中: %v",
					step.ID, step.Type, step.Plugin, m.Types)
			}
		}

		// ── L4 V25 规范检查 ───────────────────────────────────
		// 规则1：每步必须声明 on_error，防止一步失败炸掉整条链。
		// 如果确实想让失败终止工作流，应该显式写 on_error: done。
		if step.OnError == "" {
			return "", fmt.Errorf("步骤 %q 缺少 on_error 字段（V25强制项，失败终止请填 on_error: done）", step.ID)
		}

		// 规则2：think_plugin 步骤必须有 mode 字段，防止检索管道干扰 JSON 输出。
		// mode 通常为 "no_retrieval"，引擎也会自动注入，但显式声明更可读。
		if step.Plugin == "think_plugin" {
			if _, hasMode := step.Inputs["mode"]; !hasMode {
				return "", fmt.Errorf("步骤 %q（think_plugin）的 inputs 缺少 mode 字段（推荐 no_retrieval）", step.ID)
			}
		}

		// 规则3：timeout 必须 > 0，防止步骤永久阻塞。
		if step.Timeout <= 0 {
			return "", fmt.Errorf("步骤 %q 的 timeout 必须 > 0（搜索类30，LLM类120-180，通知类15）", step.ID)
		}
	}
	return def.ID, nil
}

func (p *SkillFactoryPlugin) validateAndSave(yamlContent string, force bool) (string, error) {
	name, err := p.validateOnly(yamlContent)
	if err != nil {
		return "", err
	}

	// 4. 检查文件名冲突
	path := filepath.Join(p.workflows, name+".yaml")
	if _, err := os.Stat(path); err == nil {
		if !force {
			return name, ErrSkillExists
		}
		fmt.Printf("[skill_factory] 覆盖已有工作流: %s\n", name)
	}

	// 5. 写入文件
	fullContent := fmt.Sprintf("# Generated by skill_factory_plugin\n# %s\n\n%s",
		time.Now().Format("2006-01-02 15:04:05"),
		yamlContent)
	// D03: skill_factory_plugin 是特权插件，允许直接操作 workflows/ 目录文件系统。
	// 参见 docs/reports/boundary_debt_register.md#D03
	if err := os.MkdirAll(p.workflows, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(fullContent), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return name, nil
}

// ─── skill_list / skill_view / skill_delete ───────────────

func (p *SkillFactoryPlugin) handleList() (kernel.Message, error) {
	entries, err := os.ReadDir(p.workflows)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 读取工作流目录失败: %w", err)
	}

	type skillInfo struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var skills []skillInfo

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		skills = append(skills, skillInfo{Name: name, Path: e.Name()})
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	payload, _ := json.Marshal(skills)
	fmt.Printf("[skill_factory] 列出 %d 个 skill\n", len(skills))
	return kernel.Message{Type: "skill.list", Payload: payload}, nil
}

func (p *SkillFactoryPlugin) handleView(msg kernel.Message) (kernel.Message, error) {
	name := strings.Trim(string(msg.Payload), `"`)
	path := filepath.Join(p.workflows, name+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 找不到 skill %s", name)
	}

	payload, _ := json.Marshal(map[string]string{
		"name":    name,
		"content": string(data),
	})
	return kernel.Message{Type: "skill.view", Payload: payload}, nil
}

func (p *SkillFactoryPlugin) handleDelete(msg kernel.Message) (kernel.Message, error) {
	name := strings.Trim(string(msg.Payload), `"`)
	path := filepath.Join(p.workflows, name+".yaml")

	if err := os.Remove(path); err != nil {
		// D03: skill_factory_plugin 是特权插件，允许直接删除 workflows/ 目录文件。
		// 参见 docs/reports/boundary_debt_register.md#D03
		return kernel.Message{}, fmt.Errorf("skill_factory: 删除失败: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"name":   name,
		"status": "deleted",
	})
	fmt.Printf("[skill_factory] 删除 skill: %s\n", name)
	return kernel.Message{Type: "skill.result", Payload: payload}, nil
}

// ─── 工具函数 ──────────────────────────────────────────────

func (p *SkillFactoryPlugin) buildPluginList() string {
	metas := p.kernel.KnownPluginsMeta()

	var names []string
	for name := range metas {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		if name == "scheduler_plugin" || name == "workflow_plugin" || name == "skill_factory_plugin" {
			continue
		}
		m := metas[name]
		sb.WriteString(fmt.Sprintf("- %s: %s", name, m.Description))
		if len(m.Types) > 0 {
			sb.WriteString(fmt.Sprintf(" (types: %s)", strings.Join(m.Types, ", ")))
		}
		sb.WriteString("\n")
	}
	return sb.String()

}
