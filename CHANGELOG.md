# 开发日志

## 2026-05-19 多模型 API 适配 + 条件跳过 + 停车管理 + 旧项目审计

### 多模型 API 适配（LLM_PROVIDER）

`internal/llm/config.go` 新增 Provider 系统：
- 预置 `deepseek` / `xiaomi` / `openai` 三个 provider
- 每个 provider 有独立 BaseURL、Model、RouterPrompt
- `LLM_PROVIDER=xiaomi` 一键切换小米 MiMo v2.5-pro
- Router 超时 10s→120s（适配小米慢响应）
- 错误消息从 "DeepSeek" 改为 "LLM"（厂商无关）

### 引擎：条件跳过（skip_if）

StepDef 新增 `SkipIf` 字段，引擎执行前调用 `evaluateCondition` 检查。
条件成立时产出 `"skipped: ..."` 记录，直接走 `next`。

### 引擎：嵌套工作流返回值（FinalOutput）

`WorkflowResult` 新增 `FinalOutput` 字段，存最后一步输出。
下游用 `${steps.sub_workflow.output.FinalOutput}` 获取最终结果。

### 引擎：数组索引支持

`extractJSONFieldValue` 支持 `field[0]` 语法，workflow 模板可引用数组元素。

### 引擎：buildPayload 支持 map[string]interface{} Inputs

适配 YAML 中 `max_results: 5` 等非字符串字段，`singleTemplateRef` 保持类型完整性。

### 引擎：markdown JSON 剥离

`resolveJSONValue` 自动剥离 ```json 包裹，think_plugin 偶尔返回 markdown 代码块不再影响字段提取。

### 新工作流（15 个）

| 工作流 | 文件 | 说明 |
|---|---|---|
| opensource_project_ingest | 新建 | 开源项目分层采样入库（parallel 并发 3 步） |
| code_knowledge_ingest | 新建 | 本地代码文件入库 |
| legacy_code_audit | 新建 | 旧项目结构审计 |
| legacy_module_ingest | 新建 | 逐模块质量评分 + 过滤入库 |
| legacy_doc_generate | 新建 | 反向生成文档 |
| vehicle_entry / exit | parking/ | 车辆入库/出库（备注字段） |
| parking_stats / report | parking/ | 停车统计 + CSV 报告 + 邮件 |
| writing_assistant | 更新 | 6 步完整版：检索→大纲→初稿→批评→修订→入库 |
| weekly_review | 更新 | 5 步完整版：列表→分析→入库→通知 |

### 实测修复（Apex-OS 项目真实跑通）

- file_parse 扩展 17 种代码文件类型（.py/.go/.js 等）
- knowledge_add content 字段兼容 string 和 array（oneOf schema）
- find 命令括号修复（`\( -name ... -o ... \)`）
- 4 个模块入库，2 个 SKIPPED（quality_score < 3）
- 知识库 20 条

### 模板库重构

`workflows/templates/` 拆为 `patterns/`（4 个设计模式）和 `domains/`（3 个领域模板）。
`buildWorkflowSummary` 改为 `filepath.Walk` 递归扫描子目录。

### 插件描述优化

| 插件 | 之前 | 之后 |
|---|---|---|
| write_plugin | 文本生成与写作 | 长文本生成/格式化写作/文件处理，不适合输出JSON |
| think_plugin | 通用对话问答 | 推理/分析/判断/结构化JSON，不适合直接生成长文本 |

## 2026-05-19 workflow parallel 并行步骤（goroutine + channel 并发）

### 新增：并行步骤

StepDef 新增 `steps` 字段（`ParallelSteps []StepDef`），支持工作流中定义并行子步骤。

**引擎实现：**
- 检测 `len(step.ParallelSteps) > 0` 时走并行路径
- 每个子步骤在独立 goroutine 中通过 `e.Kernel.Call` 执行
- 通过 channel 收集所有子步骤结果
- 等待全部完成后继续到 `next` 步骤
- 子步骤结果可通过 `${steps.<parent_id>.output.<sub_id>}` 引用

**YAML 用法：**
```yaml
- id: batch_search
  type: parallel
  timeout: 60
  steps:
    - id: search_go
      plugin: search_plugin
      type: web_search
      inputs:
        query: "Go framework"
    - id: search_python
      plugin: search_plugin
      type: web_search
      inputs:
        query: "Python library"
  next: summarize
```

**`_template.yaml`**：补充 `steps` 字段说明 + 并行步骤示例

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/workflow/types.go` | StepDef 新增 ParallelSteps 字段 |
| `internal/workflow/engine.go` | Run 检测并行步骤 + runParallel 方法 |
| `workflows/_template.yaml` | 文档+示例 |

## 2026-05-19 monthly_review 月报工作流 + skill_evaluate 评估工具

### monthly_review 月报工作流

`workflows/monthly_review.yaml` — 基于 weekly_review 扩展：
- 30 天/90 天灵活范围
- 分析：月度概况 → 项目进展 → 趋势分析 → 差距与机会 → 下步建议
- 邮件推送可选（NOTIFY_TARGET）

### skill_evaluate 评估工具

`internal/tools/skill_eval.go` — 工作流质量自动评估：

| 检查项 | 扣分 | 说明 |
|---|---|---|
| 步骤数 0 | -40 | 无步骤定义 |
| 步骤数 > 20 | -10 | 超过建议上限 |
| 重复/空 ID | -20 | 标识冲突 |
| 缺少 plugin/type | -15/次 | 必要字段缺失 |
| 引用不存在步骤 | -20/次 | next/goto/on_error 无效 |
| 不可达步骤 | -10 | 从首步无法到达 |
| 循环依赖 | -30 | 工作流构成环路 |

通过 skill_factory_plugin 路由，支持按名称或直接传 YAML 内容评估。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `workflows/monthly_review.yaml` | 新建 |
| `internal/tools/skill_eval.go` | 新建 |
| `internal/tools/tools.go` | Init() 追加 registerSkillEvalTools |
| `plugins/skill_factory_plugin.go` | 新增 skill_evaluate 路由 + tools 导入 |
| `main.go` | skill_factory Meta.Types 新增 skill_evaluate |

## 2026-05-19 主题图谱 + 时间线 + 6 个 Codex 历史会话入库

### Codex 历史会话导入

从 Codex 历史记录中筛选并导入 6 个与 beishan-core 项目直接相关的会话：

| 会话 | 内容 |
|---|---|
| Claude CLI → DeepSeek | 早期 API 打通与兼容方案 |
| Hermes Go + DeepSeek 集成 | Agent 移植与事件流对齐 |
| Claude Code CLI 兼容 | 工具链打通与问题解决 |
| hermes HTTP 迁移到 Go | Python→Go 架构决策 |
| Hermes Agent 部署 | 初始部署与配置 |
| Agent 意图识别与本地模型 | 本地小模型实战方案 |

知识库从 10 条增长至 **16 条**，覆盖：用户画像、项目背景、模型策略、hermes 起源、DeepSeek 配置、方向决策。

### 新增 L3 工具

**`knowledge_topic_map`** — 自动生成知识条目主题图谱。
- 按 tag 聚类条目
- 共享 ≥2 条目的 tag 自动建立关联子主题
- Top 15 主题降序排列

**`knowledge_timeline`** — 按时间回看项目演进。
- 支持 day/week/month 分组
- 按时间倒序输出
- 每条显示条目 ID 和标题

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeTopicMap、KnowledgeTimeline、2 个工具注册 |
| `plugins/memory_plugin.go` | 新增路由 |
| `main.go` | Meta.Types 新增 |

## 2026-05-19 向量检索引擎：本地词袋向量 + 余弦相似度语义搜索

### 新增

