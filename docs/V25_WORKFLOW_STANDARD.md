# v2.5 YAML 工作流参考标准

> 定义：底座所有 YAML 工作流应该遵循的治理标准。
> 引用源：`docs/ABSORPTION_GOVERNANCE.md`

---

## 1. 强制项（所有 v2.5 YAML 必须包含）

### 1.1 头部治理引用

每个 YAML 文件开头必须引用治理框架中相关的章节：

```yaml
# 治理框架引用：
#   - 证据等级 E1-E4 → docs/ABSORPTION_GOVERNANCE.md §1
#   - 风险分类       → docs/ABSORPTION_GOVERNANCE.md §3
#   - 升级策略       → docs/ABSORPTION_GOVERNANCE.md §4
```

引什么章节取决于 YAML 的类型：
- 涉及 LLM 分析判断的 → 必须引用 §1 证据等级
- 涉及风险/安全评估的 → 必须引用 §3 风险分类
- 涉及多轮/循环操作的 → 必须引用 §4 升级策略

### 1.2 错误处理

所有步骤必须有 `on_error` 兜底，不能默认失败冒泡到顶层：

```yaml
- id: some_step
  plugin: memory_plugin
  type: some_tool
  timeout: 30
  on_error: fallback_step   # 指定降级步骤
  # 或
  on_error: done            # 如果该步骤失败可跳过
```

### 1.3 终止条件

存在循环/重试的 YAML 必须有硬上限：

```yaml
# 逆向审计最多 3 轮，硬上限
next:
  - if: "steps.audit_r3.output.status == 'found'"
    goto: report   # 第 3 轮后无论结果都终止，不再进入第 4 轮
```

---

## 2. 条件项（根据 YAML 类型选择性包含）

### 2.1 LLM 分析步骤

如果 YAML 包含 `think_plugin` / `chat` 类型的步骤，prompt 必须要求输出证据等级：

```
每条发现必须标注 evidence 等级：
  E1 = 源码/对话内容直接证据
  E2 = 测试/行为推断
  E3 = 历史记录/讨论推断
  E4 = 推测（需标注 hypothesis）
```

输出 JSON schema 中对应的字段：

```json
{
  "findings": [
    {
      "description": "问题描述",
      "evidence": "E1",
      "source": "源码行号 / 对话ID / 文件名"
    }
  ]
}
```

### 2.2 风险评估步骤

如果 YAML 涉及风险/安全/兼容性评估，输出必须包含 `risk_register`：

```json
{
  "risk_register": [
    {
      "risk": "风险描述",
      "category": "correctness / compatibility / performance / security / operability / dependency",
      "severity": "low / medium / high / critical",
      "mitigation": "应对措施",
      "status": "open / resolved / accepted"
    }
  ]
}
```

### 2.3 缺口分析步骤

如果 YAML 涉及"分析/评估/审查"类的操作，必须有缺口记录步骤：

```yaml
- id: gap_analysis
  plugin: think_plugin
  type: chat
  timeout: 60
  on_error: next_step
  inputs:
    mode: "no_retrieval"
    message: >
      列出本次分析未覆盖的部分及其原因。
      输出严格 JSON：
      {
        "skipped_areas": [{"area": "未分析的区域", "reason": "为什么跳过"}],
        "partial_failure": false,
        "coverage_pct": 0.0
      }
```

### 2.4 Go 工具优先

数据采集步骤优先使用 L3 Go 工具（`memory_plugin`）而非终端命令（`terminal_plugin`）：

| 场景 | 优先用 | 降级用 |
|------|--------|--------|
| 文件列表扫描 | `code_tree(list_files=true)` | `find ... \| sort` |
| 代码统计 | `code_stats(list_files=true)` | `wc -l \| sort` |
| 结构扫描 | `go_struct_scan(root=)` | `grep ... -E` |
| 批量读文件 | `code_read_external(paths=[])` | `cat` |
| 语言检测 | `code_lang_detect` | `ls go.mod` |

---

## 3. 禁止项

| 禁止 | 原因 |
|------|------|
| 终端命令无降级路径 | memory_plugin 不可用时 workflow 不应完全中断 |
| LLM prompt 无证据等级要求 | 导致不可追溯的模糊结论 |
| 循环无硬上限 | 可能无限重试或 LLM 胡编 |
| step 之间隐式依赖 | 所有依赖必须通过 `next:` 或 `${steps.x.output}` 显式声明 |
| 硬编码底座路径 | 使用 `${input}` 和 `${steps.x.output}` 模板变量 |

---

## 4. 参考模板

