# OpenHuman 能力地图与吸收方案

> 基于 2026-05-24 全量实测。目标：右花探路 → 左花吸收。

---

## 一、实测结果

### Core — 全部通过

| 方法 | 实测 | 说明 |
|------|------|------|
| `core.ping` | ✅ | `{"ok": true}` |
| `core.version` | ✅ | `v0.54.10` |

### Memory — 全部通过

| 方法 | 实测 | 说明 |
|------|------|------|
| `memory.search`(recall) | ✅ | 返回 namespace 内全部记忆，无搜索词过滤 |
| `memory.store`(doc_put) | ✅ | key 去重，存储文档并向量化 |
| `context.retrieve`(context_query) | ✅ | **语义搜索** — 接受自然语言 query，返回最相关上下文 |
| `memory.list_namespaces` | ✅ | 列出所有 namespace |

**关键发现**：`context.retrieve` 是真正的向量语义搜索。存入文档后，语义匹配检索生效。

### 其他

| 方法 | 实测 | 说明 |
|------|------|------|
| `code.review`(agent_chat) | ⚠️ | 需 `message` 参数（对话模式），非纯代码审查 |
| `security_policy_info` | ✅ | 返回允许的命令列表 |

### 待深层探测

OpenHuman 实际包含 40+ 领域（tools/impl/ 下的 13 个工具模块），但 adapter 只暴露了 4 个 method。完整能力列表：

| 模块 | 能力 | 当前是否暴露 | 对底座的价值 |
|------|------|-------------|-------------|
| memory | 向量存储/召回/遗忘 | 部分 | ⭐⭐⭐ 最高 |
| agent | 委派/子智能体/偏好记忆 | ❌ | ⭐⭐ 参考设计 |
| filesystem | Git/grep/glob/文件操作 | ❌ | ⭐ 底座已有 96 工具 |
| browser | 浏览器自动化 | ❌ | ⭐ 底座已有 BrowserPlugin |
| network | MCP 工具链/Composio | ❌ | ⭐⭐ 右花协议可借鉴 |
| cron | 定时任务 CRUD | ❌ | ⭐ 底座已有 Scheduler |
| system | Shell/node/npm/LSP | ❌ | ⭐ 底座已有 TerminalPlugin |

---

## 二、吸收方案：memory.search 向量化

### 现状（beishan-core）

```
internal/tools/knowledge.go
  ├─ Embedding 字段 ✅ 已存在
  ├─ tryEmbedding() ✅ 可计算向量
  ├─ batchFillEmbedding() ✅ 批量补全
  ├─ embeddingEndpoint()/Model() ✅ 可配置
  └─ knowledge_search 目前以关键词匹配为主
```

`internal/retrieval/contract.go` 已定义 `KindSemantic` 检索类型和统一 `RetrievalResult` 格式。但实际的语义检索链路未完全打通。

### 吸收目标

将 OpenHuman 的 `memory_context_query`（自然语言 → 向量检索 → 召回最相关上下文）的模式吸收到底座中，补全 `keyword_match → vector_retrieve → rerank` 的完整链路。

### 吸收范围（深度分析）

| 组件 | 当前 | 吸收后 | 风险 |
|------|------|--------|------|
| `internal/tools/knowledge.go` | 关键词匹配 + embedding 字段 | 向量检索评分 + rerank | 低 |
| `internal/retrieval/retrieval.go` | 仅 contract.go | 实际检索执行引擎 | 低 |
| `plugins/memory_plugin.go` | knowledge_search 路由 | 自动语义回退 | 中 |
| `kernel/router.go` | 不涉及 | 不涉及 | ✅ 隔离 |

### 不吸收的部分

| 功能 | 理由 |
|------|------|
| OpenHuman 的树摘要(TreeSummarizer) | 底座知识库规模小，无需分层摘要 |
| 向量数据库 | 底座使用 JSON 文件 + 内存索引，小于 SVM |
| agent_chat(code.review) | 底座有自己的 code_security 硬化层 |

### 广度分析（非孤立检查）

```
调用链（吸收前）：
  think_plugin → knowledge_search → 关键词匹配 → 返回结果
                                    └─ 语义回退（仅关键词不足时）

调用链（吸收后）：
  think_plugin → knowledge_search → L0 关键词 + L1 语义并行
                                    → hybrid 混合评分 → rerank → 返回
```

**受影响的调用者**：
- `plugins/think_plugin.go` — 检索知识时调用 knowledge_search
- `plugins/memory_plugin.go` — knowledge_search handler
- `internal/tools/knowledge.go` — 检索执行 + embedding

**谁被它调用**：
- LLM embedding API（外部 HTTP）
- `internal/retrieval/` — 统一结果渲染

---

## 三、吸收优先级