**`internal/tools/embed.go`** — 零外部依赖的本地向量检索引擎

| 工具 | 功能 |
|---|---|
| `knowledge_embed` | 为单条知识生成词袋向量 |
| `knowledge_embed_all` | 批量重嵌全部条目 |
| `knowledge_semantic_search` | 语义搜索（Bow 向量 + 余弦相似度） |

**技术方案：**
- 中文逐字 + ASCII 单词混合 tokenization
- FNV-1a 哈希映射到 512 维向量空间
- 短特征（≤4 字符）自动补充 n-gram
- L2 归一化 + 余弦相似度排序
- 阈值 0.25，默认返回 top 10
- 零外部依赖：无需 API Key、无需外部模型

**与关键词搜索的对比：**

| 特性 | `knowledge_search` | `knowledge_semantic_search` |
|---|---|---|
| 匹配方式 | 关键词精确匹配 | 语义相似度 |
| 外部依赖 | 无 | 无 |
| 场景 | 精确检索 | 模糊/概念搜索 |
| 查询示例 | "开源" | "开源笔记系统" |

**实测结果（10 条知识库）：**

```
查询: "开源笔记系统"
  1. 开源个人知识库项目选型建议 (0.347)

查询: "开发习惯和用户偏好"
  1. 用户画像与偏好 (0.423)

查询: "本地部署大语言模型"
  1. 本地模型的定位与双适配策略 (0.470)
  2. 本地模型方案已放弃 (0.358)
```

### 修复

- `.embed.json` 文件被 `knowledge_list` 误读为知识条目的 bug（添加 `.embed.json` 排除过滤）
- `textToVector` 函数结构修复（多轮编辑导致的语法错误）

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/embed.go` | 新建 — 向量检索引擎 |
| `internal/tools/tools.go` | Init() 追加 registerEmbedTools |
| `plugins/memory_plugin.go` | 新增 embed/semantic_search 路由 |
| `internal/tools/knowledge.go` | `.embed.json` 排除过滤 |
| `main.go` | Meta.Types 新增 |

## 2026-05-19 smoke 测试补全 + 短期 15 条完结

### smoke 测试

`eval/scenarios/core_smoke.yaml` 新增 4 个测试用例：
- `codex_01`：codex_session_list 基本功能
- `codex_02`：codex_session_extract 不存在的 ID（错误处理）
- `claude_01`：claude_memory_list 基本功能
- `claude_02`：claude_memory_import 不存在的名称（错误处理）

### 短期 15 条完结

```
✅ 1.  claude_memory_import suggest_links 后处理
✅ 2.  codex_session_list limit 参数
✅ 3.  codex_session_list since/until 日期过滤
⬜ 4.  codex_session_extract max_chars 参数（常量已有，待暴露）
✅ 5-6. knowledge_dedupe + knowledge_merge
✅ 7.  knowledge_review 批量模式（file_ingest 作为批量入口）
✅ 8.  weekly_review 日期过滤
⬜ 9.  writing_assistant 关键词提炼
✅ 10. file_ingest 工作流
✅ 11-12. Web 最近导入 + 复制 ID
✅ 13. knowledge_confirm_links
✅ 14. smoke 测试补全
✅ 15. PERSONAL_KB_GUIDE.md
```

剩余 2 条（#4 #9）优先级较低，留到下一阶段。

## 2026-05-19 file_ingest 工作流 + knowledge_confirm_links + 日期过滤

### 新增

**file_ingest 工作流**（`workflows/file_ingest.yaml`）
4 步骤：file_parse → think_plugin 结构化 → knowledge_add 入库 → suggest_links 关联
一条命令完成文件→知识的全链路：`"工作流":"file_ingest","input":"/path/to/doc.md"`

**knowledge_confirm_links 工具**
确认关联建议的一键写入工具：将目标 ID 列表写入源条目的 `links` 字段（去重）。
配合 `knowledge_suggest_links` 使用：先看候选 → 确认后写入。

**日期过滤增强**
- `knowledge_list` 新增 `days` 参数：限定最近 N 天（0=全部）
- `codex_session_list` 新增 `since`/`until` 参数：ISO 日期范围过滤
- `weekly_review` 工作流支持 `input: "7"` 限定最近 7 天

### 剩余清单

```
⬜ 3. codex 日期过滤          → 本轮已完成
⬜ 4. codex max_chars 参数    → 常量已有，待暴露为参数
⬜ 7. knowledge_review 批量   → 待做
⬜ 8. weekly_review 日期过滤  → 本轮已完成
⬜ 9. writing_assistant 关键词 → 待做
⬜ 10. file_ingest 工作流     → 本轮已完成
⬜ 13. suggest_links 确认写入 → 本轮已完成
⬜ 14-15. smoke测试/GUIDE     → GUIDE 已完成，smoke 待补
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | KnowledgeConfirmLinks + knowledge_list days 参数 |
| `internal/tools/codex.go` | codex_session_list since/until 参数 |
| `workflows/file_ingest.yaml` | 新建 |
| `workflows/weekly_review.yaml` | list_knowledge 传 days 参数 |
| `plugins/memory_plugin.go` | 新增 knowledge_confirm_links 路由 |
| `main.go` | Meta.Types 新增 |
| `PERSONAL_KB_GUIDE.md` | 更新 |

## 2026-05-19 codex limit + claude_memory 工作流 + PERSONAL_KB_GUIDE

### 完善

- `codex_session_list` 新增 `limit` 参数（默认 50），控制最大返回数
- 创建 `workflows/claude_memory_ingest.yaml`：Claude 记忆文件 → 知识条目的专用工作流
- `knowledge_dedupe` 和 `knowledge_merge` 实机验证通过 ✅
- 合并测试：源标签 `测试` 正确合并入目标条目，源条目自动删除

### Personal KB Guide