最小 v2.5 YAML 骨架：

```yaml
# {名称} v2.5 — {一句话描述}
#
# 治理框架引用：
#   - 证据等级 → docs/ABSORPTION_GOVERNANCE.md §1
#   - 风险分类 → docs/ABSORPTION_GOVERNANCE.md §3

id: {workflow_id}

steps:
  - id: step1
    plugin: memory_plugin       # Go 工具优先
    type: {tool_name}
    timeout: 30
    on_error: step1_fallback    # 必须有降级
    inputs:
      key: "${input}"
    next: step2

  - id: step1_fallback
    plugin: terminal_plugin     # 降级
    type: terminal_exec
    timeout: 30
    on_error: done
    next: step2

  - id: step2
    plugin: think_plugin
    type: chat
    timeout: 120
    on_error: done
    inputs:
      mode: "no_retrieval"
      message: >
        分析任务描述。
        对每条发现标注 evidence 等级（E1/E2/E3/E4）。
        输出严格 JSON。
    next: step3

  - id: step3
    plugin: memory_plugin
    type: knowledge_add
    timeout: 15
    on_error: done
    inputs:
      title: "${steps.step2.output.title}"
      summary: "${steps.step2.output.summary}"
```

---

## 5. 各 Tier YAML 的 v2.5 完成状态

| YAML | Tier | v2.5 状态 | 关键升级 |
|------|------|-----------|----------|
| absorb_right_flower | Core | ✅ | 治理框架 + L2/L3 分级 + 证据等级 |
| code_deep_analyze | Core | ✅ | memory_plugin 替换终端 + evidence + risk_register |
| knowledge_review_scheduler | Core | ✅ | 来源追溯 + KB 查重 + 证据标注 |
| kb_hygiene_plus | Core | ✅ | on_error 兜底 + 治理引用 |
| code_review_9x | Leverage | ✅ | 证据等级 + risk_register + 硬上限 |
| project_analyze | Leverage | ✅ | base_capability_inventory + 证据等级 |
| knowledge_heal | Leverage | ✅ | 结构化报告 + 证据标注 |
| file_ingest | Leverage | ✅ | provenance 追踪 + key_claims |
| clarify_learn | Leverage | ✅ | on_error 兜底 + 治理引用 |
| web_research_auto | Leverage | ✅ | 来源可信度 + 治理引用 |
| agent_observer | Other | ✅ | evidence + on_error |
| batch_ingest | Ingest | ✅ | on_error 兜底 + 治理引用 |
| claude_memory_ingest | Ingest | ✅ | on_error + 治理引用 |
| code_knowledge_ingest | Ingest | ✅ | evidence 标注 + on_error |
| code_project_analyze | Code | ✅ | evidence + 治理引用 |
| codex_conversation_ingest | Ingest | ✅ | evidence + on_error |
| csv_data_pipeline | Data | ✅ | on_error + 治理引用 |
| github_radar | Research | ✅ | on_error 兜底 + 治理引用 |
| insight_followup | Code | ✅ | evidence + source_entry_id + on_error |
| kb_hygiene | Gov | ✅ | on_error 兜底 + 治理引用 |
| knowledge_classify | Gov | ✅ | evidence + on_error |
| knowledge_enrich | Gov | ✅ | on_error + 治理引用 |
| knowledge_enrich_local | Gov | ✅ | on_error + 治理引用 |
| knowledge_review | Gov | ✅ | evidence + on_error |
| knowledge_suggest_links | Gov | ✅ | on_error + 治理引用 |
| legacy_code_audit | Legacy | ✅ | evidence + on_error |
| legacy_doc_generate | Legacy | ✅ | on_error + 治理引用 |
| legacy_module_ingest | Ingest | ✅ | evidence + on_error |
| legal_review | Review | ✅ | on_error 兜底 + 治理引用 |
| monthly_review | Review | ✅ | on_error + 治理引用 |
| multimodal_ingest | Ingest | ✅ | evidence + on_error |
| opensource_project_ingest | Ingest | ✅ | evidence + on_error |
| personal_knowledge_ingest | Ingest | ✅ | on_error + 治理引用 |
| quick_project_scan | Code | ✅ | 治理引用 |
| security_audit_parallel | Security | ✅ | evidence + risk 分类 |
| stock_daily_brief | Other | ✅ | evidence + on_error |
| test_right_flower | Other | ✅ | on_error + 治理引用 |
| weekly_review | Review | ✅ | on_error + 治理引用 |
| writing_assistant | Other | ✅ | on_error + 治理引用 |