| 优先级 | 能力 | 预估工作量 | 依赖 |
|--------|------|-----------|------|
| P0 | 向量语义检索（吸收 context_query 模式） | 2-3h | ✅ 已完成 |
| P1 | 检索质量评估（bench 套件） | 1h | 🔲 |
| P2 | 自动语义回退（关键词无结果时降级） | 1h | ✅ 已包含在 P0 中 |
| P3 | adapter methodMap 扩展（更多 OpenHuman 能力暴露） | 0.5h | 🔲 |

### 运行时经验

- **Qwen3.6-27B 太重**：~3-4s/条 embedding，237 条需 ~15min。建议用 nomic-embed-text（137MB）或 bge-small
- **配置方式**：`EMBEDDING_ENDPOINT=http://<host>:<port>/v1/embeddings` + `EMBEDDING_API_KEY=xxx`
- **当前状态**：236/237 条已有 embedding（Qwen 完成），配置已注释掉。下次需轻量模型时重新启用

---

## 四、参考项目义务

被吸收来源：OpenHuman (github.com/tinyhumansai/openhuman)

| 吸收项 | 内化位置 | 状态 |
|--------|---------|------|
| `memory_context_query` 自然语言→向量检索模式 | `internal/tools/knowledge.go` 检索管线 L1 层 | ✅ 已完成 |

### 具体变更

| 文件 | 改动 |
|------|------|
| `internal/tools/knowledge.go` | L2 语义回退 → L1 并行语义检索，关键词与语义混合评分（max 合并），检索管线不再因关键词不足才触发语义搜索 |

### 验证

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./internal/tools/...` ✅

未吸收：
- TreeSummarizer — 底座知识库规模不匹配
- agent_chat — 底座已有会话管理
- 向量数据库 — 保持文件 + 内存索引


## 缺口分析（按工作流 Step 2.5）

### 吸收 1：向量语义检索（P0）

| 未吸收项 | 原因 | 后果 | 补救 | 决策 |
|---------|------|------|------|------|
| 异步 embedding 进度通知 | 不值得 — 237 条知识，异步补全用户无感知 | 首次搜索可能 0 语义结果 | 知识库 <1000 条时不处理 | 永久 — >5000 条时重评 |
| embedding 模型自动切换 | 太复杂 — 需检测可用模型并 fallback | EMBEDDING_ENDPOINT 未配置时无自动降级通知 | 下次启用轻量模型时一起做 | 临时 |
| 检索结果相关性评分 | 遗漏 — 原版有评分，我们只取 cosine threshold | 语义结果质量不可控 | 已纳入 P1 bench：TestRetrievalRanking | 临时 — bench 通过后补 |

### 吸收 2：AI 代码审查（code_ai_review）

| 未吸收项 | 原因 | 后果 | 补救 | 决策 |
|---------|------|------|------|------|
| 多语言代码分析 | 不值得 — 底座以 Go 为主，code_security 已覆盖 | 非 Go 审查质量下降 | 保持协议调用（OpenHuman agent.chat） | 永久 |
| 审查结果结构化 | 太复杂 — agent.chat 输出自由文本 | 结果无法被自动化处理 | 回退到 code_security_check（结构化） | 临时 — 右花能力定型后 |
| SESSION_EXPIRED 自动回退 | 遗漏 — 认证错误直接展示给用户 | 用户看到错误信息 | tryAICodeReview 检测认证错误并回退 | 临时 — 下次修 adapter 时 |

### 吸收 3：Agent 委派（spawn_subagent/spawn_parallel）

| 未吸收项 | 原因 | 后果 | 补救 | 决策 |
|---------|------|------|------|------|
| 工具调用原生 API | 太复杂 — 需 provider 支持 function calling | JSON 解析可能失败 | parseToolCall 有容错 | 临时 — LLM 支持 function calling 时升级 |
| 子智能体对话持久化 | 冲突 — 硬化层原则不允许自动持久化 | 调试困难，不可回溯 | 本轮 messages 储存在内存中 | 永久 |
| 嵌套深度超限通知 | 已实现 — MAX_SPAWN_DEPTH=10 | — | — | — |
| 不同模型 | 已实现 — ModelSpec.Provider | — | — | — |
| 进度事件 | 不值得 — 底座无事件总线 | 无法实时观察进度 | log.Printf 替代 | 永久 |
| 提示词裁剪 | 不值得 — 底座提示词简单 | 无影响 | 提示词膨胀时再加 | 永久 |

---

## 缺口总结

| 维度 | 已吸收 | 未吸收 | 需关注 |
|------|--------|--------|--------|
| 向量检索 | 核心检索管线 | 评分/进度通知 | 评分已有 bench |
| 代码审查 | L3 工具 + dispatch | 结构化/认证重试 | 认证重试下次补 |
| Agent 委派 | 注册表 + 执行循环 + 事件 | 持久化/裁剪 | 事件已实现，持久化待 subscriber |