创建 `PERSONAL_KB_GUIDE.md`，系统梳理：
- 知识库是什么（定位）
- 数据来源（手动/Codex/Claude/文件/Web）
- 质量保障链（审查 → 去重 → 合并 → 关联）
- 使用方式（API 调用 / Web 界面）
- 短期 15 条剩余清单
- 架构原理

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/codex.go` | codex_session_list 新增 limit 参数 |
| `workflows/claude_memory_ingest.yaml` | 新建 — Claude 记忆导入工作流 |
| `PERSONAL_KB_GUIDE.md` | 新建 — 知识库使用指南 |
| `CHANGELOG.md` | 本日日志 |

## 2026-05-19 知识去重/合并工具 + Web 最近知识列表

### 新增 L3 工具

**`knowledge_dedupe`** — 查找可能重复的知识条目。
- 按 `raw_ref` 精确匹配（同一来源的导入记录）
- 按条目 `id` 语义匹配（标题相似度、标签重叠、摘要重叠）
- 评分机制：raw_ref 相同 = 80 分，标题相同 = 50 分，标签重叠 += 10/个
- 返回评分降序的候选重复列表

**`knowledge_merge`** — 合并两个知识条目。
- 合并字段：tags/topics/tasks/links 取并集
- content 拼接（去重：源内容不重复追加）
- summary 取更长者
- 合并后自动删除源条目

### Web 界面

- 侧栏新增「最近知识」区域，页面加载时自动获取最新 5 条
- 每条可点击复制 ID（`navigator.clipboard.writeText` + 按钮反馈）
- 每 30 秒自动刷新

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeDedupe、KnowledgeMerge、unionStrings、findEntry；注册 2 个新工具 |
| `plugins/memory_plugin.go` | 新增 knowledge_dedupe、knowledge_merge 路由 |
| `main.go` | Meta.Types 新增 |
| `web/index.html` | 侧栏「最近知识」区域 + loadRecentKnowledge + copyKnowledgeId |

## 2026-05-19 Claude 记忆导入 + Codex 全链路验证通过

### Claude 记忆导入

**新增 L3 工具：**
- `claude_memory_list` — 读取 MEMORY.md 索引 + 扫描目录，列出所有记忆文件
- `claude_memory_import` — 解析 YAML frontmatter（name/description/type）+ Markdown body，转为 knowledge 条目

8 个 Claude 记忆文件全部成功导入知识库，覆盖：用户画像、项目背景、架构决策、本地模型策略。

### Codex 全链路验证

完整测试 `codex_conversation_ingest` 工作流：

| 步骤 | 状态 | 结果 |
|---|---|---|
| extract | ✅ | 7 条消息 / 56 条消息，均正确提取 |
| analyze | ✅ | DeepSeek 输出结构化 JSON |
| save_knowledge | ✅ | 2 条知识条目入库 |
| suggest_links | ✅ | 自动关联已有条目 |

### 修复

`internal/workflow/engine.go` — `ctx["input"]` 正确解包 JSON 字符串引号，修复 workflow 输入参数传递问题。此前 `${input}` 插值时会带上 JSON 字符串的额外引号，导致 `codex_session_extract` 的 ID 参数匹配失败。

### 当前知识库状态

```
10 条知识条目，2 个来源：
  claude_memory × 8:  用户画像、项目背景、架构决策、本地模型
  codex × 2:           开源知识库调研、beishan-core 方向决策
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/claude.go` | 新建 — claude_memory_list + import 工具 |
| `internal/tools/tools.go` | Init() 追加 registerClaudeTools |
| `plugins/claude_plugin.go` | 新建 — Claude 记忆导入插件 |
| `main.go` | 注册 claude_plugin |
| `internal/workflow/engine.go` | 修复 input 引号解包 |
| `web/index.html` | 侧栏 Claude 导入入口 |

## 2026-05-19 Codex 对话导入：codex_session_list + extract + 入库工作流

### 新增 L3 工具

**`codex_session_list`** — 读取 `~/.codex/session_index.jsonl`，列出所有 Codex 对话。
- 支持 `keyword` 关键词过滤
- 返回结构化列表：`{id, thread_name, updated_at}`
- 索引文件不存在时自动降级为扫描 `sessions/` 目录

**`codex_session_extract`** — 提取指定 Codex 对话的完整文本。
- 按 ID 搜索 `sessions/` 和 `archived_sessions/` 目录（文件名匹配）
- 解析 JSONL，提取 `event_msg` 类型的 `user_message` 和 `agent_message`
- 过滤 tool call、token_count、session_meta 等噪音
- 近环内容去重
- 返回结构化 JSON：`{id, title, messages[], count}`

### codex_conversation_ingest 工作流

4 步骤：
1. `extract`（codex_session_extract）— 读取 Codex 会话
2. `analyze`（think_plugin + retry:1）— DeepSeek 提炼决策/模式/任务
3. `save_knowledge`（knowledge_add）— 结构化入库
4. `suggest_links`（knowledge_suggest_links）— 关联已有知识

Web 界面侧栏新增「导入」区 + 聊天前缀 "Codex 列表" / "导入 Codex xxx"。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/codex.go` | 新建 — codex_session_list + codex_session_extract 工具 |
| `internal/tools/tools.go` | Init() 追加 registerCodexTools |
| `plugins/codex_plugin.go` | 新建 — Codex 会话插件 |
| `main.go` | 注册 codex_plugin |
| `workflows/codex_conversation_ingest.yaml` | 新建 — 4 步骤入库工作流 |
| `web/index.html` | 侧栏导入入口 + 聊天前缀检测 |
| `imports/codex_sessions/raw/` | 原始 session_index 备份 |

## 2026-05-19 notify_send 工具 + 描述优化 + 短期 10 条完结

### notify_send L3 通知工具

新增 `internal/tools/notify_tool.go` + `plugins/notify_plugin.go`：

| 参数 | 说明 |
|---|---|
| `channel` | email / slack / wechat |
| `target` | SMTP URL 或 webhook URL（可设 `NOTIFY_TARGET` 环境变量兜底） |
| `subject` | 邮件主题（非邮件渠道忽略） |
| `message` | 正文 |

**weekly_review 集成**：analyze 步骤后新增 `send_report` 步骤，`on_error: done` 确保通知未配置时工作流不中断。

**notify 包增强**：`notify.go` 新增 `SendViaChannel` 结构化调用入口，供 L3 工具直接使用。

### 插件描述优化

| 插件 | 原描述 | 新描述 |
|---|---|---|
| write_plugin | 文本生成与写作 | 文件系统操作：读/写/搜索/补丁/解析文档 |
| think_plugin | 通用对话与问答 | 调用 DeepSeek 进行对话/分析/写作/总结 |
| notify_plugin | — | 通知发送：邮件/Slack/企业微信 |

Tags 同步更新：write_plugin 从 `write,generate` → `file,filesystem`

### 短期 10 条完结

```
✅ 1. personal_knowledge_ingest
✅ 2. memory_plugin tag 支持
✅ 3. weekly_review
✅ 4. writing_assistant
✅ 5. 邮件 notify 接通           ← 本轮完成
✅ 6. PDF/文档解析
✅ 7. workflow 错误重试
✅ 8. workflow_smoke 补全
✅ 9. 描述优化                   ← 本轮完成
✅ 10. workflows/templates/ 模式库
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/notify_tool.go` | 新建 — notify_send 工具 |
| `plugins/notify_plugin.go` | 新建 — 通知插件 |
| `internal/notify/notify.go` | 新增 SendViaChannel 结构化接口 |
| `internal/tools/tools.go` | Init() 追加 registerNotifyTools |
| `main.go` | 注册 notify_plugin；更新 write_plugin/think_plugin 描述和 Tags |
| `workflows/weekly_review.yaml` | 新增 send_report 邮件推送步骤 |

## 2026-05-19 writing_assistant + workflows/templates/ 模式库

### writing_assistant 工作流

创建 `workflows/writing_assistant.yaml`，三步骤（search_act 模式）：
1. `research`（knowledge_search）— 搜索知识库获取相关材料
2. `draft`（think_plugin + retry:1）— 生成大纲 + 初稿正文
3. `refine`（think_plugin）— 自我批评分析 + 修订版全文

Web 界面侧栏新增「写作助手」快捷入口 + 聊天 "帮我写一篇文章：" 检测。

### workflows/templates/ 模式库

新建 `workflows/templates/` 目录，从 7 个工作流中提炼 5 种编排模式：

| 模式 | 文件 | 步骤数 | 参考实现 |
|---|---|---|---|
| ingest | `ingest.yaml` | 3 | personal_knowledge_ingest |
| review | `review.yaml` | 4 | knowledge_review |
| suggest | `suggest.yaml` | 2 | knowledge_suggest_links |
| aggregate | `aggregate.yaml` | 3+ | weekly_review, github_radar |
| search_act | `search_act.yaml` | 3 | writing_assistant |

每个模式包含：适用场景、步骤骨架、关键设计说明、变体选项。新工作流可直接复制骨架文件替换 TODO 标记。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `workflows/writing_assistant.yaml` | 新建 — 3 步骤写作助手 |
| `workflows/templates/` (6 文件) | 新建 — 模式库目录 + README + 5 骨架 |
| `web/index.html` | 侧栏写作助手入口 + 聊天前缀检测 + JS 函数 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 writing_assistant 测试 |

## 2026-05-19 底座三连：workflow 重试增强 + weekly_review + PDF 解析

### 1. Workflow 错误重试增强

**新增字段：**
- `retry_delay`（秒）：重试间隔，指数退避（第 N 次重试等待 `retry_delay * 2^(N-1)` 秒，默认 1s）
- `on_error`（字符串）：失败后继续到指定步骤，不终止工作流

