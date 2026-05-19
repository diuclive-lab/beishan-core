# beishan-core 个人知识库使用指南

## 定位

beishan-core 不是一个笔记软件，也不是 AI 聊天框。它是**本地持续运行的个人知识工作流引擎**。

核心循环：
```
输入（手动/对话/文件/记忆）→ 结构化入库 → 审查 → 去重 → 关联 → 复盘 → 输出
```

## 数据来源

| 入口 | 方式 | 工具/工作流 |
|---|---|---|
| 手动输入 | Web 界面「入库新内容」或聊天输入 | `personal_knowledge_ingest` |
| Codex 对话 | `codex_session_list` 筛选 → `codex_conversation_ingest` 入库 | `codex_session_extract` + `codex_conversation_ingest` |
| Claude 记忆 | `claude_memory_import` 或 `claude_memory_ingest` 工作流 | `claude_memory_list` + `claude_memory_import` |
| 本地文件 | `file_parse` 解析后手动入库 | `file_parse` + `personal_knowledge_ingest` |
| Web 搜索 | `search_plugin` 搜索后手动入库 | `github_radar` / 手动操作 |

## 质量保障链

```
入库 (ingest) → 审查 (review) → 去重 (dedupe) → 合并 (merge) → 关联 (suggest_links) → 复盘 (weekly_review)
```

每个环节都是独立的 L3 工具或 L4 工作流，可以组合使用。

| 工具 | 功能 |
|---|---|
| `knowledge_review` 工作流 | 审查条目质量，自动修复 |
| `knowledge_dedupe` | 按 raw_ref/标题/标签查重 |
| `knowledge_merge` | 合并两条重复条目 |
| `knowledge_suggest_links` | 推荐关联条目 |
| `weekly_review` | 周报 + 邮件推送 |

## API 调用

```bash
# 列出 Codex 对话（取最近 5 条）
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"codex_plugin","type":"codex_session_list","payload":{"limit":5}}'

# 提取并导入 Codex 对话
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"workflow_plugin","type":"workflow_run",\
       "payload":{"workflow":"codex_conversation_ingest","input":"<session_id>"}}'

# 列出全部知识条目
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"memory_plugin","type":"knowledge_list","payload":{}}'

# 搜索知识
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"memory_plugin","type":"knowledge_search","payload":{"keyword":"xxx"}}'

# 查重
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"memory_plugin","type":"knowledge_dedupe","payload":{"id":"kn_xxx"}}'

# 合并
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"memory_plugin","type":"knowledge_merge",\
       "payload":{"source_id":"kn_xxx","target_id":"kn_yyy"}}'

# 关联建议
curl -X POST http://localhost:8013/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"recipient":"memory_plugin","type":"knowledge_suggest_links","payload":{"id":"kn_xxx"}}'
```

## Web 界面

`http://localhost:8013/` 提供完整操作界面：
- 侧栏：工作流列表、知识库操作（浏览/搜索/审查/关联）、最近知识
- 「入库新内容」按钮：打开模态框粘贴内容直接入库
- 聊天输入：支持自然语言触发工作流

## 工作流模式

`workflows/templates/` 目录提供 5 种编排模式：

| 模式 | 使用场景 | 步骤数 |
|---|---|---|
| ingest | 内容 → 分析 → 入库 | 3 |
| review | 获取 → 审查 → 修复 → 报告 | 4 |
| suggest | 获取 → 推荐 → 报告 | 2 |
| aggregate | 多源聚合 → 聚类 → 报告 | 3+ |
| search_act | 检索 → 创作 → 修订 | 3 |

新工作流可直接复制骨架文件替换 TODO 标记。

## 短期 15 条剩余

```
✅ 1.  claude_memory_import 补 suggest_links（创建 claude_memory_ingest 工作流）
⬜ 2.  codex_session_list 增加 limit 参数
⬜ 3.  codex_session_list 增加 since/until 日期过滤
⬜ 4.  codex_session_extract 增加 max_chars 参数（常量已有，缺参数暴露）
⬜ 5.  knowledge_dedupe（已实现）
⬜ 6.  knowledge_merge（已实现）
⬜ 7.  knowledge_review 批量模式
⬜ 8.  weekly_review 增加最近 N 天过滤
⬜ 9.  writing_assistant 增加关键词提炼步骤
⬜ 10. file_parse → personal_knowledge_ingest 自动链
⬜ 11. Web 最近导入列表（已实现）
⬜ 12. Web 复制知识 ID（已实现）
⬜ 13. suggest_links 一键写入 links 确认工具
⬜ 14. smoke 测试补 Claude/Codex 错误场景
⬜ 15. PERSONAL_KB_GUIDE.md（已创建）
```

注意：2/3/4 为 codex 工具增强，7/8/9/10/13 为 L4 工作流增强。

## 架构原理

```
L1 kernel/      注册 + 路由 + 转发            冻结不改
L2 glue/        IPC + 进程管理                不接触 Payload
L3 internal/tools/ Schema 校验 + 执行         硬化关卡
L4 plugins/     编排 L3 完成任务              业务逻辑

知识条目存储: ~/.hermes/memory/knowledge/<id>.json
待办存储:      ~/.hermes/memory/todos.json
Codex 会话:    ~/.codex/sessions/ / archived_sessions/
Claude 记忆:   $CLAUDE_MEMORY_DIR（默认 ~/.claude/projects/-Users-dc/memory/）
```