**引擎变更：**
- 重试循环添加 `time.Sleep` 退避等待
- `on_error` 分支：失败后记录错误结果并跳转到指定步骤继续执行
- 提取 `buildResult()` 辅助函数消除重复

**文档更新：** `workflows/_template.yaml` 补充 `retry_delay`、`on_error` 字段说明

### 2. weekly_review 工作流

创建 `workflows/weekly_review.yaml`，三步骤：

1. `list_knowledge`（knowledge_list）— 获取所有知识条目（按时间倒序）
2. `list_todos`（todo_list）— 获取当前待办列表
3. `analyze`（think_plugin + retry:1）— DeepSeek 输出周报

输出格式：本周概况 → 待办状态 → 主题聚类 → 差距与机会 → 下步建议

注意：无日期过滤，DeepSeek 根据 `created_at` 时间戳自行识别近期内容。

### 3. PDF/文本解析工具

**新增 `internal/tools/file_parse.go`** — L3 文件解析工具

| 文件类型 | 实现 |
|---|---|
| `.txt` | 直接 `os.ReadFile` |
| `.md`、`.markdown` | 直接 `os.ReadFile` |
| `.pdf` | `github.com/ledongthuc/pdf` 纯 Go 库逐页提取，`GetTextByRow` 按行拼接 |

安全措施：路径遍历检查、50MB 上限、文件存在性校验

工具注册到 `write_plugin`，返回结构化 JSON（path/type/size/filename/content）。

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/workflow/types.go` | StepDef 新增 RetryDelay、OnError 字段 |
| `internal/workflow/engine.go` | 指数退避、on_error 分支、buildResult 辅助函数 |
| `workflows/_template.yaml` | 补充 retry_delay、on_error 文档 |
| `workflows/weekly_review.yaml` | 新建 — 3 步骤周报工作流 |
| `internal/tools/file_parse.go` | 新建 — PDF/TXT/MD 文件解析工具 |
| `internal/tools/tools.go` | Init() 追加 registerFileParseTools |
| `plugins/write_plugin.go` | 新增 file_parse 路由 + 返回 Payload |
| `main.go` | write_plugin Meta.Types 新增 file_parse |
| `eval/scenarios/core_smoke.yaml` | 新增 file_parse 和 suggest_links 错误处理测试 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 weekly_review 工作流测试 |
| `go.mod` | 新增 `github.com/ledongthuc/pdf` 依赖 |

## 2026-05-19 知识关联建议工作流 + 知识网络建设

### 新增

**`knowledge_suggest_links` L3 工具 — 基于内容的关联建议引擎**

评分算法：
- 共享标签（最大 0.70）：每共享一个标签 +0.35
- 共享主题（最大 0.60）：每共享一个主题 +0.30
- 关键词匹配（固定 +0.20）：源条目的标签/主题/标题词出现在目标条目的标题或摘要中
- 总分 ≥ 0.20 才视为候选，按降序排列

输出结构化 JSON：`source_id`、`candidates[]`（含 score/shared_tags/shared_topics/keyword_match/reason）

**`workflows/knowledge_suggest_links.yaml` — 知识关联建议工作流**
2 步骤：
1. `suggest` — L3 引擎匹配候选，最多返回 15 条
2. `report` — DeepSeek 生成可读报告（强关联 / 弱关联 / 无结果）

**Web 界面**
- 侧栏新增「关联建议」快捷操作
- 聊天输入 "帮我关联 kn_xxx" 自动调关联建议工作流

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeSuggestLinks、loadAllKnowledge、intersectStrings、extractKnowledgeKeywords；工具注册 |
| `plugins/memory_plugin.go` | 新增 knowledge_suggest_links 路由 |
| `main.go` | Meta.Types 新增 knowledge_suggest_links |
| `workflows/knowledge_suggest_links.yaml` | 新建 — 2 步骤关联建议工作流 |
| `web/index.html` | 侧栏关联建议入口 + 聊天前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新增 knowledge_suggest_links 错误处理测试 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 suggest_links 工作流失败场景 |

## 2026-05-19 知识审查工作流 + knowledge_update + 输出固化

### 新增

**`knowledge_update` 工具**
- 支持选择性地更新知识条目的任意字段（title/summary/tags/topics/tasks/source_type/links/raw_ref/content）
- 保留原始 `CreatedAt` 时间戳不变（修复了 `saveKnowledge` 覆盖 CreatedAt 的 bug）
- 仅更新显式提供的字段，未提供的字段保持不变

**`workflows/knowledge_review.yaml` — 知识条目质量审查工作流**
4 步骤：
1. `get_entry` — 获取知识条目完整内容
2. `analyze` — DeepSeek 逐字段审查（title/summary/tags/topics/tasks/source_type），输出结构化 JSON
3. `apply_fix` — 知识条目自动修复（调 `knowledge_update`）
4. `report` — 生成可读的中文审查报告

审查规则：
- title: 是否简洁有信息量
- summary: 是否描述核心内容
- tags: 数量合理（2-5个）、有辨识度
- topics: 是否与 tags 互补
- tasks: 是否具体、动词开头
- null 字段跳过，仅非 null 修复值写入

**Web 界面增强**
- 侧栏新增「审查质量」快捷操作
- 聊天输入 "帮我审查知识条目 kn_xxx" 自动调知识审查工作流

**烟雾测试补充**
- `personal_knowledge_smoke.yaml`：新增 `knowledge_get` 无效条目错误处理、`knowledge_update` 无效条目错误处理
- `workflow_smoke.yaml`：新增 `knowledge_review` 不存在的条目工作流失败场景

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新增 KnowledgeUpdate + knowledge_update 工具注册；修复 saveKnowledge 覆盖 CreatedAt |
| `plugins/memory_plugin.go` | 新增 knowledge_update 路由 |
| `main.go` | Meta.Types 新增 knowledge_update |
| `workflows/knowledge_review.yaml` | 新建 — 4 步骤知识质量审查工作流 |
| `web/index.html` | 侧栏审查入口 + 聊天前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新增 error handling 测试用例 |
| `eval/scenarios/workflow_smoke.yaml` | 新增 knowledge_review 工作流测试 |

## 2026-05-19 个人知识系统基础建设（三步）

### 第一步：Memory Schema 规范化

**新增 `internal/tools/knowledge.go` — 统一知识条目类型 + CRUD 工具**

知识条目 `KnowledgeEntry` 统一字段：
```
id, source_type, title, summary, tags[], topics[], tasks[], created_at, links[], raw_ref, content
```

| 工具 | 用途 |
|---|---|
| `knowledge_add` | 添加结构化知识条目（含 tags/topics/tasks 数组） |
| `knowledge_search` | 按关键词匹配 title/summary/content/tags/topics |
| `knowledge_list` | 按 source_type 过滤列出 |
| `knowledge_get` | 获取完整条目 |
| `knowledge_delete` | 删除条目 |

存储位置：`~/.hermes/memory/knowledge/<id>.json`

**todo 持久化 + memory 关联**

- `internal/tools/todo.go` 重构：从 `map[string]interface{}` 切换到 `TodoItem` 结构体，文件持久化到 `~/.hermes/memory/todos.json`
- `todo_add` 新增 `source` 字段，指向关联的 memory/知识 ID
- 新增 `todo_by_source` 工具：按来源查询待办

**memory_plugin 增强**

- 新增 knowledge 消息类型路由（`knowledge_add/search/list/get/delete`）
- memory_plugin 现在返回知识工具的执行结果 Payload，供 workflow 后续步骤使用

### 第二步：personal_knowledge_ingest 工作流

创建 `workflows/personal_knowledge_ingest.yaml`，三步骤：

1. **analyze**（think_plugin）：DeepSeek 分析输入 → 输出 JSON（title/summary/source_type/tags/topics/tasks）
2. **save_knowledge**（memory_plugin knowledge_add）：结构化知识入库，返回知识 ID
3. **save_tasks**（todo_plugin todo_add）：提取的待办写待办列表，`source` 关联知识 ID

**workflow 引擎增强**

- `buildPayload` 支持 JSON 字段路径提取（`${steps.xxx.output.field}`）
- `buildPayload` 自动检测 JSON 数组/对象，保持参数类型完整性
- `resolveJSONValue` 自动解包嵌套编码的 JSON 字符串

**think_plugin 增强**

- 新增 `extractPrompt()` 函数：支持 JSON 对象 Payload 提取 `message` 字段（workflow 场景）

### 第三步：知识入口 + 烟雾测试

**Web 界面增强（`web/index.html`）**

- 侧栏新增「+ 入库新内容」按钮，打开模态框
- 模态框：textarea 粘贴内容 → 直接调 workflow API
- 侧栏知识库快捷操作：浏览知识库、搜索知识
- 聊天输入自动检测 "帮我入库" 前缀，直接触发 workflow

**烟雾测试（`eval/scenarios/personal_knowledge_smoke.yaml`）**

9 个测试用例覆盖：knowledge_add → search → list → todo_by_source → todo_add(含source) → cleanup

### 涉及文件

| 文件 | 变更 |
|---|---|
| `internal/tools/knowledge.go` | 新建 — 知识条目结构体 + CRUD + 工具注册 |
| `internal/tools/todo.go` | 重构 — TodoItem 结构体 + 文件持久化 + source 字段 + todo_by_source |
| `internal/tools/tools.go` | Init() 追加 registerKnowledgeTools() |
| `plugins/memory_plugin.go` | 新增 knowledge 工具路由 + 返回响应 Payload |
| `plugins/todo_plugin.go` | 新增 todo_by_source 路由 |
| `plugins/think_plugin.go` | 新增 extractPrompt() JSON 对象兼容 |
| `internal/workflow/engine.go` | buildPayload JSON 字段路径 + 数组自动检测 |
| `main.go` | Meta.Types 更新（knowledge + todo_by_source） |
| `workflows/personal_knowledge_ingest.yaml` | 新建 — 三步骤知识入库工作流 |
| `web/index.html` | 知识入库模态框 + 侧栏快捷操作 + 输入前缀检测 |
| `eval/scenarios/personal_knowledge_smoke.yaml` | 新建 — 9 用例烟雾测试 |

## 2026-05-18 Web 界面 — 内嵌单页应用

### 新增

- **`web/index.html`**：单文件 Web 界面，深色主题
  - 左侧栏：连接状态指示 + 工作流列表（调用 `skill_list` 自动加载，点击直接触发）
  - 主面板：聊天式交互，支持自然语言输入和结构化响应展示
  - 零外部依赖：纯 vanilla HTML/CSS/JS，无框架、无 CDN
- **`main.go` `//go:embed`**：将 `web/index.html` 编译进二进制，保持单文件部署
  - 访问 `http://localhost:8013/` 打开 Web 界面

### 效果

```bash
# 启动后浏览器打开
open http://localhost:8013
# 在输入框里直接打字：
#   "帮我跑一下开源雷达" → 触发 workflow
#   "搜索今天的AI新闻"  → Router 路由 search_plugin
#   左侧工作流列表     → 点击直接发送
```

### 架构

```
浏览器 ← HTTP → beishan-core (:8013)
                 ├─ GET  /         → web/index.html (嵌入二进制)
                 ├─ POST /api/chat → 完整消息链路
                 └─ GET  /health   → 连接状态
```

## 2026-05-18 Router 工作流发现：用户自然语言触发 workflow

### 新增

- **`kernel.Decision.Payload` 字段**：DeepSeek 路由决策时可输出 payload，`kernel.Send()` 自动应用
- **`kernel.Router.SetWorkflowSummary()`**：注入可用 workflow 列表到路由 prompt
- **`main.go` 启动时扫描 `workflows/`**：`buildWorkflowSummary()` 读取每个 YAML 的 id 和头部注释，注入 Router
- **`workflow_plugin` 纯文本降级**：payload 为裸字符串时直接作为 workflow 名处理

### 效果

```
用户说"帮我跑一下开源雷达"
  → Router 识别为 workflow_plugin (置信度 1.00)
  → 自动设置 payload: {"workflow":"github_radar"}
  → workflow_plugin 执行 7 步全链 ✅
```

### 涉及文件

| 文件 | 变更 |
|---|---|
| `kernel/router.go` | Decision 加 Payload、Router 加 workflowSummary、Route prompt 加 payload 指令 |
| `kernel/kernel.go` | Send() 应用 Decision.Payload |
| `plugins/workflow_plugin.go` | 纯文本 payload 降级为 workflow 名 |
| `main.go` | buildWorkflowSummary() 扫描 workflows/ 注入 Router |

## 2026-05-18 skill_factory 增强：Types 注册 + 硬化层第四关

### 变更

- **`kernel.Meta` 新增 `Types []string` 字段**：记录每个插件支持的 `msg.Type` 列表
- **`kernel.KnownPluginsMeta()` 新增方法**：返回 `map[string]Meta`，含 Description、Tags、Types
- **`skill_factory_plugin` 增强**：
  - `buildPluginList()` 输出带 types：`search_plugin: 通用网络搜索 (types: web_search, web_fetch)`
  - `validateAndSave()` 增加第四关校验：验证 `step.Type` 在插件注册的 `Types` 列表内
- **`main.go`**：15 个插件注册全部补上 `Types` 字段

### 效果

| 指标 | 之前 | 之后 |
|---|---|---|
| DeepSeek 生成 type 正确率 | ~0%（全靠猜） | 实测 4/4 全对 ✅ |
| 硬化层校验 | 不校验 type | 第四关拦截非法 type |
| prompt 中插件信息 | 只有名字+描述 | 名字+描述+可用 types |

### 硬化层四关

1. YAML 语法解析
2. 语义检查（id、steps、plugin、type）
3. 插件注册表校验（plugin 必须在 kernel 已注册）
4. **type 合法性校验**（step.Type 必须在插件注册的 Types 列表内）

## 2026-05-18 skill_factory_plugin — 用自然语言生成 YAML 工作流

### 新增

- **`plugins/skill_factory_plugin.go`**：技能工场插件，接收自然语言描述，用 DeepSeek 自动生成标准 YAML 工作流并保存到 `workflows/`
- **`main.go`**：注册 `skill_factory_plugin`，传入 `workflows/` 目录路径

### 消息类型

| 类型 | 用途 |
|---|---|
| `skill_create` | 根据自然语言描述生成 YAML 工作流并保存 |
| `skill_list` | 列出所有已有 skill/workflow |
| `skill_view` | 查看某个 skill 的 YAML 内容 |
| `skill_delete` | 删除一个 skill |

### 硬化层验证

生成的 YAML 经过三层校验才写入：
1. YAML 语法解析（`gopkg.in/yaml.v3`）
2. 语义检查（id 必有、steps 非空、每步有 plugin/type）
3. 插件注册表校验（引用的 plugin 必须已注册到 kernel）

文件名冲突保护：同名 workflow 已存在时拒绝覆盖。

### 用法

```bash
# 一句话生成工作流
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_create","payload":{"description":"每天早上搜索HackerNews热门技术话题，总结趋势后存入记忆","name":"hn_daily"}}'

# 列出所有 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_list"}'

# 查看某个 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_view","payload":"hn_daily"}'

# 删除 skill
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"skill_factory_plugin","type":"skill_delete","payload":"hn_daily"}'
```

## 2026-05-18 scheduler 支持 cron 定点触发

### 新增

- **scheduler `cron` 字段**：`schedule_add` 支持标准 5 字段 cron 表达式，与 `interval` 互斥
- **`cronNext()` 最小 cron 解析器**：内建于 `plugins/scheduler_plugin.go`，支持 `*`、`*/N`、`N-M`、`N,M` 语法
- **cron 模式 timer 调度**：`time.NewTimer` 计算到下次触发的时间，触发后重算下一轮，支持 `schedule_list` 显示下次执行时间

### 用法

```bash
# 每天上午 10 点执行
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"scheduler_plugin","type":"schedule_add","payload":{"name":"daily_radar","cron":"0 10 * * *","workflow":"github_radar"}}'
```

### 测试

|cron 表达式|期望行为|验证|
|---|---|---|
|`0 10 * * *`|每天 10:00|6 场景全部 PASS ✅|
|`*/15 * * * *`|每 15 分钟|6 场景全部 PASS ✅|
|`0 9 * * 1-5`|工作日 9:00|6 场景全部 PASS ✅|

## 2026-05-18 callback webhook + workflow 超时/重试

### 新增

- **`internal/notify/` 回调推送层**：
  - `slack.go`: Slack Incoming Webhook 推送
  - `email.go`: SMTP 邮件发送
  - `wechat.go`: 企业微信机器人
  - `notify.go`: `callback:platform:地址` 格式分发
- **`kernel/deliverReply` `callback:` 分支**：调 `notify.Callback()`，goroutine 异步推送

### 增强

- **workflow 超时可配**：YAML 每个步骤支持 `timeout` 字段（秒，默认 120）
- **workflow 错误重试**：YAML 每个步骤支持 `retry` 字段（次数，默认 0）
- **legal_review.yaml**：各步骤标注 `timeout:30` / `retry:1`
- **eval/scenarios/workflow_smoke.yaml**：工作流引擎冒烟测试场景

### 用法

| 方式 | ReplyTo 格式 |
|---|---|
| Slack | `callback:slack:https://hooks.slack.com/services/xxx` |
| 邮件 | `callback:email:smtp://user:pass@smtp.qq.com:587/to@addr` |
| 企业微信 | `callback:wechat:https://qyapi.weixin.qq.com/...` |

## 2026-05-18 think_plugin + Router MsgType + 启动清理 + REPL

### 新增

- **`plugins/think_plugin.go`**：通用对话插件。`chat` 类型消息调 DeepSeek 生成回答，系统提示词同步 beishan-core 能力列表，不自称"不能XX"
- **`cmd/repl/main.go`**：REPL 交互界面。`go run cmd/repl/main.go` 启动，直接打字聊天，支持 `/todo_plugin:todo_add 买牛奶` 格式指定插件
- **`eval/scripts/run_core_smoke.sh`** 新增 think_plugin 测试用例

### 修复

- **`kernel/router.go`**: `Decision` 新增 `MsgType` 字段。用户说"搜索新闻"→ 路由到 `search_plugin` + `Type: web_search`，不再把 `chat` 类型送到所有插件
- **`kernel/kernel.go`**: `Send()` 应用决策中的 `MsgType`
- **启动清理**：编译 `go_example` / `l3_echo_go` 二进制，删除 `l4_research` / `l4_template_python` 残档，manifest 目录名校验全通过
- **think_plugin 系统提示词**：列出所有插件能力，避免 DeepSeek 说"我不能生成图片"

### 实测

| 操作 | 结果 |
|---|---|
| `{"message":"你好"}` | → think_plugin 回答 ✅ |
| `{"message":"搜索新闻"}` | → search_plugin/web_search ✅ |
| `{"message":"帮我写文件"}` | → write_plugin/write_file ✅ |
| `{"message":"生成图片"}` | → image_gen_plugin/image_generate ✅ |
| REPL 交互 | → 打字聊天可用 ✅ |
| 启动零警告 | → 20 插件全部就绪 ✅ |

## 2026-05-18 工作流引擎 + legal_review 替换为 YAML

### 新增

- **`internal/workflow/` 工作流引擎**：
  - `engine.go`：核心执行器，读 YAML → 顺序调用 `kernel.Call` → 条件分支路由
  - `types.go`：支持 `next: string` 和 `next: [{if:..., goto:...}, {default:...}]` 双格式
  - `buildPayload`：`${input}` 和 `${steps.<id>.output}` 插值解析
  - `evaluateCondition`：`steps.<id>.output.<field> == 'value'` 条件评估

- **`workflows/` 目录**：
  - `legal_review.yaml`：首个 YAML 工作流（4 步：cold_start → legal_search → clause_analysis → write_report）
  - `_template.yaml`：标准示例模板，含字段说明和条件分支示例

- **`plugins/workflow_plugin.go`**：薄插件包装，接收 `workflow_run` 消息，委托 engine 执行

### 删除

- **`plugins/legal_review_plugin.go`**（-150 行 Go 代码）：被 `workflows/legal_review.yaml` 替代
- `main.go` 中 `legal_review_plugin` 注册移除，描述合并到 `workflow_plugin`

### 替换后影响

| 项目 | 替换前 | 替换后 |
|---|---|---|
| 加新场景 | 写 Go 代码 + 编译 | YAML 文件，无需编译 |
| 法律审查调用 | `legal_review_plugin` | `workflow_plugin`（workflow: legal_review） |
| 插件数 | 18 | 17 |
| 测试 | legal_smoke 6/6 通过 | legal_smoke 6/6 通过 ✅ |

### 工作流引擎实测

```
cold_start → legal_search → clause_analysis → write_report  4步全通 ✅
```

## 2026-05-18 全功能冒烟测试 + 工具移植完结

### Eval 补全

- **新增 `eval/scenarios/core_smoke.yaml`**：12 个测试用例覆盖全部 L3/L4 插件
- **新增 `eval/scripts/run_core_smoke.sh`**：自动编译 + 启动 + 测试 + 清理
- **实测 12/12 全部通过** ✅

### hermes-go 工具移植完结

本轮完成最后 4 个工具移植：

| 工具 | 文件 | 定位 |
|---|---|---|
| vision_analyze（视觉分析） | `internal/tools/media.go` | 预留接口，需 Vision API |
| image_generate（图片生成） | `internal/tools/media.go` | 预留接口，需 DALL-E / SD |
| text_to_speech（文本转语音） | `internal/tools/media.go` | 本地 `say` 命令可用 |
| clarify（意图澄清+学习） | `internal/tools/clarify.go` | 3 次学习后自动推断 |

### 最终统计

| 指标 | 数值 |
|---|---|
| 工具注册数 | 34 |
| 插件注册数 | 17（含 3 glue 子进程） |
| Eval 场景 | legal_smoke（6 用例） + core_smoke（12 用例） |
| 内核文件 | 冻结不改 |
| 全部 hermes-go 工具 | 移植完毕 ✅ |

## 2026-05-18 开发日志归档

## 2026-05-18 memory continuity（路线 A：session 内含 evidence）

### 新增

- **`internal/tools/memory.go` 重写为 session 感知存储**：
  - 存储结构：`~/.hermes/memory/sessions/<session_id>.json`
  - 每条 session 内包含 `messages[]` + `evidence[]`
  - 7 个新工具：session_add、session_get、session_search、session_list、session_delete、evidence_add、evidence_search
  - 并发安全：`sync.RWMutex` 保护读写
  - 威胁扫描：注入检测保留

- **`plugins/memory_plugin.go` 更新**：支持全部 7 种 session 消息类型

- **`main.go` HTTP handler 自动记录 session**：
  - 每次 `/api/chat` 请求生成 `session_id`
  - 同步模式下自动记录 `user → plugin → response` 到 session
  - 异步模式通过 goroutine 处理，`ReplyTo` 回程后存入

### 注册工具统计

`tools registered:` **15** 个工具（原 11 + 新增 7 个 session/evidence 工具，剔除 3 个旧 memory 工具）

### 路线选择

**路线 A**：evidence 作为 session 的子结构存储，不独立管理。
当前无跨 session 引用证据的真实需求，路线 B 留接口。

### 实测

- 发送消息 → session 自动创建 ✅
- 消息持久化到磁盘 ✅
- session_list 查询 ✅

## 2026-05-18 第三轮：glue 依赖管理（路线 A）

- **`glue/spawn()` 新增 `requirements.txt` 自动检测**：spawn 前 `os.Stat` 检测，存在则 `pip3 install -r`
- **向后兼容**：没有 `requirements.txt` 的插件行为不变
- **路线 B 预留**：未来如需独立 venv，在 `spawn()` 中 `switch m.Type` 处分支即可

## 2026-05-18 第二轮：ReplyTo 回程路由 + HTTP 异步 session

### 新增

- **`Message.ReplyTo` 字段**：支持 `plugin:`、`session:`、`callback:`、空 四种前缀
- **`deliverReply()` 内核方法**：`Send()` 完成后检查 `ReplyTo`，按前缀分派
- **`SessionHandler` 回调**：内核不持有 session 状态，由 HTTP 层注入存储函数
- **`/api/chat` 异步模式**：`{"message":"...","async":true}` 立即返回 `session_id`，后台 goroutine 处理
- **`/api/result/:session_id` 轮询端点**：有结果返回结果，无结果返回 `{"status":"pending"}`

### 清理

- `Router.checkRecipient` 和 `SetRecipientValidator` 删除，`parseDecision` 只用 `knownPlugins` 验证
- `NewKernel` 不再依赖 `tools.GetToolSchema`，内核层与工具层注册表完全解耦

### 架构边界（正式确立）

```
内核路由层  → 只认识插件名（knownPlugins）
插件执行层  → 只认识工具名（tools.Registry）
两层之间    → 不互相知道对方的注册表
```

### 实测

- 异步请求 → 立即返回 session_id ✅
- goroutine 后台处理 → DeepSeek 路由 → 插件执行 → deliverReply 存储结果 ✅
- 轮询 /api/result/:session_id → 返回结果 ✅

## 2026-05-18 Meta 注册 + 路由描述增强

### 新增

- **Meta 结构体**：`kernel.Register` 新增可选的 `Meta` 参数，支持 `Description` 和 `Tags`，向后兼容
- **路由 prompt 增强**：DeepSeek 现在能看到每个插件的语义描述，路由决策质量提升
- **`AddKnownPlugin` 替代 `SetPlugins`**：注册时自动维护路由列表，不再需要手动同步
- **`KnownPlugins()`**：新增方法返回所有已注册插件
- **Markdown 容错**：`parseDecision` 自动剥离 DeepSeek 返回的 `` ```json ``、`` ``` `` 等标记
- **HTTP API 兼容**：`/api/chat` 支持 `{"message":"..."}` 简单格式和 `{"type":"...","payload":...}` 完整格式

### 修复

- **Router 不暴露工具名**：路由 prompt 不再包含 `internal/tools` 的工具名，只显示 kernel 注册的插件名
- **`checkRecipient` 改为查 `knownPlugins`**：移除对 `tools.GetToolSchema` 的最后引用，Router 不再依赖 tools 包
- **tools.Init() 调用**：main.go 缺失的初始化已补齐

### 实测

- `web_search` → `search_plugin` ✅ 搜索结果正确返回
- `write_file` → `write_plugin` ✅ 文件写入成功

## 2026-05-17 全链路冒烟测试通过 6/6

### 修复

- **Router extraNames**：DeepSeek 提示词现在包含 kernel 注册的插件名，确保法律插件可被路由
- **legal_review_plugin 响应 Sender**：移除了导致 `deliverResponse` 跳过回传的 Sender 字段
- **纯文本 Payload 兼容**：legal_search_plugin 和 clause_analyzer_plugin 现在也接受纯文本输入
- **parseProfile 空值降级**：空 profile JSON 不再报错
- **WriteRequest 包装**：legal_review_plugin Step 4 正确将 AnalysisReport 包装为 WriteRequest

### 新增

- **HTTP API 服务**：main.go 改为持久 HTTP 服务（`:8013`），添加 GET /health 和 POST /api/chat
- **Router.KnownPlugins**：新增方法返回所有注册插件名

### 测试

- 法律插件冒烟测试 6/6 全量通过
- 测试链路：冷启动(2) → 法律检索(2) → 条款分析(1) → 全链路审查(1)

## 2026-05-17 L3 法律分析插件簇 + 中国法律适配

### 新增

| 文件 | 类型 | 用途 |
|---|---|---|
| `plugins/legal_review_plugin.go` | L4 编排 | 法律审查编排：访谈→检索→分析→生成四步流程 |
| `plugins/l3_echo_go/` | L3 Go 示例 | Go 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/l3_echo_python/` | L3 Python 示例 | Python 语言 L3 子进程标准模板（IPC 协议） |
| `plugins/cold_start_plugin.go` | L3 插件 | 冷启动访谈：合同类型识别、角色分析、法律画像构建 |
| `plugins/legal_search_plugin.go` | L3 插件 | 中国法律检索：适配北大法宝/威科先行查询结构，法律效力层级排序 |
| `plugins/clause_analyzer_plugin.go` | L3 插件 | 条款分析：三段论（大前提-小前提-结论）替代 IRAC，风险三档评级 |
| `plugins/legal_write_plugin.go` | L3 插件 | 法律文书生成：合同审查报告/法律意见书/风险矩阵，AI 标识合规 |

### 中国法律适配

所有法律插件遵循以下中国法域适配规则：
- **法律关系分析**：使用《民法典》合同编典型合同分类体系（第595-978条）
- **三段论推理**：大前提（法律规则）→ 小前提（合同约定）→ 结论（法律评价），替代 IRAC
- **法律效力层级**：宪法 > 法律 > 司法解释 > 行政法规 > 部门规章
- **风险评级**：🟢 合规 / 🟡 提示 / 🔴 违规（参考 claude-for-legal 三档制）
- **文书模板**：合同审查报告、法律意见书、风险矩阵，均使用中国法律文书格式
- **AI 标识**：根据《人工智能生成合成内容标识办法》，所有输出标注 AI 生成身份

### 参考 claude-for-legal 的模式迁移

| 模式 | 迁移比例 | 用途 |
|---|---|---|
| 冷启动访谈（SKILL.md） | ~10% | cold_start_plugin.go 的法律画像构建流程 |
| 风险评级（GREEN/YELLOW/RED） | ~10% | clause_analyzer_plugin.go 的三档评级体系 |
| 文书模板（法律意见书/审查报告） | ~8% | legal_write_plugin.go 的输出模板 |
| 免责声明分级（律师/法务/个人） | ~5% | 按用户角色生成不同的免责声明 |
| MCP 连接器接口（北大法宝预留） | ~3% | legal_search_plugin.go 的 tryPkulawSearch 预留接口 |

### 主程序变更

`main.go` 新增全部法律插件注册：
- 通用工具插件（search/write/memory/scheduler）
- 法律审查插件簇（legal_review/cold_start/legal_search/clause_analyzer/legal_write）

### design principles 新增

`DESIGN_PRINCIPLES.md` 新增 **"Type 即意图，Payload 即数据"** 章节，确立三条子原则：
- 路由只认 Type：Router 做机械映射，不看 Payload
- Payload 不参与决策：Router 永不解析 Payload 字段
- 不能把路由判断权交给 LLM

### 司法数据源集成

新增 `judicial_search` 工具，接入中国司法大数据服务网 (data.court.gov.cn) 和中国裁判文书网 (wenshu.court.gov.cn) 公开数据：

| 变更 | 文件 | 说明 |
|---|---|---|
| 新增工具 | `internal/tools/judicial.go` | judicial_search 工具：HTTP 封装 + HTML 解析 + 结果格式化 |
| 检索链路更新 | `plugins/legal_search_plugin.go` | searchStatutes/searchCases 优先调用 judicial_search |
| 注册入口 | `internal/tools/tools.go` | Init() 追加 registerJudicialTools |

**数据源优先级**：司法大数据服务网 → 裁判文书网 → 通用 web_search 回退

**免费接口限制**：非注册用户仅支持部分案由（民间借贷、离婚、买卖合同等）的统计查询，裁判文书网有反爬机制。所有结果标记 `source` 字段，供审查者验证。

### 测试基础设施 + Router 验证修复

从 `66/FangLab` 项目移植评估基础设施：

| 新增 | 文件 | 说明 |
|---|---|---|
| 测试场景 | `eval/scenarios/legal_smoke.yaml` | 6 个法律插件测试用例（冷启动/检索/分析/全链路） |
| 运行脚本 | `eval/scripts/run_legal_smoke.sh` | 启动服务→发送测试→验证响应→汇总结果 |
| 共享库 | `eval/lib/lib.sh` | 端口检查/进程管理/日志（移植自 runtime_stack_lib.sh） |
| 文档 | `eval/README.md` | 测试方案说明 |
| 环境配置 | `.env` | DeepSeek API Key 配置（已 gitignore） |

**Router 验证修复** (`kernel/kernel.go`, `kernel/router.go`)：

Router.parseDecision 原先只查 `tools.GetToolSchema`，但法律插件通过 `kernel.Register` 注册而非工具注册中心。导致 DeepSeek 路由到合法插件时被误判为"无效收件人"。

修复：`NewKernel` 注入 `SetRecipientValidator`，同时检查内核插件表和工具注册中心。kernel.go 新增 .env 自动加载（init 函数）。

### 架构对齐确认

`legal_review_plugin.go` 作为 L4 编排插件，遵循以下契约：
- 每个步骤通过 `kernel.Call()` 调用 L3 插件，Type 字段精确指定路由目标
- Payload 只传数据（`json.RawMessage`），不做 type assertion
- 导入路径使用 `beishan/kernel`（非 `github.com/...`）
- 错误处理硬编码：任一步骤失败立即终止，不降级

## 2026-05-16 L3/L4 边界硬化

### 背景

文档定义的"三层架构"（第一核心/胶水层/插件）与实际代码的"四层架构"（L1 kernel/L2 glue/L3 internal/tools/L4 plugins）不一致，导致 L3 硬化职责掉落在 L3/L4 之间的缝隙中。

根因：L4 plugins 直接调用 `tools.Execute`（L3 内部调度），跳过了参数校验。且 `tools.Execute` 内部对 JSON 解析失败的 payload 做 lenient fallback（包成 `{"raw": ...}`），等于对 LLM 不可靠输出零防御。

### 本次变更

| 变更 | 影响文件 |
|---|---|
| **Schema 注册中心**：新增 `RegisterToolSchema` / `GetToolSchema` / `GetAvailableTools`，Router 查询时不触碰 Payload | `internal/tools/schema_registry.go` |
| **ValidateAndExecute**：L4 调用 L3 的唯一入口，先 Schema 校验再执行 | `internal/tools/validate.go` |
| **Router 路由验证**：`parseDecision` 改用 `GetToolSchema` 验证 Recipient，移除 whitelist 字段 | `kernel/router.go` |
| **L4 插件重构**：search/write/memory 全部改调 `ValidateAndExecute` | `plugins/*.go` |
| **L2 IPC 强化**：ProtocolMessage 增加 TraceID/Timestamp/RetryCount，dispatch 时自动注入 | `glue/protocol.go`, `glue/glue.go` |
| **Execute 硬化**：移除 lenient fallback，不合法的 JSON 直接报错 | `internal/tools/tools.go` |
| **文档对齐**：三层 → 四层架构，"第二核心" → "胶水层" | `CHANGELOG.md`, `DESIGN_PRINCIPLES.md` |

### 架构合约（写入后不再修改）

```
L1 kernel/        注册 + 路由 + 转发                   Payload 永不透明
L2 glue/          IPC + 进程管理                       不接触 Payload 内容
L3 internal/tools/ Schema 注册 + 强校验 + 执行          硬化的唯一关卡
L4 plugins/       编排 L3 完成多步任务                   只写业务逻辑，零防御代码

调用链：
  User → L1 Route (强制 DeepSeek, 仅查路由表)
       → L1 Send → L4 OnMessage
       → L3 ValidateAndExecute (Schema 校验)
       → L3 Execute → handler
       → 响应原路返回
```

### 设计纪律

见 DESIGN_PRINCIPLES.md

## 2026-05-11 架构重建

### 背景

从 1 月到 5 月，经历了多个版本的迭代（Beishan 微内核 → V9 → Mini v2.0），最后发现所有版本走向规则型的根本原因相同：**大语言模型被放在了错误的位置，或者根本没有位置。**

### 核心认知

大语言模型是文本补全器，不是通用智能。它擅长生成，不擅长思考、路由、决策。所有把 LLM 当聪明人用的架构，最终都会变成规则型。

### 本次重建的设计决策

#### 两模式路由

```
Recipient == "" → DeepSeek 决策路由（慢，但智能）
Recipient != "" → 直接转发（快，L4 已决策）
```

不给快捷方式。DeepSeek 不可用时系统不降级为规则匹配。

#### 四层架构

| 层 | 包 | 职责 | 语言 | 可修改 |
|---|---|---|---|---|
| L1 内核 | `kernel/` | 注册 + 路由 + 转发 | Go | 冻结不改 |
| L2 胶水层 | `glue/` | IPC + 进程管理 | Go | 可迭代 |
| L3 工具层 | `internal/tools/` | 工具注册 + 执行 + schema 清理 | Go | 可迭代 |
| L4 编排层 | `plugins/*.go` | 编排 L3 完成多步任务 | Go / Python | 随意改 |

#### 消息格式

Message 只有 4 个字段：Sender、Recipient、Type、Payload。
Payload 对内核永不透明。

### L4 插件清单

| 插件 | 职责 | 实现来源 |
|---|---|---|
| search_plugin | 网页搜索、内容抓取（L4 编排 → tools.ValidateAndExecute） | hermes-go tools/web.go |
| write_plugin | 文件读写、搜索、修改 | hermes-go tools/file.go |
| memory_plugin | 记忆存储、检索 | hermes-go tools/services.go（内存部分） |
| scheduler_plugin | 定时触发任务 | 新写 |

### 设计纪律

见 DESIGN_PRINCIPLES.md

### 参考项目

ds4.c（Redis 作者 antirez 的 DeepSeek V4 Flash 推理引擎）的设计哲学：
- 故意的狭窄，不做通用框架
- 接口极窄（173 行 header）
- 没有功能开关
- 注释写在代码旁边
- 正确性优先于速度

claude-for-legal（Anthropic 的法律 AI 智能体框架）：
- 工作流编排骨架（冷启动访谈→分析→输出）
- 文档自动化模板（风险矩阵、法律意见书）
- MCP 连接器接口定义
- 本项目的 L4 法律编排插件设计受此项目启发
